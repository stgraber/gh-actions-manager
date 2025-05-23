package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/go-github/v67/github"
)

// Github globals.
var ghClient *github.Client

type ghWebhook struct {
	Action   string `json:"action"`
	Workflow struct {
		URL    string   `json:"html_url"`
		ID     int      `json:"id"`
		Name   string   `json:"name"`
		Labels []string `json:"labels"`
	} `json:"workflow_job"`
	Repository struct {
		Name  string `json:"name"`
		Owner struct {
			Login string `json:"login"`
		} `json:"owner"`
	} `json:"repository"`
}

func ghConnect() error {
	ghClient = github.NewClient(nil).WithAuthToken(config.Github.Token)

	return nil
}

func ghValidateSignature(r *http.Request, body []byte) error {
	// Get the header.
	signature := strings.SplitN(r.Header.Get("X-Hub-Signature-256"), "=", 2)
	if signature[0] != "sha256" {
		return errors.New("github signature header not found")
	}

	// Compute the hash.
	hash := hmac.New(sha256.New, []byte(config.Github.Webhook.Key))
	_, err := hash.Write(body)
	if err != nil {
		return fmt.Errorf("failed to compute the request hash: %w", err)
	}

	// Validate the hash.
	expectedHash := hex.EncodeToString(hash.Sum(nil))
	if signature[1] != expectedHash {
		return fmt.Errorf("signature mismatch: %s vs %s", signature[1], expectedHash)
	}

	return nil
}

func ghHandle(w http.ResponseWriter, r *http.Request) {
	err := ghHandleRequest(r.Context(), r)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("500 - Internal Server Error"))
		slog.Error("Failed to handle request", "err", err)
	}
}

func ghHandleRequest(ctx context.Context, r *http.Request) error {
	// Read the request body.
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return fmt.Errorf("failed to read the request body: %w", err)
	}
	defer r.Body.Close()

	// Validate that it's from Github.
	err = ghValidateSignature(r, body)
	if err != nil {
		return fmt.Errorf("failed validation: %w", err)
	}

	// Parse the request.
	var req *ghWebhook
	err = json.Unmarshal(body, &req)
	if err != nil {
		return fmt.Errorf("failed to parse body: %w", err)
	}

	// Only handle queued items.
	if req.Action != "queued" {
		slog.Error("unknown action", "action", req.Action)

		return nil
	}

	// Skip requests for hosted runners.
	if !slices.Contains(req.Workflow.Labels, "self-hosted") {
		return nil
	}

	// Print the request.
	slog.Info("New request", "action", req.Action, "workflow", req.Workflow.Name, "labels", req.Workflow.Labels)

	// Figure out the instance needed.
	instCPU := config.Defaults.CPU
	instMemory := config.Defaults.Memory
	instDisk := config.Defaults.Disk
	instArch := config.Defaults.Architecture
	instOS := config.Defaults.Image

	for _, label := range req.Workflow.Labels {
		fields := strings.SplitN(label, "-", 2)
		if len(fields) < 2 {
			continue
		}

		if fields[0] == "cpu" {
			value, err := strconv.Atoi(fields[1])
			if err != nil {
				continue
			}

			instCPU = value

			continue
		}

		if fields[0] == "mem" {
			instMemory = strings.Replace(fields[1], "G", "GiB", 1)

			continue
		}

		if fields[0] == "disk" {
			instDisk = strings.Replace(fields[1], "G", "GiB", 1)

			continue
		}

		if fields[0] == "arch" {
			instArch = fields[1]

			continue
		}

		if fields[0] == "image" {
			instOS = strings.ReplaceAll(fields[1], "-", "/")

			continue
		}
	}

	// Obtain a new token.
	token, _, err := ghClient.Actions.CreateRegistrationToken(ctx, req.Repository.Owner.Login, req.Repository.Name)
	if err != nil {
		return fmt.Errorf("couldn't register worker: %w", err)
	}

	// Spawn the runner.
	slog.Info("Spawning instance", "os", instOS, "architecture", instArch, "cpu", instCPU, "memory", instMemory, "disk", instDisk, "url", req.Workflow.URL)

	for range 5 {
		err = incusSpawnInstance(fmt.Sprintf("gh-%s-%s-%d", req.Repository.Owner.Login, req.Repository.Name, req.Workflow.ID), req.Workflow.Labels, instOS, instArch, instCPU, instMemory, instDisk, fmt.Sprintf("%s/%s", req.Repository.Owner.Login, req.Repository.Name), *token.Token)
		if err == nil {
			break
		}

		time.Sleep(5 * time.Second)
	}

	if err != nil {
		return fmt.Errorf("failed to spawn instance: %w", err)
	}

	return nil
}

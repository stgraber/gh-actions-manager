// Package main is used for the gh-actions-manager daemon.
package main

import (
	"log/slog"
	"net/http"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config represents the Github Actions Manager configuration.
type Config struct {
	Defaults struct {
		Architecture string `yaml:"architecture"`
		CPU          int    `yaml:"cpu"`
		Memory       string `yaml:"memory"`
		Disk         string `yaml:"disk"`
		Image        string `yaml:"image"`
	} `yaml:"defaults"`

	Daemon struct {
		Listen string `yaml:"listen"`
	} `yaml:"daemon"`

	Incus struct {
		Client struct {
			Certificate string `yaml:"certificate"`
			Key         string `yaml:"key"`
		} `yaml:"client"`

		Project string `yaml:"project"`

		Server struct {
			URL         string `yaml:"url"`
			Certificate string `yaml:"certificate"`
		} `yaml:"server"`
	} `yaml:"incus"`

	Github struct {
		Agent struct {
			Version string `yaml:"version"`
		} `yaml:"agent"`

		Token string `yaml:"token"`

		Webhook struct {
			Key string `yaml:"key"`
		} `yaml:"webhook"`
	} `yaml:"github"`
}

var config *Config

func main() {
	// Load the configuration.
	data, err := os.ReadFile(os.Args[1])
	if err != nil {
		slog.Error("failed to read the configuration", "err", err)
		os.Exit(1)
	}

	err = yaml.Unmarshal(data, &config)
	if err != nil {
		slog.Error("failed to parse the configuration", "err", err)
		os.Exit(1)
	}

	// Connect to Github.
	err = ghConnect()
	if err != nil {
		slog.Error("failed to connect to Github", "err", err)
		os.Exit(1)
	}

	// Login to LXD.
	err = incusConnect()
	if err != nil {
		slog.Error("failed to connect to Incus", "err", err)
		os.Exit(1)
	}

	// Start the web server.
	http.HandleFunc("/", ghHandle)

	server := &http.Server{
		Addr:              config.Daemon.Listen,
		ReadHeaderTimeout: 3 * time.Second,
	}

	err = server.ListenAndServe()
	if err != nil {
		slog.Error("failed start server", "err", err)
		os.Exit(1)
	}
}

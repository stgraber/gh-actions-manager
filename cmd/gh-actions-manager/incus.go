package main

import (
	"fmt"
	"strings"
	"sync"
	"text/template"

	"github.com/lxc/incus/v6/client"
	"github.com/lxc/incus/v6/shared/api"
	"github.com/lxc/incus/v6/shared/revert"
)

var incusClient incus.InstanceServer
var incusMu sync.Mutex

func incusConnect() error {
	// Setup connection arguments.
	args := &incus.ConnectionArgs{
		TLSClientCert: config.Incus.Client.Certificate,
		TLSClientKey:  config.Incus.Client.Key,
		TLSServerCert: config.Incus.Server.Certificate,
	}

	// Connect to the server.
	client, err := incus.ConnectIncus(config.Incus.Server.URL, args)
	if err != nil {
		return err
	}

	// Set the client.
	incusClient = client.UseProject(config.Incus.Project)
	return nil
}

var incusCloudInitTpl = template.Must(template.New("incusCloudInitTpl").Parse(`#cloud-config:
package_update: true
package_upgrade: true

packages:
  - apt-transport-https
  - curl
  - git
  - jq
  - libicu-dev

runcmd:
  - mkdir /actions-runner
  - cd /actions-runner
  - curl -O -L https://github.com/actions/runner/releases/download/v{{ .agentVersion }}/actions-runner-linux-{{ .agentArch }}-{{ .agentVersion }}.tar.gz
  - tar xzf ./actions-runner-linux-{{ .agentArch }}-{{ .agentVersion }}.tar.gz
  - RUNNER_ALLOW_RUNASROOT=1 ./bin/installdependencies.sh
  - RUNNER_ALLOW_RUNASROOT=1 ./config.sh --url https://github.com/{{ .repo }} --token {{ .token }} --ephemeral --labels {{ .labels }}
  - RUNNER_ALLOW_RUNASROOT=1 HOME=/root USER=root SHELL=/bin/bash ./run.sh
  - poweroff
`))

func incusUserData(arch string, labels []string, repo string, token string) string {
	// Render the cloud-init data.
	var agentArch string
	if arch == "amd64" {
		agentArch = "x64"
	} else if arch == "arm64" {
		agentArch = "arm64"
	} else {
		return ""
	}

	var sb *strings.Builder = &strings.Builder{}
	err := incusCloudInitTpl.Execute(sb, map[string]any{
		"agentVersion": config.Github.Agent.Version,
		"agentArch":    agentArch,
		"repo":         repo,
		"token":        token,
		"labels":       strings.Join(labels, ","),
	})
	if err != nil {
		return ""
	}

	return sb.String()
}

func incusSpawnInstance(name string, labels []string, os string, arch string, cpu int, memory string, disk string, repo string, token string) error {
	// Setup a reverter.
	reverter := revert.New()
	defer reverter.Fail()

	// Create the instance.
	incusMu.Lock()
	req := api.InstancesPost{
		Source: api.InstanceSource{
			Type:     "image",
			Alias:    fmt.Sprintf("%s/cloud/%s", os, arch),
			Server:   "https://images.linuxcontainers.org",
			Protocol: "simplestreams",
		},
		InstancePut: api.InstancePut{
			Config: map[string]string{
				"limits.cpu":           fmt.Sprintf("%d", cpu),
				"limits.memory":        memory,
				"cloud-init.user-data": incusUserData(arch, labels, repo, token),
			},
			Ephemeral: true,
		},
		Name: name,
		Type: "virtual-machine",
	}

	op, err := incusClient.CreateInstance(req)
	if err != nil {
		incusMu.Unlock()
		return err
	}

	reverter.Add(func() {
		incusClient.DeleteInstance(req.Name)
	})

	err = op.Wait()
	if err != nil {
		incusMu.Unlock()
		return err
	}
	incusMu.Unlock()

	// Get the current instance definition.
	inst, etag, err := incusClient.GetInstance(name)
	if err != nil {
		return err
	}

	// Override the disk entry.
	inst.Devices["root"] = inst.ExpandedDevices["root"]
	inst.Devices["root"]["size"] = disk

	// Update the instance config.
	op, err = incusClient.UpdateInstance(name, inst.InstancePut, etag)
	if err != nil {
		return err
	}

	err = op.Wait()
	if err != nil {
		return err
	}

	// Start the instance.
	reqState := api.InstanceStatePut{
		Action: "start",
	}

	incusMu.Lock()
	op, err = incusClient.UpdateInstanceState(name, reqState, "")
	if err != nil {
		incusMu.Unlock()
		return err
	}

	err = op.Wait()
	if err != nil {
		incusMu.Unlock()
		return err
	}
	incusMu.Unlock()

	// We're done.
	reverter.Success()

	return nil
}

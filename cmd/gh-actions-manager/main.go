package main

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"os"

	"gopkg.in/yaml.v3"
)

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
		}
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
	data, err := ioutil.ReadFile(os.Args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to read the configuration: %s\n", err)
		os.Exit(1)
	}

	err = yaml.Unmarshal(data, &config)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to parse the configuration: %s\n", err)
		os.Exit(1)
	}

	// Connect to Github.
	err = ghConnect()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to connect to Github: %s\n", err)
		os.Exit(1)
	}

	// Login to LXD.
	err = incusConnect()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to connect to Incus: %s\n", err)
		os.Exit(1)
	}

	// Start the web server.
	http.HandleFunc("/", ghHandle)
	http.ListenAndServe(config.Daemon.Listen, nil)
}

package api

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/goccy/go-yaml"
)

type settings struct {
	Welcome      bool              `yaml:"welcome"`
	Terminal     map[string]string `yaml:"terminal"`
	AllowedHosts []string          `yaml:"allowed_hosts"`
	UI           struct {
		Address string `yaml:"address"`
	} `yaml:"ui"`
}

func readSettings(dir, name string) (settings, error) {
	var config settings
	file := name
	if !filepath.IsAbs(file) {
		file = filepath.Join(dir, file)
	}
	data, err := os.ReadFile(file)
	if err != nil && (!errors.Is(err, os.ErrNotExist) || name != "rtcbench.yml") {
		return config, err
	}
	if err := yaml.UnmarshalWithOptions(data, &config, yaml.Strict()); err != nil {
		return config, fmt.Errorf("%s: %w", file, err)
	}
	if err := config.validate(); err != nil {
		return config, fmt.Errorf("%s: %w", file, err)
	}
	return config, nil
}

func (s settings) validate() error {
	for kind, value := range s.Terminal {
		switch kind {
		case "server", "instances", "deployments":
		default:
			return fmt.Errorf("unknown terminal type %q", kind)
		}
		if value != "disabled" && value != "enabled" {
			return fmt.Errorf("terminal.%s must be enabled or disabled", kind)
		}
	}
	return nil
}

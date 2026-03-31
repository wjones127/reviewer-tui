package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

type Config struct {
	User          string   `toml:"user"`
	Repos         []string `toml:"repos"`
	Tags          []string `toml:"tags"`
	TriageEnabled bool     `toml:"triage_enabled"`
}

func configDir() string {
	return filepath.Join(os.Getenv("HOME"), ".config", "reviewer-tui")
}

func DefaultConfigPath() string {
	return filepath.Join(configDir(), "config.toml")
}

func LoadConfig(path string) (Config, error) {
	var cfg Config
	_, err := toml.DecodeFile(path, &cfg)
	if err != nil {
		return cfg, fmt.Errorf("loading config: %w", err)
	}
	if cfg.User == "" {
		return cfg, fmt.Errorf("config: 'user' is required")
	}
	if len(cfg.Repos) == 0 {
		return cfg, fmt.Errorf("config: 'repos' must have at least one entry")
	}
	return cfg, nil
}

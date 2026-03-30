package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	content := `
user = "alice"
repos = ["org/repo-a", "org/repo-b"]
tags = ["python", "rust"]
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.User != "alice" {
		t.Errorf("User = %q, want %q", cfg.User, "alice")
	}
	if len(cfg.Repos) != 2 {
		t.Errorf("Repos len = %d, want 2", len(cfg.Repos))
	}
	if len(cfg.Tags) != 2 {
		t.Errorf("Tags len = %d, want 2", len(cfg.Tags))
	}
}

func TestLoadConfigMissingUser(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	content := `repos = ["org/repo"]`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadConfig(path)
	if err == nil {
		t.Fatal("expected error for missing user")
	}
}

func TestLoadConfigMissingRepos(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	content := `user = "alice"`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadConfig(path)
	if err == nil {
		t.Fatal("expected error for missing repos")
	}
}

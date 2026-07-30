package config

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestLoadDemoDefaults(t *testing.T) {
	t.Setenv("HEARTH_DEMO", "true")
	t.Setenv("HEARTH_ADMIN_PASSWORD", "")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Listen != "127.0.0.1:8080" {
		t.Fatalf("Listen = %q", cfg.Listen)
	}
	if cfg.AdminPassword != "admin" {
		t.Fatalf("AdminPassword = %q", cfg.AdminPassword)
	}
}

func TestLoadRequiresPasswordOutsideDemo(t *testing.T) {
	t.Setenv("HEARTH_DEMO", "false")
	t.Setenv("HEARTH_ADMIN_PASSWORD", "")

	if _, err := Load(""); err == nil {
		t.Fatal("Load() expected an error")
	}
}

func TestLoadFileAndEnvironmentOverride(t *testing.T) {
	t.Setenv("HEARTH_DEMO", "false")
	t.Setenv("HEARTH_ADMIN_PASSWORD", "secret")
	t.Setenv("HEARTH_LISTEN", "127.0.0.1:9090")

	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"listen":"0.0.0.0:8080","secureCookies":true}`), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Listen != "127.0.0.1:9090" {
		t.Fatalf("Listen = %q", cfg.Listen)
	}
	if !cfg.SecureCookies {
		t.Fatal("SecureCookies = false")
	}
}

func TestLoadPasswordFile(t *testing.T) {
	t.Setenv("HEARTH_DEMO", "false")
	t.Setenv("HEARTH_ADMIN_PASSWORD", "")

	directory := t.TempDir()
	passwordPath := filepath.Join(directory, "password.txt")
	if err := os.WriteFile(passwordPath, []byte("\uFEFFfile-secret\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(directory, "config.json")
	configJSON := []byte(`{"listen":"127.0.0.1:8080","adminPasswordFile":` + strconv.Quote(passwordPath) + `}`)
	if err := os.WriteFile(configPath, configJSON, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.AdminPassword != "file-secret" {
		t.Fatalf("AdminPassword = %q", cfg.AdminPassword)
	}
}

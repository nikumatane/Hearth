package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
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

func TestRevisionExcludesRuntimeAdminPassword(t *testing.T) {
	first := Config{Listen: "127.0.0.1:8080", AdminPassword: "first-secret"}
	second := first
	second.AdminPassword = "second-secret"
	firstRevision, err := Revision(first)
	if err != nil {
		t.Fatal(err)
	}
	secondRevision, err := Revision(second)
	if err != nil {
		t.Fatal(err)
	}
	if firstRevision != secondRevision {
		t.Fatal("runtime administrator password changed the persisted revision")
	}
}

func TestSaveRetainsPreviousAndOmitsRuntimeSecrets(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "config.json")
	if err := os.WriteFile(path, []byte("old-config\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := Config{
		Listen: "127.0.0.1:8080", AdminPassword: "must-not-be-persisted", LogFile: "runtime.log",
		Management: ManagementConfig{InstallRoot: filepath.Join(directory, "games")},
	}
	if err := Save(path, cfg); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	previous, err := os.ReadFile(path + ".previous")
	if err != nil {
		t.Fatal(err)
	}
	if string(previous) != "old-config\n" {
		t.Fatalf("previous = %q", previous)
	}
	current, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(current)
	if strings.Contains(text, "must-not-be-persisted") || strings.Contains(text, "runtime.log") {
		t.Fatalf("runtime secret was persisted: %s", text)
	}
	if !strings.Contains(text, `"installRoot"`) {
		t.Fatalf("management settings missing: %s", text)
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
	t.Setenv("HEARTH_ADMIN_PASSWORD", "secret1234")
	t.Setenv("HEARTH_LISTEN", "127.0.0.1:9090")

	directory := t.TempDir()
	path := filepath.Join(directory, "config.json")
	auditPath := filepath.Join(directory, "login-audit.jsonl")
	if err := os.WriteFile(
		path,
		[]byte(`{"listen":"0.0.0.0:8080","secureCookies":true,"auditFile":`+strconv.Quote(auditPath)+`}`),
		0o600,
	); err != nil {
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
	if cfg.ConfigAuditFile != filepath.Join(directory, "config-audit.jsonl") {
		t.Fatalf("ConfigAuditFile = %q", cfg.ConfigAuditFile)
	}
	if cfg.OperationAuditFile != filepath.Join(directory, "operation-audit.jsonl") {
		t.Fatalf("OperationAuditFile = %q", cfg.OperationAuditFile)
	}
	if cfg.IPRulesFile != filepath.Join(directory, "ip-rules.json") {
		t.Fatalf("IPRulesFile = %q", cfg.IPRulesFile)
	}
	if cfg.DeviceKeyFile != filepath.Join(directory, "device-cookie.key") {
		t.Fatalf("DeviceKeyFile = %q", cfg.DeviceKeyFile)
	}
	if len(cfg.TrustedProxyCIDRs) != 2 ||
		cfg.TrustedProxyCIDRs[0] != "127.0.0.0/8" ||
		cfg.TrustedProxyCIDRs[1] != "::1/128" {
		t.Fatalf("TrustedProxyCIDRs = %#v", cfg.TrustedProxyCIDRs)
	}
	if cfg.Update.Channel != "stable" || cfg.Update.StagingDir != filepath.Join(directory, "updates") {
		t.Fatalf("Update = %#v", cfg.Update)
	}
}

func TestSaveAndLoadPreservesExplicitEmptyTrustedProxyList(t *testing.T) {
	t.Setenv("HEARTH_DEMO", "false")
	t.Setenv("HEARTH_ADMIN_PASSWORD", "secret1234")

	path := filepath.Join(t.TempDir(), "config.json")
	cfg := Config{
		Listen:            "127.0.0.1:8080",
		TrustedProxyCIDRs: []string{},
	}
	if err := Save(path, cfg); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.TrustedProxyCIDRs == nil || len(loaded.TrustedProxyCIDRs) != 0 {
		t.Fatalf("TrustedProxyCIDRs = %#v; want explicit empty list", loaded.TrustedProxyCIDRs)
	}
}

func TestLoadRejectsUnknownUpdateChannel(t *testing.T) {
	t.Setenv("HEARTH_DEMO", "true")
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"update":{"channel":"nightly"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("Load() accepted an unknown update channel")
	}
}

func TestLoadRejectsShortProductionAdminPassword(t *testing.T) {
	t.Setenv("HEARTH_DEMO", "false")
	t.Setenv("HEARTH_ADMIN_PASSWORD", "short")

	if _, err := Load(""); err == nil {
		t.Fatal("Load() accepted a production administrator password shorter than 10 characters")
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

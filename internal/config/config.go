package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const minimumAdminPasswordLength = 10

type GameConfig struct {
	Enabled                   bool     `json:"enabled"`
	ServiceUnit               string   `json:"serviceUnit,omitempty"`
	InstallDir                string   `json:"installDir"`
	SteamCmd                  string   `json:"steamCmd"`
	SettingsFile              string   `json:"settingsFile,omitempty"`
	DefaultSettingsFile       string   `json:"defaultSettingsFile,omitempty"`
	Executable                string   `json:"executable,omitempty"`
	ProcessName               string   `json:"processName,omitempty"`
	StartArgs                 []string `json:"startArgs,omitempty"`
	BackupDir                 string   `json:"backupDir,omitempty"`
	BackupRetentionDays       int      `json:"backupRetentionDays,omitempty"`
	BackupMaxTotalGB          int64    `json:"backupMaxTotalGB,omitempty"`
	RESTURL                   string   `json:"restUrl,omitempty"`
	RESTUsername              string   `json:"restUsername,omitempty"`
	ShutdownWaitSeconds       int      `json:"shutdownWaitSeconds,omitempty"`
	SteamCmdNoProgressMinutes int      `json:"steamCmdNoProgressMinutes,omitempty"`
	Port                      int      `json:"port,omitempty"`
	ClusterDir                string   `json:"clusterDir,omitempty"`
}

type GamesConfig struct {
	Palworld           GameConfig `json:"palworld"`
	DontStarveTogether GameConfig `json:"dontStarveTogether"`
}

type Config struct {
	Listen             string      `json:"listen"`
	Demo               bool        `json:"demo"`
	SecureCookies      bool        `json:"secureCookies"`
	LogFile            string      `json:"-"`
	AdminPassword      string      `json:"-"`
	PasswordFile       string      `json:"adminPasswordFile,omitempty"`
	AccessFile         string      `json:"accessFile,omitempty"`
	AuditFile          string      `json:"auditFile,omitempty"`
	ConfigAuditFile    string      `json:"configAuditFile,omitempty"`
	OperationAuditFile string      `json:"operationAuditFile,omitempty"`
	IPRulesFile        string      `json:"ipRulesFile,omitempty"`
	DeviceKeyFile      string      `json:"deviceKeyFile,omitempty"`
	TrustedProxyCIDRs  []string    `json:"trustedProxyCidrs,omitempty"`
	Games              GamesConfig `json:"games"`
}

func Load(path string) (Config, error) {
	cfg := Config{
		Listen: "127.0.0.1:8080",
		Demo:   os.Getenv("HEARTH_DEMO") == "true",
	}

	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return Config{}, fmt.Errorf("read %s: %w", path, err)
		}
		if err := json.Unmarshal(data, &cfg); err != nil {
			return Config{}, fmt.Errorf("decode %s: %w", path, err)
		}
	}

	if listen := os.Getenv("HEARTH_LISTEN"); listen != "" {
		cfg.Listen = listen
	}
	if demo, ok := os.LookupEnv("HEARTH_DEMO"); ok {
		cfg.Demo = demo == "true"
	}
	cfg.AdminPassword = os.Getenv("HEARTH_ADMIN_PASSWORD")
	if cfg.AdminPassword == "" && cfg.PasswordFile != "" {
		password, err := os.ReadFile(cfg.PasswordFile)
		if err != nil {
			return Config{}, fmt.Errorf("read admin password file: %w", err)
		}
		if len(password) > 4096 {
			return Config{}, errors.New("admin password file is too large")
		}
		cfg.AdminPassword = strings.TrimSpace(strings.TrimPrefix(string(password), "\uFEFF"))
	}
	if cfg.Demo && cfg.AdminPassword == "" {
		cfg.AdminPassword = "admin"
	}
	if cfg.Listen == "" {
		return Config{}, errors.New("listen address must not be empty")
	}
	if cfg.AdminPassword == "" {
		return Config{}, errors.New("Hearth admin password is required through HEARTH_ADMIN_PASSWORD or adminPasswordFile")
	}
	if !cfg.Demo && utf8.RuneCountInString(cfg.AdminPassword) < minimumAdminPasswordLength {
		return Config{}, fmt.Errorf("Hearth admin password must contain at least %d characters", minimumAdminPasswordLength)
	}
	if cfg.ConfigAuditFile == "" && cfg.AuditFile != "" {
		cfg.ConfigAuditFile = filepath.Join(filepath.Dir(cfg.AuditFile), "config-audit.jsonl")
	}
	if cfg.OperationAuditFile == "" && cfg.AuditFile != "" {
		cfg.OperationAuditFile = filepath.Join(filepath.Dir(cfg.AuditFile), "operation-audit.jsonl")
	}
	if cfg.IPRulesFile == "" && cfg.AuditFile != "" {
		cfg.IPRulesFile = filepath.Join(filepath.Dir(cfg.AuditFile), "ip-rules.json")
	}
	if cfg.DeviceKeyFile == "" && cfg.AuditFile != "" {
		cfg.DeviceKeyFile = filepath.Join(filepath.Dir(cfg.AuditFile), "device-cookie.key")
	}
	if cfg.TrustedProxyCIDRs == nil {
		cfg.TrustedProxyCIDRs = []string{"127.0.0.0/8", "::1/128"}
	}
	return cfg, nil
}

package config

import (
	"crypto/sha256"
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

type ManagementConfig struct {
	InstallRoot    string   `json:"installRoot,omitempty"`
	SteamCmdRoot   string   `json:"steamCmdRoot,omitempty"`
	DiscoveryRoots []string `json:"discoveryRoots,omitempty"`
}

type UpdateConfig struct {
	Channel    string `json:"channel,omitempty"`
	TokenFile  string `json:"tokenFile,omitempty"`
	StagingDir string `json:"stagingDir,omitempty"`
}

type Config struct {
	Listen             string           `json:"listen"`
	Demo               bool             `json:"demo"`
	SecureCookies      bool             `json:"secureCookies"`
	LogFile            string           `json:"-"`
	AdminPassword      string           `json:"-"`
	PasswordFile       string           `json:"adminPasswordFile,omitempty"`
	AccessFile         string           `json:"accessFile,omitempty"`
	AuditFile          string           `json:"auditFile,omitempty"`
	ConfigAuditFile    string           `json:"configAuditFile,omitempty"`
	OperationAuditFile string           `json:"operationAuditFile,omitempty"`
	IPRulesFile        string           `json:"ipRulesFile,omitempty"`
	DeviceKeyFile      string           `json:"deviceKeyFile,omitempty"`
	TrustedProxyCIDRs  []string         `json:"trustedProxyCidrs,omitempty"`
	Management         ManagementConfig `json:"management,omitempty"`
	Update             UpdateConfig     `json:"update,omitempty"`
	Games              GamesConfig      `json:"games"`
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
	if cfg.Update.Channel == "" {
		cfg.Update.Channel = "stable"
	}
	if cfg.Update.Channel != "stable" && cfg.Update.Channel != "prerelease" {
		return Config{}, errors.New("update channel must be stable or prerelease")
	}
	stateDirectory := ""
	if cfg.AuditFile != "" {
		stateDirectory = filepath.Dir(cfg.AuditFile)
	} else if path != "" {
		stateDirectory = filepath.Dir(path)
	}
	if stateDirectory != "" {
		absoluteStateDirectory, err := filepath.Abs(stateDirectory)
		if err != nil {
			return Config{}, fmt.Errorf("resolve Hearth state directory: %w", err)
		}
		stateDirectory = absoluteStateDirectory
		if cfg.Update.TokenFile == "" {
			cfg.Update.TokenFile = filepath.Join(stateDirectory, "github-token.txt")
		}
		if cfg.Update.StagingDir == "" {
			cfg.Update.StagingDir = filepath.Join(stateDirectory, "updates")
		}
	}
	if cfg.Update.TokenFile != "" && !filepath.IsAbs(cfg.Update.TokenFile) {
		return Config{}, errors.New("update tokenFile must be an absolute path")
	}
	if cfg.Update.StagingDir != "" && !filepath.IsAbs(cfg.Update.StagingDir) {
		return Config{}, errors.New("update stagingDir must be an absolute path")
	}
	return cfg, nil
}

// Revision returns a stable digest of the persisted, non-secret configuration.
// Runtime-only values and the administrator password are excluded by their
// json tags, so the revision can safely be returned to an authenticated admin.
func Revision(cfg Config) (string, error) {
	data, err := json.Marshal(cfg)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(data)), nil
}

// Save atomically replaces a configuration file and retains one known-good
// predecessor at <path>.previous. It never persists runtime-only secrets.
func Save(path string, cfg Config) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("configuration path is required")
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("encode configuration: %w", err)
	}
	data = append(data, '\n')
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create configuration directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".hearth-config-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary configuration: %w", err)
	}
	temporaryPath := temporary.Name()
	cleanup := true
	defer func() {
		_ = temporary.Close()
		if cleanup {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return fmt.Errorf("secure temporary configuration: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		return fmt.Errorf("write temporary configuration: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("sync temporary configuration: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary configuration: %w", err)
	}

	previousPath := path + ".previous"
	if _, err := os.Stat(path); err == nil {
		if removeErr := os.Remove(previousPath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			return fmt.Errorf("remove stale previous configuration: %w", removeErr)
		}
		if err := os.Rename(path, previousPath); err != nil {
			return fmt.Errorf("retain previous configuration: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect current configuration: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		if _, previousErr := os.Stat(previousPath); previousErr == nil {
			_ = os.Rename(previousPath, path)
		}
		return fmt.Errorf("activate configuration: %w", err)
	}
	cleanup = false
	return nil
}

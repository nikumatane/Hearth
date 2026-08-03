package gamemanager

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"hearth/internal/config"
	"hearth/internal/panel"
)

func TestDiscoveryAndExplicitAdoption(t *testing.T) {
	root := t.TempDir()
	installDir := filepath.Join(root, "steamapps", "common", "PalServer")
	steamRoot := filepath.Join(root, "steamcmd")
	settingsPath := filepath.Join(installDir, "Pal", "Saved", "Config", "WindowsServer", "PalWorldSettings.ini")
	for _, path := range []string{
		filepath.Join(installDir, "PalServer.exe"),
		filepath.Join(installDir, "DefaultPalWorldSettings.ini"),
		settingsPath,
		filepath.Join(steamRoot, "steamcmd.exe"),
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("test"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	configPath := filepath.Join(root, "config.json")
	cfg := config.Config{
		Listen: "127.0.0.1:8080",
		Management: config.ManagementConfig{
			InstallRoot:    filepath.Join(root, "steamapps", "common"),
			SteamCmdRoot:   steamRoot,
			DiscoveryRoots: []string{root},
		},
		TrustedProxyCIDRs: []string{"127.0.0.0/8", "::1/128"},
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	manager, err := New(cfg, configPath)
	if err != nil {
		t.Fatal(err)
	}
	manager.factory = func(config.GameConfig) (panel.Service, error) { return panel.NewDemoService(), nil }
	document := manager.Management()
	game := document.Games[0]
	if game.State != "detected" || len(game.Candidates) != 1 || !game.CanAdopt {
		t.Fatalf("game = %#v", game)
	}
	if _, err := manager.AdoptGame(palworldID, panel.AdoptGameRequest{CandidateID: game.Candidates[0].ID}); !errors.Is(err, panel.ErrInvalid) {
		t.Fatalf("unconfirmed adoption error = %v", err)
	}
	managed, err := manager.AdoptGame(palworldID, panel.AdoptGameRequest{
		CandidateID: game.Candidates[0].ID, Confirm: true,
	})
	if err != nil {
		t.Fatalf("AdoptGame() error = %v", err)
	}
	if managed.State != "managed" || managed.InstallDir != installDir {
		t.Fatalf("managed = %#v", managed)
	}
	persisted, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	var persistedConfig config.Config
	if err := json.Unmarshal(persisted, &persistedConfig); err != nil {
		t.Fatalf("decode persisted config: %v\n%s", err, persisted)
	}
	if persistedConfig.Games.Palworld.InstallDir != installDir {
		t.Fatalf("persisted config = %s", persisted)
	}
}

func TestDiscoveryShowsDSTAsPlannedOnly(t *testing.T) {
	root := t.TempDir()
	executable := filepath.Join(root, "DST", "bin64", "dontstarve_dedicated_server_nullrenderer_x64.exe")
	if err := os.MkdirAll(filepath.Dir(executable), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(executable, []byte("test"), 0o600); err != nil {
		t.Fatal(err)
	}
	manager, err := New(config.Config{
		Management: config.ManagementConfig{
			InstallRoot: filepath.Join(root, "games"), SteamCmdRoot: filepath.Join(root, "steamcmd"),
			DiscoveryRoots: []string{root},
		},
	}, filepath.Join(root, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	dst := manager.Management().Games[1]
	if dst.Support != "planned" || dst.State != "detected" || dst.CanInstall || dst.CanAdopt {
		t.Fatalf("dst = %#v", dst)
	}
}

func TestSystemSettingsRejectStaleRevisionAndGlobalProxy(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.json")
	cfg := config.Config{
		Management:        config.ManagementConfig{InstallRoot: filepath.Join(root, "games"), SteamCmdRoot: filepath.Join(root, "steamcmd")},
		TrustedProxyCIDRs: []string{"127.0.0.0/8"},
	}
	if err := config.Save(configPath, cfg); err != nil {
		t.Fatal(err)
	}
	manager, err := New(cfg, configPath)
	if err != nil {
		t.Fatal(err)
	}
	settings := manager.Management().Settings
	patch := panel.SystemSettingsPatch{
		Revision: "stale", InstallRoot: settings.InstallRoot, SteamCmdRoot: settings.SteamCmdRoot,
		BackupRetentionDays: 30, BackupMaxTotalGB: 20, ShutdownWaitSeconds: 30,
		SteamCmdNoProgressMinutes: 30, PalworldPort: 8211,
		TrustedProxyCIDRs: []string{"127.0.0.0/8"},
	}
	if _, err := manager.UpdateSystemSettings(patch); !errors.Is(err, panel.ErrInvalid) {
		t.Fatalf("stale revision error = %v", err)
	}
	patch.Revision = settings.Revision
	patch.TrustedProxyCIDRs = []string{"0.0.0.0/0"}
	if _, err := manager.UpdateSystemSettings(patch); !errors.Is(err, panel.ErrInvalid) {
		t.Fatalf("global proxy error = %v", err)
	}
}

func TestInstallValidationRejectsOverlapAndExistingFiles(t *testing.T) {
	root := t.TempDir()
	if err := validateInstallPaths(filepath.Join(root, "steamcmd", "PalServer"), filepath.Join(root, "steamcmd"), filepath.Join(root, "hearth", "config.json")); !errors.Is(err, panel.ErrInvalid) {
		t.Fatalf("overlap error = %v", err)
	}
	installDir := filepath.Join(root, "PalServer")
	if err := os.MkdirAll(installDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(installDir, "existing.save"), []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := requireEmptyInstallDirectory(installDir); !errors.Is(err, panel.ErrInvalid) {
		t.Fatalf("existing directory error = %v", err)
	}
}

func TestSteamCMDArchiveRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	archivePath := filepath.Join(root, "steamcmd.zip")
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	entry, err := writer.Create("../outside.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte("unsafe")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(archivePath, buffer.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(root, "stage")
	if err := os.MkdirAll(destination, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := extractSteamCMDArchive(archivePath, destination); err == nil {
		t.Fatal("expected unsafe ZIP path to be rejected")
	}
	if _, err := os.Stat(filepath.Join(root, "outside.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("archive escaped destination: %v", err)
	}
}

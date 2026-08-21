package palworld

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hearth/internal/config"
	modmodel "hearth/internal/mods"
	"hearth/internal/panel"
)

func TestInstallWorkshopPackageValidatesAndCommitsDisabledMod(t *testing.T) {
	root := t.TempDir()
	service := &Service{
		config:   config.GameConfig{InstallDir: root, ProcessName: "PalServer.exe"},
		platform: &fakePlatform{},
	}
	archive := modArchive(t, map[string]string{
		"3625223587/Info.json":     `{"PackageName":"UE4SSExperimentalPW","DisplayName":"UE4SS Experimental","Version":"1.2.3","InstallRules":[{"IsServer":true}]}`,
		"3625223587/Mods/main.lua": "return true",
	})
	inventory, err := service.InstallWorkshopPackage(context.Background(), modmodel.PackageInstallRequest{
		WorkshopID: "3625223587", FileName: "ue4ss.zip", Package: bytes.NewReader(archive), Confirm: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(inventory.Mods) != 1 {
		t.Fatalf("inventory = %#v", inventory)
	}
	installed := inventory.Mods[0]
	if installed.ID != "UE4SSExperimentalPW" || installed.Enabled || installed.Ownership != modmodel.OwnershipHearth ||
		installed.Source != modmodel.SourceSteamWorkshop || installed.Compatibility != modmodel.CompatibilitySupported {
		t.Fatalf("installed = %#v", installed)
	}
	if _, err := os.Stat(filepath.Join(root, "Mods", "Workshop", "3625223587", "Mods", "main.lua")); err != nil {
		t.Fatal(err)
	}
	settingsPath := filepath.Join(root, "Mods", "PalModSettings.ini")
	if _, err := os.Stat(settingsPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("install unexpectedly changed PalModSettings.ini: %v", err)
	}
}

func TestInstallWorkshopPackageRejectsUnsafeArchiveWithoutPartialTarget(t *testing.T) {
	root := t.TempDir()
	service := &Service{
		config:   config.GameConfig{InstallDir: root, ProcessName: "PalServer.exe"},
		platform: &fakePlatform{},
	}
	archive := modArchive(t, map[string]string{
		"../outside.txt": "escape",
		"Info.json":      `{"PackageName":"Unsafe","InstallRules":{"IsServer":true}}`,
	})
	_, err := service.InstallWorkshopPackage(context.Background(), modmodel.PackageInstallRequest{
		WorkshopID: "3767052724", FileName: "unsafe.zip", Package: bytes.NewReader(archive), Confirm: true,
	})
	if err == nil || !strings.Contains(err.Error(), "不安全路径") {
		t.Fatalf("error = %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "Mods", "Workshop", "3767052724")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("partial target exists: %v", statErr)
	}
}

func TestInstallWorkshopPackageRequiresStoppedServerAndServerCompatibility(t *testing.T) {
	root := t.TempDir()
	platform := &fakePlatform{running: true, pid: 7}
	service := &Service{config: config.GameConfig{InstallDir: root, ProcessName: "PalServer.exe"}, platform: platform}
	archive := modArchive(t, map[string]string{
		"Info.json": `{"PackageName":"ClientOnly","InstallRules":{"IsServer":false}}`,
	})
	request := modmodel.PackageInstallRequest{
		WorkshopID: "3767052724", FileName: "client.zip", Package: bytes.NewReader(archive), Confirm: true,
	}
	if _, err := service.InstallWorkshopPackage(context.Background(), request); !errors.Is(err, panel.ErrUnsafe) || !strings.Contains(err.Error(), "先停止") {
		t.Fatalf("running error = %v", err)
	}
	platform.running = false
	request.Package = bytes.NewReader(archive)
	if _, err := service.InstallWorkshopPackage(context.Background(), request); !errors.Is(err, panel.ErrInvalid) || !strings.Contains(err.Error(), "未允许服务端") {
		t.Fatalf("compatibility error = %v", err)
	}
}

func modArchive(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	for name, contents := range files {
		file, err := archive.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write([]byte(contents)); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

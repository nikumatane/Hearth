package palworld

import (
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"hearth/internal/panel"
)

func TestDetectActiveWorldUsesDedicatedServerName(t *testing.T) {
	installDir := t.TempDir()
	worldID := "E67C6D5A4D25543748EBC2BAB926DC80"
	worldDir := filepath.Join(installDir, "Pal", "Saved", "SaveGames", "0", worldID)
	configDir := filepath.Join(installDir, "Pal", "Saved", "Config", "WindowsServer")
	if err := os.MkdirAll(worldDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worldDir, "Level.sav"), []byte("level"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(configDir, "GameUserSettings.ini"),
		[]byte("[/Script/Pal.PalGameLocalSettings]\nDedicatedServerName="+worldID+"\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}

	world, err := detectActiveWorld(installDir)
	if err != nil {
		t.Fatalf("detectActiveWorld() error = %v", err)
	}
	if world.ID != worldID || world.Detection != "GameUserSettings.ini" {
		t.Fatalf("world = %#v", world)
	}
}

func TestDetectActiveWorldUsesOnlyValidDirectory(t *testing.T) {
	installDir := t.TempDir()
	worldID := "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	worldDir := filepath.Join(installDir, "Pal", "Saved", "SaveGames", "0", worldID)
	if err := os.MkdirAll(worldDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worldDir, "Level.sav"), []byte("level"), 0o600); err != nil {
		t.Fatal(err)
	}

	world, err := detectActiveWorld(installDir)
	if err != nil {
		t.Fatalf("detectActiveWorld() error = %v", err)
	}
	if world.ID != worldID || world.Detection != "唯一存档目录" {
		t.Fatalf("world = %#v", world)
	}
}

func TestValidateWorldOptionContainer(t *testing.T) {
	payload := []byte{1, 2, 3, 4}
	data := make([]byte, 12+len(payload))
	binary.LittleEndian.PutUint32(data[0:4], 16)
	binary.LittleEndian.PutUint32(data[4:8], uint32(len(payload)))
	copy(data[8:12], []byte("PlZ1"))
	copy(data[12:], payload)

	if err := validateWorldOptionContainer(data); err != nil {
		t.Fatalf("validateWorldOptionContainer() error = %v", err)
	}
	data[8] = 'X'
	if err := validateWorldOptionContainer(data); err == nil {
		t.Fatal("validateWorldOptionContainer() expected an invalid header error")
	}
}

func TestDetectActiveWorldRejectsAmbiguousDirectories(t *testing.T) {
	installDir := t.TempDir()
	for _, worldID := range []string{
		"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		"BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB",
	} {
		worldDir := filepath.Join(installDir, "Pal", "Saved", "SaveGames", "0", worldID)
		if err := os.MkdirAll(worldDir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(worldDir, "Level.sav"), []byte("level"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	_, err := detectActiveWorld(installDir)
	if !errors.Is(err, panel.ErrUnsafe) {
		t.Fatalf("detectActiveWorld() error = %v; want ErrUnsafe", err)
	}
}

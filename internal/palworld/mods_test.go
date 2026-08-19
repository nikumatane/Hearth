package palworld

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	modmodel "hearth/internal/mods"
)

func TestScanPalworldModsReturnsStableEmptyInventory(t *testing.T) {
	root := t.TempDir()
	before, err := directorySnapshot(root)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 19, 9, 0, 0, 0, time.UTC)
	inventory, err := scanPalworldMods(root, now)
	if err != nil {
		t.Fatal(err)
	}
	after, err := directorySnapshot(root)
	if err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatalf("empty mod scan changed the install directory\nbefore:\n%s\nafter:\n%s", before, after)
	}
	if inventory.GameID != palworldID || !inventory.Managed || inventory.ScannedAt != now || inventory.Revision == "" {
		t.Fatalf("inventory identity = %#v", inventory)
	}
	if len(inventory.Mods) != 0 || inventory.Mods == nil || len(inventory.Warnings) != 0 || inventory.Warnings == nil {
		t.Fatalf("empty inventory = %#v", inventory)
	}
}

func TestScanPalworldModsReadsOfficialLayoutWithoutMutatingFiles(t *testing.T) {
	root := t.TempDir()
	modsRoot := filepath.Join(root, "Mods")
	workshopRoot := filepath.Join(modsRoot, "Workshop")
	for path, contents := range map[string]string{
		filepath.Join(modsRoot, "PalModSettings.ini"): `[PalModSettings]
bGlobalEnableMod=true
ActiveModList=ServerTools
ActiveModList=MissingPackage
WorkshopRootDir=C:\External\Workshop
`,
		filepath.Join(workshopRoot, "3760000001", "Info.json"): `{
  "PackageName": "ServerTools",
  "DisplayName": "Server Tools",
  "Version": "2.1.0",
  "Dependencies": [{"PackageName":"CoreLibrary"},{"PackageName":"corelibrary"}],
  "InstallRules": [{"Type":"Paks","IsServer":true}]
}`,
		filepath.Join(workshopRoot, "3760000002", "Info.json"): `{
  "PackageName": "ClientOnly",
  "Name": "Client Only",
  "Version": 7,
  "InstallRule": {"IsServer":false}
}`,
		filepath.Join(workshopRoot, "broken", "Info.json"): `{not-json}`,
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	before, err := directorySnapshot(root)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	inventory, err := scanPalworldMods(root, now)
	if err != nil {
		t.Fatal(err)
	}
	after, err := directorySnapshot(root)
	if err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatalf("read-only scan changed the mod tree\nbefore:\n%s\nafter:\n%s", before, after)
	}
	if inventory.GameID != palworldID || !inventory.Managed || inventory.ScannedAt != now || inventory.Revision == "" {
		t.Fatalf("inventory identity = %#v", inventory)
	}
	if len(inventory.Mods) != 2 {
		t.Fatalf("mods = %#v; warnings = %#v", inventory.Mods, inventory.Warnings)
	}
	server := inventory.Mods[1]
	if server.ID != "ServerTools" || server.Name != "Server Tools" || server.Version != "2.1.0" ||
		!server.Enabled || server.Compatibility != modmodel.CompatibilitySupported ||
		server.Ownership != modmodel.OwnershipExternal || server.Source != modmodel.SourceOfficialPackage ||
		server.SourceReference != "Mods/Workshop/3760000001" ||
		len(server.Dependencies) != 1 || server.Dependencies[0] != "CoreLibrary" {
		t.Fatalf("server mod = %#v", server)
	}
	client := inventory.Mods[0]
	if client.ID != "ClientOnly" || client.Enabled || client.Version != "7" ||
		client.Compatibility != modmodel.CompatibilityUnsupported || len(client.Warnings) == 0 {
		t.Fatalf("client mod = %#v", client)
	}
	joinedWarnings := strings.Join(inventory.Warnings, "\n")
	for _, expected := range []string{"WorkshopRootDir", "MissingPackage", "Info.json 无效"} {
		if !strings.Contains(strings.ToLower(joinedWarnings), strings.ToLower(expected)) {
			t.Fatalf("warnings = %#v, missing %q", inventory.Warnings, expected)
		}
	}

	settingsPath := filepath.Join(modsRoot, "PalModSettings.ini")
	if err := os.WriteFile(settingsPath, []byte("[PalModSettings]\nbGlobalEnableMod=false\nActiveModList=ServerTools\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	updated, err := scanPalworldMods(root, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if updated.Revision == inventory.Revision || updated.Mods[1].Enabled {
		t.Fatalf("updated inventory = %#v", updated)
	}
}

func TestScanPalworldModsRejectsUnsafeInfoAndReportsUnknownCompatibility(t *testing.T) {
	root := t.TempDir()
	workshopRoot := filepath.Join(root, "Mods", "Workshop")
	for directory, contents := range map[string]string{
		"safe":   `{"PackageName":"SafePackage","Version":"1"}`,
		"unsafe": `{"PackageName":"../escape","InstallRules":{"IsServer":true}}`,
	} {
		path := filepath.Join(workshopRoot, directory, "Info.json")
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	inventory, err := scanPalworldMods(root, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(inventory.Mods) != 1 || inventory.Mods[0].ID != "SafePackage" ||
		inventory.Mods[0].Compatibility != modmodel.CompatibilityUnknown || len(inventory.Mods[0].Warnings) == 0 {
		t.Fatalf("inventory = %#v", inventory)
	}
	if !strings.Contains(strings.Join(inventory.Warnings, "\n"), "PackageName") {
		t.Fatalf("warnings = %#v", inventory.Warnings)
	}
}

func TestParsePalModSettingsRejectsOverlongLine(t *testing.T) {
	data := []byte("[PalModSettings]\n" + strings.Repeat("x", 70<<10) + "\nActiveModList=HiddenAfterTruncation\n")
	globalEnabled, active, _, err := parsePalModSettings(data)
	if err == nil || globalEnabled || len(active) != 0 {
		t.Fatalf("parsePalModSettings() = enabled %v, active %#v, error %v", globalEnabled, active, err)
	}
}

func TestPathWithinModRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "Workshop")
	if !pathWithinModRoot(root, filepath.Join(root, "3760000001")) {
		t.Fatal("direct Workshop child was rejected")
	}
	if pathWithinModRoot(root, filepath.Join(filepath.Dir(root), "outside")) {
		t.Fatal("path outside Workshop root was accepted")
	}
}

func directorySnapshot(root string) (string, error) {
	lines := []string{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		line := filepath.ToSlash(relative)
		if !entry.IsDir() {
			info, err := entry.Info()
			if err != nil {
				return err
			}
			line += ":" + info.ModTime().UTC().Format(time.RFC3339Nano) + ":" + strconv.FormatInt(info.Size(), 10)
		}
		lines = append(lines, line)
		return nil
	})
	return strings.Join(lines, "\n"), err
}

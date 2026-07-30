package palworld

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hearth/internal/panel"
)

const testSettings = `[/Script/Pal.PalGameWorldSettings]
OptionSettings=(ServerName="Four, Friends",AdminPassword="very-secret",ServerPassword="join-secret",ExpRate=1.000000,RESTAPIEnabled=True,CrossplayPlatforms=(Steam,Xbox,PS5,Mac),DenyTechnologyList=("PALBOX","RepairBench"),FutureOption=42)
`

func TestParseINIPreservesNestedAndQuotedCommas(t *testing.T) {
	document, err := parseINI(testSettings)
	if err != nil {
		t.Fatalf("parseINI() error = %v", err)
	}
	values := document.values()
	if values["ServerName"] != `"Four, Friends"` {
		t.Fatalf("ServerName = %q", values["ServerName"])
	}
	if values["CrossplayPlatforms"] != "(Steam,Xbox,PS5,Mac)" {
		t.Fatalf("CrossplayPlatforms = %q", values["CrossplayPlatforms"])
	}
	if values["DenyTechnologyList"] != `("PALBOX","RepairBench")` {
		t.Fatalf("DenyTechnologyList = %q", values["DenyTechnologyList"])
	}
	if rendered := document.render(); rendered != testSettings {
		t.Fatalf("rendered settings changed\nwant: %q\n got: %q", testSettings, rendered)
	}
}

func TestReadSettingsRedactsSecretsAndIncludesUnknownOptions(t *testing.T) {
	path := writeTestSettings(t, testSettings)
	settings, err := readPalworldSettings(path, "")
	if err != nil {
		t.Fatalf("readPalworldSettings() error = %v", err)
	}
	if strings.Contains(settings.Raw, "very-secret") || strings.Contains(settings.Raw, "join-secret") {
		t.Fatal("raw settings exposed a password")
	}
	if !strings.Contains(settings.Raw, secretMask) {
		t.Fatal("raw settings did not contain the secret placeholder")
	}
	if settingByKey(settings, "FutureOption") == nil {
		t.Fatal("future option was not exposed through the dynamic 1.0 group")
	}
}

func TestWriteSettingsPreservesSecretsAndUnknownOptions(t *testing.T) {
	path := writeTestSettings(t, testSettings)
	settings, err := readPalworldSettings(path, "")
	if err != nil {
		t.Fatal(err)
	}
	expRate := settingByKey(settings, "ExpRate")
	if expRate == nil {
		t.Fatal("ExpRate is missing")
	}
	expRate.Value = 2.5

	updated, err := writePalworldSettings(path, "", settings, true)
	if err != nil {
		t.Fatalf("writePalworldSettings() error = %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, expected := range []string{
		`AdminPassword="very-secret"`,
		`ServerPassword="join-secret"`,
		"ExpRate=2.5",
		"FutureOption=42",
		"CrossplayPlatforms=(Steam,Xbox,PS5,Mac)",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("updated file does not contain %q: %s", expected, text)
		}
	}
	if strings.Contains(updated.Raw, "very-secret") {
		t.Fatal("updated response exposed AdminPassword")
	}
	backups, err := filepath.Glob(path + ".panel-backup-*")
	if err != nil || len(backups) != 1 {
		t.Fatalf("backup files = %v, error = %v", backups, err)
	}
}

func TestWriteSettingsRejectsPasswordChangeWhileRunning(t *testing.T) {
	path := writeTestSettings(t, testSettings)
	settings, err := readPalworldSettings(path, "")
	if err != nil {
		t.Fatal(err)
	}
	adminPassword := settingByKey(settings, "AdminPassword")
	if adminPassword == nil {
		t.Fatal("AdminPassword is missing")
	}
	adminPassword.Value = "replacement"

	_, err = writePalworldSettings(path, "", settings, true)
	if !errors.Is(err, panel.ErrUnsafe) {
		t.Fatalf("writePalworldSettings() error = %v", err)
	}
	raw, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(raw) != testSettings {
		t.Fatal("settings changed after rejected password update")
	}
}

func TestDefaultFileAddsNewVersionOptionsWithoutLosingCurrentValues(t *testing.T) {
	directory := t.TempDir()
	currentPath := filepath.Join(directory, "PalWorldSettings.ini")
	defaultPath := filepath.Join(directory, "DefaultPalWorldSettings.ini")
	current := `OptionSettings=(AdminPassword="secret",ServerName="Existing")`
	defaults := `OptionSettings=(AdminPassword="",ServerName="Default",NewInOnePointZero=True)`
	if err := os.WriteFile(currentPath, []byte(current), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(defaultPath, []byte(defaults), 0o600); err != nil {
		t.Fatal(err)
	}

	settings, err := readPalworldSettings(currentPath, defaultPath)
	if err != nil {
		t.Fatal(err)
	}
	newSetting := settingByKey(settings, "NewInOnePointZero")
	if newSetting == nil || newSetting.Value != true {
		t.Fatalf("new default setting = %#v", newSetting)
	}
	if _, err := writePalworldSettings(currentPath, defaultPath, settings, false); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(currentPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if !strings.Contains(text, "NewInOnePointZero=True") {
		t.Fatalf("new option was not added: %s", text)
	}
	if !strings.Contains(text, `ServerName="Existing"`) {
		t.Fatalf("current option was overwritten by default: %s", text)
	}
}

func TestParseINIRejectsDuplicateKeys(t *testing.T) {
	_, err := parseINI("OptionSettings=(ExpRate=1,ExpRate=2)")
	if err == nil {
		t.Fatal("parseINI() expected duplicate-key error")
	}
}

func writeTestSettings(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "PalWorldSettings.ini")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func settingByKey(settings panel.PalworldSettings, key string) *panel.Setting {
	for groupIndex := range settings.Groups {
		for settingIndex := range settings.Groups[groupIndex].Settings {
			setting := &settings.Groups[groupIndex].Settings[settingIndex]
			if setting.Key == key {
				return setting
			}
		}
	}
	return nil
}

func TestRenderManagementSettingsOnlySynchronizesAllowedKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "PalWorldSettings.ini")
	raw := `[/Script/Pal.PalGameWorldSettings]
OptionSettings=(AdminPassword="old",RESTAPIEnabled=False,RESTAPIPort=8212,ExpRate=1.000000)`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}

	rendered, err := renderManagementSettings(path, map[string]any{
		"AdminPassword":  "new-secret",
		"RESTAPIEnabled": true,
		"ExpRate":        20,
	})
	if err != nil {
		t.Fatalf("renderManagementSettings() error = %v", err)
	}
	document, err := parseINI(string(rendered))
	if err != nil {
		t.Fatal(err)
	}
	values := document.values()
	if values["AdminPassword"] != `"new-secret"` || values["RESTAPIEnabled"] != "True" {
		t.Fatalf("management values = %#v", values)
	}
	if values["ExpRate"] != "1.000000" {
		t.Fatalf("ExpRate was unexpectedly synchronized: %q", values["ExpRate"])
	}
}

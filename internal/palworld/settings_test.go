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
	for _, test := range []struct {
		key  string
		want bool
	}{
		{key: "ServerName", want: true},
		{key: "ExpRate", want: true},
		{key: "DenyTechnologyList", want: true},
		{key: "AdminPassword", want: false},
		{key: "RESTAPIEnabled", want: false},
		{key: "CrossplayPlatforms", want: false},
		{key: "FutureOption", want: false},
	} {
		setting := settingByKey(settings, test.key)
		if setting == nil || setting.MemberEditable != test.want {
			t.Fatalf("%s memberEditable = %#v; want %v", test.key, setting, test.want)
		}
	}
}

func TestReadSettingsDoesNotMaskEmptyPasswords(t *testing.T) {
	path := writeTestSettings(t, `OptionSettings=(AdminPassword="",ServerPassword="")`)
	settings, err := readPalworldSettings(path, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"AdminPassword", "ServerPassword"} {
		setting := settingByKey(settings, key)
		if setting == nil || setting.Value != "" {
			t.Fatalf("%s = %#v", key, setting)
		}
	}
}

func TestPatchSettingsPreservesSecretsAndUnknownOptions(t *testing.T) {
	path := writeTestSettings(t, testSettings)
	settings, err := readPalworldSettings(path, "")
	if err != nil {
		t.Fatal(err)
	}
	updated, err := patchPalworldSettings(path, "", panel.PalworldSettingsPatch{
		Revision: settings.Revision,
		Changes:  map[string]any{"ExpRate": 2.5},
	})
	if err != nil {
		t.Fatalf("patchPalworldSettings() error = %v", err)
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
	if updated.Revision == settings.Revision {
		t.Fatal("settings revision did not change")
	}
	backups, err := filepath.Glob(path + ".panel-backup-*")
	if err != nil || len(backups) != 1 {
		t.Fatalf("backup files = %v, error = %v", backups, err)
	}
}

func TestPatchSettingsRejectsStaleRevisionAndMaskedPassword(t *testing.T) {
	path := writeTestSettings(t, testSettings)
	settings, err := readPalworldSettings(path, "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = patchPalworldSettings(path, "", panel.PalworldSettingsPatch{
		Revision: "stale",
		Changes:  map[string]any{"ExpRate": 2},
	})
	if !errors.Is(err, panel.ErrUnsafe) {
		t.Fatalf("stale revision error = %v", err)
	}
	_, err = patchPalworldSettings(path, "", panel.PalworldSettingsPatch{
		Revision: settings.Revision,
		Changes:  map[string]any{"AdminPassword": secretMask},
	})
	if !errors.Is(err, panel.ErrInvalid) {
		t.Fatalf("masked password error = %v", err)
	}
	raw, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(raw) != testSettings {
		t.Fatal("settings changed after rejected patches")
	}
}

func TestPatchSettingsRejectsRawTextOptionInjection(t *testing.T) {
	path := writeTestSettings(t, testSettings)
	settings, err := readPalworldSettings(path, "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = patchPalworldSettings(path, "", panel.PalworldSettingsPatch{
		Revision: settings.Revision,
		Changes: map[string]any{
			"DenyTechnologyList": `("PALBOX"),InjectedSystemOption=True`,
		},
	})
	if !errors.Is(err, panel.ErrInvalid) {
		t.Fatalf("raw option injection error = %v", err)
	}
	raw, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if strings.Contains(string(raw), "InjectedSystemOption") {
		t.Fatalf("raw option injection changed settings: %s", raw)
	}
	if _, err := patchPalworldSettings(path, "", panel.PalworldSettingsPatch{
		Revision: settings.Revision,
		Changes:  map[string]any{"DenyTechnologyList": `("RepairBench")`},
	}); err != nil {
		t.Fatalf("valid composite value was rejected: %v", err)
	}
}

func TestPatchSettingsCanAddKnownDefaultWithoutLosingCurrentValues(t *testing.T) {
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
	if currentSetting := settingByKey(settings, "ServerName"); currentSetting == nil || !currentSetting.Configured {
		t.Fatalf("current setting source state = %#v", currentSetting)
	}
	if defaultSetting := settingByKey(settings, "NewInOnePointZero"); defaultSetting == nil || defaultSetting.Configured {
		t.Fatalf("default-only setting source state = %#v", defaultSetting)
	}
	if _, err := patchPalworldSettings(currentPath, defaultPath, panel.PalworldSettingsPatch{
		Revision: settings.Revision,
		Changes:  map[string]any{"NewInOnePointZero": true},
	}); err != nil {
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
		t.Fatalf("current option was overwritten: %s", text)
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

func TestPatchSettingsOnlyChangesRequestedKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "PalWorldSettings.ini")
	raw := `[/Script/Pal.PalGameWorldSettings]
OptionSettings=(AdminPassword="old",ServerPassword="keep-me",RESTAPIEnabled=False,RESTAPIPort=8212,ExpRate=1.000000)`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	settings, err := readPalworldSettings(path, "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = patchPalworldSettings(path, "", panel.PalworldSettingsPatch{
		Revision: settings.Revision,
		Changes: map[string]any{
			"AdminPassword": "new-secret",
			"RCONEnabled":   true,
		},
	})
	if err != nil {
		t.Fatalf("patchPalworldSettings() error = %v", err)
	}
	updatedRaw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	document, err := parseINI(string(updatedRaw))
	if err != nil {
		t.Fatal(err)
	}
	values := document.values()
	if values["AdminPassword"] != `"new-secret"` || values["RCONEnabled"] != "True" {
		t.Fatalf("patched values = %#v", values)
	}
	if values["ServerPassword"] != `"keep-me"` || values["RESTAPIEnabled"] != "False" || values["ExpRate"] != "1.000000" {
		t.Fatalf("untouched values changed: %#v", values)
	}
}

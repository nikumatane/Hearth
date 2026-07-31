package palworld

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hearth/internal/config"
)

func TestParsePublicBuildID(t *testing.T) {
	output := `Redirecting stderr to 'C:\steamcmd\logs\stderr.txt'
AppInfo update complete
"2394010"
{
	"common"
	{
		"name"		"Palworld Dedicated Server"
	}
	"depots"
	{
		"2394011"
		{
			"manifests"
			{
				"public"
				{
					"gid"		"999999999999999999"
				}
			}
		}
		"branches"
		{
			"public"
			{
				"buildid"		"24681357"
				"timeupdated"		"1780000000"
			}
		}
	}
}`
	buildID, err := parsePublicBuildID(bufio.NewScanner(strings.NewReader(output)), palworldAppID)
	if err != nil {
		t.Fatalf("parsePublicBuildID() error = %v", err)
	}
	if buildID != "24681357" {
		t.Fatalf("buildID = %q; want 24681357", buildID)
	}
}

func TestParsePublicBuildIDIgnoresOtherAppsAndDepots(t *testing.T) {
	output := `"100"
{
	"depots"
	{
		"branches"
		{
			"public"
			{
				"buildid" "111"
			}
		}
	}
}
"2394010"
{
	"depots"
	{
		"2394011"
		{
			"buildid" "222"
		}
		"branches"
		{
			"public"
			{
				"buildid" "333"
			}
		}
	}
}`
	buildID, err := parsePublicBuildID(bufio.NewScanner(strings.NewReader(output)), palworldAppID)
	if err != nil {
		t.Fatalf("parsePublicBuildID() error = %v", err)
	}
	if buildID != "333" {
		t.Fatalf("buildID = %q; want 333", buildID)
	}
}

func TestCompareSteamBuilds(t *testing.T) {
	current, err := compareSteamBuilds("24681357", "24681357")
	if err != nil || current.State != versionCheckCurrent || current.UpdateAvailable {
		t.Fatalf("current status = %#v, error = %v", current, err)
	}

	available, err := compareSteamBuilds("24681357", "24681358")
	if err != nil {
		t.Fatalf("available comparison error = %v", err)
	}
	if available.State != versionCheckAvailable ||
		!available.UpdateAvailable ||
		available.AvailableVersion != "24681358" {
		t.Fatalf("available status = %#v", available)
	}

	if _, err := compareSteamBuilds("24681358", "24681357"); err == nil {
		t.Fatal("older public build was accepted as a valid current status")
	}
}

func TestVersionStatusResetsWhenInstalledBuildChanges(t *testing.T) {
	service := &Service{}
	service.setVersionStatus("100", steamVersionStatus{
		State:            versionCheckAvailable,
		AvailableVersion: "101",
		UpdateAvailable:  true,
	})
	if got := service.versionStatusForBuild("100"); !got.UpdateAvailable {
		t.Fatalf("cached status = %#v; want update available", got)
	}
	if got := service.versionStatusForBuild("101"); got.State != versionCheckUnchecked {
		t.Fatalf("status after installed build changed = %#v; want unchecked", got)
	}
}

func TestInstalledBuildIDReadsSteamManifest(t *testing.T) {
	root := t.TempDir()
	installDir := filepath.Join(root, "steamapps", "common", "PalServer")
	if err := os.MkdirAll(installDir, 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	manifestPath := filepath.Join(root, "steamapps", "appmanifest_"+palworldAppID+".acf")
	manifest := `"AppState"
{
	"appid"		"2394010"
	"buildid"		"24681357"
}`
	if err := os.WriteFile(manifestPath, []byte(manifest), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	service := &Service{config: config.GameConfig{InstallDir: installDir}}

	if got := service.installedBuildID(); got != "24681357" {
		t.Fatalf("installedBuildID() = %q; want 24681357", got)
	}
}

package palworld

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"hearth/internal/config"
)

func TestCheckVersionPreparesSteamCMDBeforeQueryingPalworld(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test helper uses a POSIX shell script")
	}
	root := t.TempDir()
	installDir := filepath.Join(root, "steamapps", "common", "PalServer")
	if err := os.MkdirAll(installDir, 0o700); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(root, "steamapps", "appmanifest_"+palworldAppID+".acf")
	if err := os.WriteFile(manifestPath, []byte(`"AppState"
{
	"buildid" "100"
	"MountedDepots"
	{
		"2394011" "999"
	}
}`), 0o600); err != nil {
		t.Fatal(err)
	}
	counterPath := filepath.Join(root, "attempts")
	steamCmd := filepath.Join(root, "steamcmd")
	script := fmt.Sprintf(`#!/bin/sh
count=0
if [ -f %q ]; then count=$(cat %q); fi
count=$((count + 1))
printf "%%s" "$count" > %q
case "$*" in
  *app_info_print*)
    printf '"2394010"\n{\n"depots"\n{\n"2394011"\n{\n"manifests"\n{\n"public"\n{\n"gid" "1000"\n}\n}\n}\n"branches"\n{\n"public"\n{\n"buildid" "101"\n}\n}\n}\n}\n'
    ;;
esac
`, counterPath, counterPath, counterPath)
	if err := os.WriteFile(steamCmd, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	service := &Service{config: config.GameConfig{InstallDir: installDir, SteamCmd: steamCmd}}
	if err := service.checkVersion(func(string, int, string) {}, func(string, string) {}); err != nil {
		t.Fatalf("checkVersion() error = %v", err)
	}
	attempts, err := os.ReadFile(counterPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(attempts) != "2" {
		t.Fatalf("SteamCMD attempts = %q; want preparation plus query", attempts)
	}
	status := service.versionStatusForBuild("100")
	if !status.UpdateAvailable {
		t.Fatalf("version status = %#v", status)
	}
}

func TestVersionCheckDueHonorsSuccessfulCheckInterval(t *testing.T) {
	service := &Service{}
	if !service.versionCheckDue() {
		t.Fatal("new service should need a version check")
	}
	service.setVersionStatus("100", steamVersionStatus{State: versionCheckChecking})
	service.setVersionStatus("100", steamVersionStatus{State: versionCheckUnavailable})
	if service.versionCheckDue() {
		t.Fatal("recent failed check should wait before retrying")
	}
	service.versionMu.Lock()
	service.versionAttemptedAt = time.Now().Add(-automaticVersionCheckFailureRetry)
	service.versionMu.Unlock()
	if !service.versionCheckDue() {
		t.Fatal("failed check should retry after the failure interval")
	}
	service.setVersionStatus("100", steamVersionStatus{State: versionCheckCurrent})
	if service.versionCheckDue() {
		t.Fatal("recent successful check should not be due")
	}
	service.versionMu.Lock()
	service.versionCheckedAt = time.Now().Add(-automaticVersionCheckInterval)
	service.versionMu.Unlock()
	if !service.versionCheckDue() {
		t.Fatal("stale successful check should be due")
	}
}

func TestInvalidInstalledBuildRecordsVersionAttempt(t *testing.T) {
	service := &Service{}
	err := service.checkVersion(func(string, int, string) {}, func(string, string) {})
	if err == nil || !strings.Contains(err.Error(), "installed Steam build ID") {
		t.Fatalf("checkVersion() error = %v", err)
	}
	if service.versionCheckDue() {
		t.Fatal("invalid local manifest should still respect failure retry interval")
	}
}

func TestSteamVersionCommandUsesNoProgressTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test helper uses a POSIX shell script")
	}
	directory := t.TempDir()
	steamCmd := filepath.Join(directory, "steamcmd")
	if err := os.WriteFile(steamCmd, []byte("#!/bin/sh\nsleep 10\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(directory, "steamcmd.log")
	logFile, err := os.OpenFile(logPath, os.O_WRONLY|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer logFile.Close()
	service := &Service{config: config.GameConfig{SteamCmd: steamCmd}}
	started := time.Now()
	err = service.runSteamVersionCommand(logFile, 150*time.Millisecond, "+quit")
	if err == nil || !strings.Contains(err.Error(), "no log progress") {
		t.Fatalf("runSteamVersionCommand() error = %v", err)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("no-progress termination took %v", elapsed)
	}
}

func TestSteamVersionCommandAllowsLongRunningLogProgress(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test helper uses a POSIX shell script")
	}
	directory := t.TempDir()
	steamCmd := filepath.Join(directory, "steamcmd")
	script := "#!/bin/sh\nfor value in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15; do echo $value; sleep 0.1; done\n"
	if err := os.WriteFile(steamCmd, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(directory, "steamcmd.log")
	logFile, err := os.OpenFile(logPath, os.O_WRONLY|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer logFile.Close()
	service := &Service{config: config.GameConfig{SteamCmd: steamCmd}}
	started := time.Now()
	if err := service.runSteamVersionCommand(logFile, time.Second, "+quit"); err != nil {
		t.Fatalf("runSteamVersionCommand() error = %v", err)
	}
	if elapsed := time.Since(started); elapsed < 1200*time.Millisecond {
		t.Fatalf("command did not run long enough to exercise progress timeout: %v", elapsed)
	}
}

func TestParsePublicDepotManifests(t *testing.T) {
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
	manifests, err := parsePublicDepotManifests(bufio.NewScanner(strings.NewReader(output)), palworldAppID)
	if err != nil {
		t.Fatalf("parsePublicDepotManifests() error = %v", err)
	}
	if manifests["2394011"] != "999999999999999999" {
		t.Fatalf("manifests = %#v", manifests)
	}
}

func TestParseInstalledDepotManifests(t *testing.T) {
	manifest := `"AppState"
{
	"buildid" "333"
	"InstalledDepots"
	{
		"2394011"
		{
			"manifest" "777"
		}
	}
}`
	manifests, err := parseInstalledDepotManifests(bufio.NewScanner(strings.NewReader(manifest)))
	if err != nil {
		t.Fatalf("parseInstalledDepotManifests() error = %v", err)
	}
	if manifests["2394011"] != "777" {
		t.Fatalf("manifests = %#v", manifests)
	}
}

func TestCompareSteamDepotManifestsIgnoresUnrelatedAppBuildChange(t *testing.T) {
	current, err := compareSteamDepotManifests(
		map[string]string{"2394011": "777"},
		map[string]string{"2394011": "777", "100": "999"},
	)
	if err != nil || current.State != versionCheckCurrent || current.UpdateAvailable {
		t.Fatalf("current status = %#v, error = %v", current, err)
	}

	available, err := compareSteamDepotManifests(
		map[string]string{"2394011": "777"},
		map[string]string{"2394011": "778"},
	)
	if err != nil {
		t.Fatalf("available comparison error = %v", err)
	}
	if available.State != versionCheckAvailable || !available.UpdateAvailable {
		t.Fatalf("available status = %#v", available)
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

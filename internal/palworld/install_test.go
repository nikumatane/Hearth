//go:build !windows

package palworld

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestInstallDedicatedServerRetriesAfterSelfUpdateExitError(t *testing.T) {
	directory := t.TempDir()
	counterPath := filepath.Join(directory, "attempts")
	steamCmd := filepath.Join(directory, "steamcmd.exe")
	script := fmt.Sprintf(`#!/bin/sh
count=0
if [ -f %q ]; then count=$(cat %q); fi
count=$((count + 1))
printf "%%s" "$count" > %q
if [ "$count" -eq 1 ]; then
  echo "[----] Update complete, launching Steamcmd..."
  exit 7
fi
echo "Success! App '2394010' fully installed."
`, counterPath, counterPath, counterPath)
	if err := os.WriteFile(steamCmd, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(directory, "install.log")
	if err := InstallDedicatedServer(
		steamCmd, logPath, 5*time.Second, nil,
	); err != nil {
		t.Fatalf("InstallDedicatedServer() error = %v", err)
	}
	attempts, err := os.ReadFile(counterPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(attempts) != "2" {
		t.Fatalf("attempts = %q", attempts)
	}
}

func TestInstallDedicatedServerRequiresPalworldCompletionMarker(t *testing.T) {
	directory := t.TempDir()
	steamCmd := filepath.Join(directory, "steamcmd.exe")
	script := "#!/bin/sh\necho \"Success! App '2394010' fully installed.\"\n"
	if err := os.WriteFile(steamCmd, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(directory, "install.log")
	stages := make([]string, 0, 4)
	err := InstallDedicatedServer(
		steamCmd, logPath, 5*time.Second,
		func(stage string, _ int, _ string) { stages = append(stages, stage) },
	)
	if err != nil {
		t.Fatalf("InstallDedicatedServer() error = %v", err)
	}
	if len(stages) == 0 || stages[len(stages)-1] != "下载完成" {
		t.Fatalf("stages = %#v", stages)
	}
}

func TestInstallDedicatedServerRejectsCleanExitWithoutMarker(t *testing.T) {
	directory := t.TempDir()
	steamCmd := filepath.Join(directory, "steamcmd.exe")
	if err := os.WriteFile(steamCmd, []byte("#!/bin/sh\necho done\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	err := InstallDedicatedServer(
		steamCmd, filepath.Join(directory, "install.log"), 5*time.Second, nil,
	)
	if err == nil {
		t.Fatal("InstallDedicatedServer() accepted output without an app completion marker")
	}
}

func TestInstallDedicatedServerUsesSteamCMDDefaultLibrary(t *testing.T) {
	directory := t.TempDir()
	steamCmd := filepath.Join(directory, "steamcmd.exe")
	argumentsPath := filepath.Join(directory, "arguments")
	script := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' \"$@\" > %q\necho \"Success! App '2394010' fully installed.\"\n", argumentsPath)
	if err := os.WriteFile(steamCmd, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := InstallDedicatedServer(
		steamCmd, filepath.Join(directory, "install.log"), 5*time.Second, nil,
	); err != nil {
		t.Fatalf("InstallDedicatedServer() error = %v", err)
	}
	arguments, err := os.ReadFile(argumentsPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(arguments), "+force_install_dir") {
		t.Fatalf("SteamCMD arguments unexpectedly override the default library: %q", arguments)
	}
}

func TestInstallProgressFromSteamLog(t *testing.T) {
	tests := []struct {
		name string
		line string
		want int
		ok   bool
	}{
		{name: "SteamCMD bootstrap", line: "[ 41%] Downloading update...", want: 33, ok: true},
		{name: "game download", line: "Update state (0x61) downloading, progress: 89.03", want: 81, ok: true},
		{name: "game complete", line: "Update state downloading, progress: 100.00", want: 85, ok: true},
		{name: "unknown", line: "Waiting for client config...OK", ok: false},
		{name: "invalid percent", line: "[ 101%] Downloading update...", ok: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := installProgressFromSteamLog(test.line)
			if got != test.want || ok != test.ok {
				t.Fatalf("installProgressFromSteamLog(%q) = %d, %v; want %d, %v", test.line, got, ok, test.want, test.ok)
			}
		})
	}
}

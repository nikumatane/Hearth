//go:build !windows

package steamapp

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestInstallDedicatedServerUsesRequestedAppAndExactCompletionMarker(t *testing.T) {
	directory := t.TempDir()
	steamCmd := filepath.Join(directory, "steamcmd.exe")
	script := `#!/bin/sh
if [ "$4" != "343050" ]; then exit 4; fi
echo "Success! App '2394010' fully installed."
echo "Success! App '343050' already up to date."
`
	if err := os.WriteFile(steamCmd, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	err := InstallDedicatedServer(
		steamCmd, filepath.Join(directory, "install.log"), 5*time.Second,
		InstallSpec{AppID: "343050", ProductName: "DST"}, nil,
	)
	if err != nil {
		t.Fatalf("InstallDedicatedServer() error = %v", err)
	}
}

func TestInstallDedicatedServerRejectsAnotherAppCompletionMarker(t *testing.T) {
	directory := t.TempDir()
	steamCmd := filepath.Join(directory, "steamcmd.exe")
	if err := os.WriteFile(steamCmd, []byte("#!/bin/sh\necho \"Success! App '2394010' fully installed.\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	err := InstallDedicatedServer(
		steamCmd, filepath.Join(directory, "install.log"), 5*time.Second,
		InstallSpec{AppID: "343050", ProductName: "DST"}, nil,
	)
	if err == nil {
		t.Fatal("completion marker for another app was accepted")
	}
}

//go:build windows

package gamemanager

import (
	"os"
	"path/filepath"
	"strings"
)

func discoverWindowsDSTRoots() []string {
	drive := strings.TrimSpace(os.Getenv("SystemDrive"))
	if drive == "" {
		drive = "C:"
	}
	usersRoot := filepath.Join(drive+string(filepath.Separator), "Users")
	entries, err := os.ReadDir(usersRoot)
	if err != nil {
		return nil
	}
	roots := make([]string, 0, len(entries)*2)
	for _, entry := range entries {
		if !entry.IsDir() || isWindowsProfileNoise(entry.Name()) {
			continue
		}
		profile := filepath.Join(usersRoot, entry.Name())
		oneDriveProfiles := make([]string, 0, 2)
		if profileEntries, readErr := os.ReadDir(profile); readErr == nil {
			for _, profileEntry := range profileEntries {
				if profileEntry.IsDir() && strings.HasPrefix(strings.ToLower(profileEntry.Name()), "onedrive") {
					oneDriveProfiles = append(oneDriveProfiles, filepath.Join(profile, profileEntry.Name()))
				}
			}
		}
		roots = append(roots, dstProfileRoots(profile, oneDriveProfiles)...)
	}
	return roots
}

func isWindowsProfileNoise(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "all users", "default", "default user", "public":
		return true
	default:
		return false
	}
}

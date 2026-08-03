package gamemanager

import (
	"crypto/sha256"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"hearth/internal/panel"
)

const (
	maxDiscoveryDepth       = 5
	maxDiscoveryDirectories = 1500
	maxDiscoveryCandidates  = 32
)

func (s *Service) discoverLocked() {
	roots := s.discoveryRootsLocked()
	steamCommands := discoverSteamCommands(roots, s.config.Management.SteamCmdRoot, s.config.Games.Palworld.SteamCmd)
	palworldDirectories := make(map[string]string)
	dstDirectories := make(map[string]string)

	addPalworld := func(directory string) {
		directory = filepath.Clean(directory)
		if fileExists(filepath.Join(directory, "PalServer.exe")) {
			palworldDirectories[strings.ToLower(directory)] = directory
		}
	}
	addDST := func(executable string) {
		if fileExists(executable) {
			directory := filepath.Dir(filepath.Dir(executable))
			dstDirectories[strings.ToLower(directory)] = directory
		}
	}

	addPalworld(s.config.Games.Palworld.InstallDir)
	for _, root := range roots {
		for _, candidate := range []string{
			root,
			filepath.Join(root, "PalServer"),
			filepath.Join(root, "steamapps", "common", "PalServer"),
		} {
			addPalworld(candidate)
		}
		for _, executable := range []string{
			filepath.Join(root, "bin64", "dontstarve_dedicated_server_nullrenderer_x64.exe"),
			filepath.Join(root, "Don't Starve Together Dedicated Server", "bin64", "dontstarve_dedicated_server_nullrenderer_x64.exe"),
			filepath.Join(root, "steamapps", "common", "Don't Starve Together Dedicated Server", "bin64", "dontstarve_dedicated_server_nullrenderer_x64.exe"),
		} {
			addDST(executable)
		}
	}

	for _, root := range s.config.Management.DiscoveryRoots {
		walkDiscoveryRoot(root, func(path string, entry fs.DirEntry) bool {
			if strings.EqualFold(entry.Name(), "PalServer.exe") {
				addPalworld(filepath.Dir(path))
				return len(palworldDirectories)+len(dstDirectories) < maxDiscoveryCandidates
			}
			if strings.EqualFold(entry.Name(), "dontstarve_dedicated_server_nullrenderer_x64.exe") {
				addDST(path)
			}
			return len(palworldDirectories)+len(dstDirectories) < maxDiscoveryCandidates
		})
	}

	s.candidates[palworldID] = make([]panel.GameCandidate, 0, len(palworldDirectories))
	for _, directory := range palworldDirectories {
		steamCmd := closestSteamCommand(directory, steamCommands)
		settings := filepath.Join(directory, "Pal", "Saved", "Config", "WindowsServer", "PalWorldSettings.ini")
		settingsPresent := fileExists(settings)
		detail := "PalServer.exe 已确认"
		if !settingsPresent {
			detail += "；缺少 PalWorldSettings.ini，暂不自动修改"
		}
		if steamCmd == "" {
			detail += "；未找到 steamcmd.exe"
		}
		s.candidates[palworldID] = append(s.candidates[palworldID], panel.GameCandidate{
			ID: candidateID(palworldID, directory), InstallDir: directory, SteamCmd: steamCmd,
			SettingsPresent: settingsPresent, Detail: detail,
		})
	}
	s.candidates[dstID] = make([]panel.GameCandidate, 0, len(dstDirectories))
	for _, directory := range dstDirectories {
		s.candidates[dstID] = append(s.candidates[dstID], panel.GameCandidate{
			ID: candidateID(dstID, directory), InstallDir: directory,
			SteamCmd: closestSteamCommand(directory, steamCommands),
			Detail:   "已识别 DST Dedicated Server；1.3.0 才开放接管",
		})
	}
	for id := range s.candidates {
		sort.Slice(s.candidates[id], func(left, right int) bool {
			return strings.ToLower(s.candidates[id][left].InstallDir) < strings.ToLower(s.candidates[id][right].InstallDir)
		})
	}
}

func (s *Service) discoveryRootsLocked() []string {
	roots := append([]string(nil), s.config.Management.DiscoveryRoots...)
	for _, value := range []string{
		s.config.Management.InstallRoot,
		s.config.Management.SteamCmdRoot,
		s.config.Games.Palworld.InstallDir,
		filepath.Dir(s.config.Games.Palworld.InstallDir),
	} {
		if strings.TrimSpace(value) != "" {
			roots = append(roots, value)
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		roots = append(roots, filepath.Join(home, "Downloads", "steamcmd"))
	}
	if runtime.GOOS == "windows" {
		for _, drive := range []string{"C:", "D:", "E:"} {
			for _, leaf := range []string{"steamcmd", "SteamCMD", "Games", "GameServers"} {
				roots = append(roots, filepath.Join(drive+string(filepath.Separator), leaf))
			}
		}
	}
	result := cleanUniquePaths(roots)
	filtered := result[:0]
	for _, root := range result {
		if directoryExists(root) {
			filtered = append(filtered, root)
		}
	}
	return filtered
}

func discoverSteamCommands(roots []string, configuredRoot, configuredExecutable string) []string {
	commands := make([]string, 0, len(roots)+2)
	for _, path := range []string{configuredExecutable, filepath.Join(configuredRoot, "steamcmd.exe")} {
		if fileExists(path) {
			commands = append(commands, filepath.Clean(path))
		}
	}
	for _, root := range roots {
		for _, path := range []string{
			filepath.Join(root, "steamcmd.exe"),
			filepath.Join(root, "SteamCMD", "steamcmd.exe"),
		} {
			if fileExists(path) {
				commands = append(commands, filepath.Clean(path))
			}
		}
	}
	return cleanUniquePaths(commands)
}

func closestSteamCommand(installDir string, commands []string) string {
	if len(commands) == 0 {
		return ""
	}
	installLower := strings.ToLower(filepath.Clean(installDir))
	for _, command := range commands {
		root := strings.ToLower(filepath.Dir(command))
		if strings.HasPrefix(installLower, root+string(filepath.Separator)) {
			return command
		}
	}
	return commands[0]
}

func walkDiscoveryRoot(root string, visit func(string, fs.DirEntry) bool) {
	root = filepath.Clean(root)
	directoryCount := 0
	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			if entry != nil && entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		relative, relativeErr := filepath.Rel(root, path)
		if relativeErr != nil {
			return nil
		}
		depth := 0
		if relative != "." {
			depth = len(strings.Split(relative, string(filepath.Separator)))
		}
		if entry.IsDir() {
			directoryCount++
			if directoryCount > maxDiscoveryDirectories || depth > maxDiscoveryDepth || entry.Type()&os.ModeSymlink != 0 {
				return filepath.SkipDir
			}
			return nil
		}
		if depth <= maxDiscoveryDepth && !visit(path, entry) {
			return fs.SkipAll
		}
		return nil
	})
}

func candidateID(gameID, path string) string {
	digest := sha256.Sum256([]byte(gameID + "\x00" + strings.ToLower(filepath.Clean(path))))
	return fmt.Sprintf("%s-%x", gameID, digest[:8])
}

func fileExists(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func directoryExists(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

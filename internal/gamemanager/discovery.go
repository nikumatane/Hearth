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
		candidate := panel.GameCandidate{
			ID: candidateID(palworldID, directory), InstallDir: directory, SteamCmd: steamCmd,
			SettingsPresent: settingsPresent, Detail: detail,
		}
		candidate.CanAdopt = candidateUsesSteamCMDDefaultLayout(candidate)
		if settingsPresent && steamCmd != "" && !candidate.CanAdopt {
			candidate.Detail += "；不在 SteamCMD 标准目录，暂不接管"
		}
		s.candidates[palworldID] = append(s.candidates[palworldID], candidate)
	}
	dstClusters := discoverDSTClusters(roots)
	s.candidates[dstID] = make([]panel.GameCandidate, 0, len(dstDirectories)*max(1, len(dstClusters)))
	for _, directory := range dstDirectories {
		steamCmd := closestSteamCommand(directory, steamCommands)
		if len(dstClusters) == 0 {
			s.candidates[dstID] = append(s.candidates[dstID], panel.GameCandidate{
				ID: candidateID(dstID, directory), InstallDir: directory, SteamCmd: steamCmd,
				Detail: "已识别 DST Dedicated Server；未发现有效 cluster 目录",
			})
			continue
		}
		for _, clusterDir := range dstClusters {
			validCluster := validDSTCluster(clusterDir)
			canAdopt := validCluster && dstUsesSteamCMDDefaultLayout(directory, steamCmd)
			tokenPresent := dstClusterTokenPresent(clusterDir)
			detail := "已识别 DST Dedicated Server 与 cluster 配置"
			if !validCluster {
				detail += "；cluster.ini、Master/server.ini 或 Caves/server.ini 不完整"
			} else if !canAdopt {
				detail += "；不在 SteamCMD 标准目录，暂不接管"
			} else {
				detail += "；可确认接管"
			}
			if tokenPresent {
				detail += "；cluster token 已配置"
			} else {
				detail += "；缺少 cluster token，接管后暂不能启动"
			}
			s.candidates[dstID] = append(s.candidates[dstID], panel.GameCandidate{
				ID: candidateID(dstID, directory+"\x00"+clusterDir), InstallDir: directory,
				ClusterDir: clusterDir, ClusterTokenPresent: tokenPresent, SteamCmd: steamCmd, SettingsPresent: validCluster,
				CanAdopt: canAdopt, Detail: detail,
			})
		}
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
		roots = append(roots, filepath.Join(home, "Documents", "Klei", "DoNotStarveTogether"))
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

func discoverDSTClusters(roots []string) []string {
	clusters := make(map[string]string)
	add := func(path string) {
		path = filepath.Clean(path)
		if validDSTCluster(path) {
			clusters[strings.ToLower(path)] = path
		}
	}
	for _, root := range roots {
		add(root)
		walkDiscoveryRoot(root, func(path string, entry fs.DirEntry) bool {
			if strings.EqualFold(entry.Name(), "cluster.ini") {
				add(filepath.Dir(path))
			}
			return true
		})
	}
	result := make([]string, 0, len(clusters))
	for _, path := range clusters {
		result = append(result, path)
	}
	sort.Strings(result)
	return result
}

func validDSTCluster(directory string) bool {
	if strings.TrimSpace(directory) == "" || !filepath.IsAbs(directory) {
		return false
	}
	for _, path := range []string{
		filepath.Join(directory, "cluster.ini"),
		filepath.Join(directory, "Master", "server.ini"),
		filepath.Join(directory, "Caves", "server.ini"),
	} {
		if !fileExists(path) {
			return false
		}
	}
	return true
}

func dstClusterTokenPresent(directory string) bool {
	return fileExists(filepath.Join(directory, "cluster_token.txt"))
}

func dstUsesSteamCMDDefaultLayout(installDir, steamCmd string) bool {
	if strings.TrimSpace(steamCmd) == "" {
		return false
	}
	steamRoot := filepath.Dir(steamCmd)
	for _, name := range []string{"Don't Starve Together Dedicated Server", "DontStarveTogetherDedicatedServer"} {
		expected := filepath.Join(steamRoot, "steamapps", "common", name)
		if strings.EqualFold(filepath.Clean(installDir), expected) {
			return true
		}
	}
	return false
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

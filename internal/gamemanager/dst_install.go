package gamemanager

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unicode/utf8"

	"hearth/internal/panel"
	"hearth/internal/steamapp"
)

const (
	dstSteamAppID       = "343050"
	dstSteamProductName = "Don't Starve Together Dedicated Server"
)

type dstInstallPlan struct {
	steamCmdRoot string
	installDir   string
	clusterDir   string
	clusterName  string
	clusterToken string
}

func newDSTInstallPlan(request panel.InstallGameRequest, configPath string) (dstInstallPlan, error) {
	if request.DST == nil {
		return dstInstallPlan{}, fmt.Errorf("%w: DST 安装缺少集群初始化参数", panel.ErrInvalid)
	}
	steamCmdRoot := filepath.Clean(strings.TrimSpace(request.SteamCmdRoot))
	if err := validateInstallPaths(steamCmdRoot, configPath); err != nil {
		return dstInstallPlan{}, err
	}
	installDir := dstInstallDir(steamCmdRoot)
	if err := validateExistingDSTInstall(steamCmdRoot, installDir); err != nil {
		return dstInstallPlan{}, err
	}
	clusterDir := filepath.Clean(strings.TrimSpace(request.DST.ClusterDir))
	if err := validateNewDSTClusterPath(clusterDir, steamCmdRoot, installDir, configPath); err != nil {
		return dstInstallPlan{}, err
	}
	clusterName := strings.TrimSpace(request.DST.ClusterName)
	if err := validateDSTText("集群名称", clusterName, 128, false); err != nil {
		return dstInstallPlan{}, err
	}
	token := strings.TrimSpace(request.DST.ClusterToken)
	if err := validateDSTText("cluster token", token, 4096, true); err != nil {
		return dstInstallPlan{}, err
	}
	return dstInstallPlan{
		steamCmdRoot: steamCmdRoot,
		installDir:   installDir,
		clusterDir:   clusterDir,
		clusterName:  clusterName,
		clusterToken: token,
	}, nil
}

func dstInstallDir(steamCmdRoot string) string {
	return filepath.Join(filepath.Clean(steamCmdRoot), "steamapps", "common", dstSteamProductName)
}

// validateExistingDSTInstall allows a retry after SteamCMD has already
// finished, but refuses to mutate an unknown non-empty directory.
func validateExistingDSTInstall(steamCmdRoot, installDir string) error {
	entries, err := os.ReadDir(installDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("%w: inspect DST install directory: %v", panel.ErrInvalid, err)
	}
	if len(entries) == 0 {
		return nil
	}
	executable := filepath.Join(installDir, "bin64", "dontstarve_dedicated_server_nullrenderer_x64.exe")
	manifest := filepath.Join(steamCmdRoot, "steamapps", "appmanifest_"+dstSteamAppID+".acf")
	if !fileExists(executable) || !validSteamManifest(manifest, dstSteamAppID) {
		return fmt.Errorf(
			"%w: DST 标准安装目录非空但未通过 App %s 校验；请保留现有文件并使用接管，或选择其他 SteamCMD 目录",
			panel.ErrInvalid, dstSteamAppID,
		)
	}
	return nil
}

func validSteamManifest(path, appID string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() || info.Size() <= 0 || info.Size() > 2<<20 {
		return false
	}
	return steamapp.ReadAppID(path) == appID
}

func validateNewDSTClusterPath(clusterDir, steamCmdRoot, installDir, configPath string) error {
	if clusterDir == "." || !filepath.IsAbs(clusterDir) {
		return fmt.Errorf("%w: clusterDir 必须是绝对路径", panel.ErrInvalid)
	}
	volume := filepath.VolumeName(clusterDir)
	if volume != "" && filepath.Clean(clusterDir) == filepath.Clean(volume+string(filepath.Separator)) {
		return fmt.Errorf("%w: clusterDir 不能是磁盘根目录", panel.ErrInvalid)
	}
	parent := filepath.Dir(clusterDir)
	parentVolume := filepath.VolumeName(parent)
	if filepath.Clean(parent) == filepath.Clean(parentVolume+string(filepath.Separator)) {
		return fmt.Errorf("%w: clusterDir 不能直接位于磁盘根目录；DST 需要可识别的 conf_dir", panel.ErrInvalid)
	}
	protected := map[string]string{
		"SteamCMD 目录": steamCmdRoot,
		"DST 安装目录":    installDir,
	}
	if strings.TrimSpace(configPath) != "" {
		configDirectory := filepath.Dir(configPath)
		if absolute, err := filepath.Abs(configDirectory); err == nil {
			configDirectory = absolute
		}
		protected["Hearth 配置目录"] = configDirectory
	}
	for label, other := range protected {
		if strings.TrimSpace(other) != "" && pathsOverlap(clusterDir, other) {
			return fmt.Errorf("%w: clusterDir 不能覆盖%s", panel.ErrInvalid, label)
		}
	}
	if _, err := os.Lstat(clusterDir); err == nil {
		return fmt.Errorf("%w: clusterDir 必须是尚不存在的全新目录；现有集群请使用接管", panel.ErrInvalid)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%w: inspect clusterDir: %v", panel.ErrInvalid, err)
	}
	parentInfo, err := os.Stat(parent)
	if err != nil || !parentInfo.IsDir() {
		return fmt.Errorf("%w: clusterDir 的父目录必须已存在", panel.ErrInvalid)
	}
	resolvedParent, err := filepath.EvalSymlinks(parent)
	if err != nil {
		return fmt.Errorf("%w: 无法解析 clusterDir 父目录: %v", panel.ErrInvalid, err)
	}
	resolvedCluster := filepath.Join(resolvedParent, filepath.Base(clusterDir))
	for label, other := range protected {
		resolvedOther := filepath.Clean(other)
		if value, resolveErr := filepath.EvalSymlinks(other); resolveErr == nil {
			resolvedOther = value
		}
		if pathsOverlap(resolvedCluster, resolvedOther) {
			return fmt.Errorf("%w: clusterDir 解析真实路径后不能覆盖%s", panel.ErrInvalid, label)
		}
	}
	return nil
}

func pathsOverlap(left, right string) bool {
	return pathContains(left, right) || pathContains(right, left)
}

func validateDSTText(label, value string, maxRunes int, optional bool) error {
	if value == "" {
		if optional {
			return nil
		}
		return fmt.Errorf("%w: %s不能为空", panel.ErrInvalid, label)
	}
	if !utf8.ValidString(value) || utf8.RuneCountInString(value) > maxRunes || strings.ContainsAny(value, "\x00\r\n") {
		return fmt.Errorf("%w: %s格式无效", panel.ErrInvalid, label)
	}
	return nil
}

// createDSTCluster commits a complete cluster in a single rename. The returned
// rollback removes only the directory created by this function and is used if
// adapter activation or Hearth configuration persistence fails.
func createDSTCluster(plan dstInstallPlan) (rollback func() error, err error) {
	parent := filepath.Dir(plan.clusterDir)
	staging, err := os.MkdirTemp(parent, ".hearth-dst-cluster-")
	if err != nil {
		return nil, fmt.Errorf("create DST cluster staging directory: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.RemoveAll(staging)
		}
	}()
	clusterKey, err := randomDSTClusterKey()
	if err != nil {
		return nil, err
	}
	files := map[string]string{
		"cluster.ini":                         dstClusterINI(plan.clusterName, clusterKey),
		filepath.Join("Master", "server.ini"): dstMasterINI(),
		filepath.Join("Caves", "server.ini"):  dstCavesINI(),
		filepath.Join("Caves", "worldgenoverride.lua"): `return {
  override_enabled = true,
  preset = "DST_CAVE",
}
`,
	}
	if plan.clusterToken != "" {
		files["cluster_token.txt"] = plan.clusterToken
	}
	for relative, contents := range files {
		path := filepath.Join(staging, relative)
		if err := writeNewPrivateFile(path, []byte(contents)); err != nil {
			return nil, err
		}
	}
	if err := os.Rename(staging, plan.clusterDir); err != nil {
		return nil, fmt.Errorf("commit DST cluster directory: %w", err)
	}
	committed = true
	return func() error { return os.RemoveAll(plan.clusterDir) }, nil
}

func writeNewPrivateFile(path string, contents []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create DST configuration directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create DST configuration: %w", err)
	}
	if _, err := file.Write(contents); err != nil {
		_ = file.Close()
		return fmt.Errorf("write DST configuration: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("sync DST configuration: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close DST configuration: %w", err)
	}
	return nil
}

func randomDSTClusterKey() (string, error) {
	buffer := make([]byte, 24)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("generate DST cluster key: %w", err)
	}
	return hex.EncodeToString(buffer), nil
}

func dstClusterINI(name, key string) string {
	return fmt.Sprintf(`[GAMEPLAY]
game_mode = survival
max_players = 16
pvp = false
pause_when_empty = true

[NETWORK]
cluster_description = Managed by Hearth
cluster_name = %s
cluster_intention = cooperative

[MISC]
console_enabled = true

[SHARD]
shard_enabled = true
bind_ip = 127.0.0.1
master_ip = 127.0.0.1
master_port = 10888
cluster_key = %s
`, name, key)
}

func dstMasterINI() string {
	return `[NETWORK]
server_port = 10999

[SHARD]
is_master = true

[STEAM]
master_server_port = 27016
authentication_port = 8766
`
}

func dstCavesINI() string {
	return `[NETWORK]
server_port = 11000

[SHARD]
is_master = false
name = Caves
id = 2

[STEAM]
master_server_port = 27017
authentication_port = 8767
`
}

func suggestedDSTClusterDir(discoveryRoots []string) string {
	roots := make([]string, 0, len(discoveryRoots)+8)
	for _, root := range discoveryRoots {
		if strings.EqualFold(filepath.Base(filepath.Clean(root)), "DoNotStarveTogether") {
			roots = append(roots, root)
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		roots = append(roots, filepath.Join(home, "Documents", "Klei", "DoNotStarveTogether"))
	}
	if runtime.GOOS == "windows" {
		roots = append(roots, discoverWindowsDSTRoots()...)
	}
	for _, root := range cleanUniquePaths(roots) {
		if !directoryExists(root) {
			continue
		}
		for index := 1; index <= 100; index++ {
			name := "HearthCluster"
			if index > 1 {
				name = fmt.Sprintf("HearthCluster%d", index)
			}
			candidate := filepath.Join(root, name)
			if _, err := os.Lstat(candidate); errors.Is(err, os.ErrNotExist) {
				return candidate
			} else if err != nil {
				break
			}
		}
	}
	return ""
}

package gamemanager

import (
	"fmt"
	"net/netip"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"hearth/internal/config"
	"hearth/internal/palworld"
	"hearth/internal/panel"
)

const (
	palworldID = "palworld"
	dstID      = "dont-starve-together"
)

type serviceFactory func(config.GameConfig) (panel.Service, error)

type Service struct {
	mu         sync.RWMutex
	config     config.Config
	configPath string
	delegate   panel.Service
	initError  error
	candidates map[string][]panel.GameCandidate
	activities []panel.Activity
	logPaths   map[string]string
	installing bool
	activeTask string
	factory    serviceFactory

	host hostMonitor
}

func New(cfg config.Config, configPath string) (*Service, error) {
	applyManagementDefaults(&cfg, configPath)
	manager := &Service{
		config: cfg, configPath: configPath,
		candidates: make(map[string][]panel.GameCandidate), logPaths: make(map[string]string),
		factory: func(gameConfig config.GameConfig) (panel.Service, error) {
			return palworld.NewService(gameConfig)
		},
	}
	if cfg.Games.Palworld.Enabled {
		manager.delegate, manager.initError = manager.factory(cfg.Games.Palworld)
	}
	manager.discoverLocked()
	return manager, nil
}

func applyManagementDefaults(cfg *config.Config, configPath string) {
	if cfg.Management.InstallRoot == "" {
		if cfg.Games.Palworld.InstallDir != "" {
			cfg.Management.InstallRoot = filepath.Dir(cfg.Games.Palworld.InstallDir)
		} else if runtime.GOOS == "windows" {
			cfg.Management.InstallRoot = `C:\GameServers`
		} else {
			cfg.Management.InstallRoot = filepath.Join(filepath.Dir(configPath), "game-servers")
		}
	}
	if cfg.Management.SteamCmdRoot == "" {
		if cfg.Games.Palworld.SteamCmd != "" {
			cfg.Management.SteamCmdRoot = filepath.Dir(cfg.Games.Palworld.SteamCmd)
		} else if runtime.GOOS == "windows" {
			cfg.Management.SteamCmdRoot = `C:\SteamCMD`
		} else {
			cfg.Management.SteamCmdRoot = filepath.Join(filepath.Dir(configPath), "steamcmd")
		}
	}
}

func (s *Service) Overview() panel.Overview {
	s.mu.RLock()
	delegate := s.delegate
	activities := cloneActivities(s.activities)
	installRoot := s.config.Management.InstallRoot
	if installRoot == "" {
		installRoot = filepath.Dir(s.configPath)
	}
	s.mu.RUnlock()

	var overview panel.Overview
	if delegate != nil {
		overview = delegate.Overview()
	} else {
		overview = panel.Overview{
			Host:  hostUsageOrEmpty(&s.host, installRoot),
			Games: []panel.Game{}, Activities: []panel.Activity{}, UpdatedAt: time.Now(),
		}
	}
	overview.Activities = append(activities, overview.Activities...)
	overview.UpdatedAt = time.Now()
	return overview
}

func (s *Service) Game(id string) (panel.Game, error) {
	s.mu.RLock()
	delegate := s.delegate
	s.mu.RUnlock()
	if delegate == nil {
		return panel.Game{}, panel.ErrNotFound
	}
	return delegate.Game(id)
}

func (s *Service) RunAction(id string, request panel.ActionRequest) (panel.Activity, error) {
	s.mu.RLock()
	delegate := s.delegate
	s.mu.RUnlock()
	if delegate == nil {
		return panel.Activity{}, panel.ErrNotFound
	}
	return delegate.RunAction(id, request)
}

func (s *Service) PalworldSettings() (panel.PalworldSettings, error) {
	s.mu.RLock()
	delegate := s.delegate
	s.mu.RUnlock()
	if delegate == nil {
		return panel.PalworldSettings{}, panel.ErrNotFound
	}
	return delegate.PalworldSettings()
}

func (s *Service) UpdatePalworldSettings(patch panel.PalworldSettingsPatch) (panel.PalworldSettings, error) {
	s.mu.RLock()
	delegate := s.delegate
	s.mu.RUnlock()
	if delegate == nil {
		return panel.PalworldSettings{}, panel.ErrNotFound
	}
	return delegate.UpdatePalworldSettings(patch)
}

func (s *Service) WorldOption() (panel.WorldOptionDocument, error) {
	s.mu.RLock()
	delegate := s.delegate
	s.mu.RUnlock()
	if delegate == nil {
		return panel.WorldOptionDocument{}, panel.ErrNotFound
	}
	return delegate.WorldOption()
}

func (s *Service) UpdateWorldOption(document panel.WorldOptionDocument) (panel.WorldOptionDocument, error) {
	s.mu.RLock()
	delegate := s.delegate
	s.mu.RUnlock()
	if delegate == nil {
		return panel.WorldOptionDocument{}, panel.ErrNotFound
	}
	return delegate.UpdateWorldOption(document)
}

func (s *Service) Management() panel.Management {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.managementLocked()
}

func (s *Service) RefreshDiscovery() (panel.Management, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.discoverLocked()
	return s.managementLocked(), nil
}

func (s *Service) managementLocked() panel.Management {
	palworldState := "not_installed"
	palworldDetail := "未发现 Palworld Dedicated Server"
	if len(s.candidates[palworldID]) > 0 {
		palworldState = "detected"
		palworldDetail = "发现现有安装，确认路径后可以接管"
	}
	if s.config.Games.Palworld.Enabled {
		palworldState = "error"
		palworldDetail = "配置中的 Palworld 暂时无法使用"
		if s.initError != nil {
			palworldDetail = safeErrorDetail(s.initError)
		}
	}
	if s.delegate != nil {
		palworldState = "managed"
		palworldDetail = "已由 Hearth 管理"
	}
	if s.installing {
		palworldState = "installing"
		palworldDetail = "管理员已确认安装，任务正在执行"
	}

	settings := s.systemSettingsLocked(false)
	return panel.Management{
		Games: []panel.ManagedGame{
			{
				ID: palworldID, Name: "幻兽帕鲁", ShortName: "PAL", Support: "available",
				State: palworldState, Detail: palworldDetail,
				InstallDir: s.config.Games.Palworld.InstallDir,
				SteamCmd:   s.config.Games.Palworld.SteamCmd,
				CanInstall: !s.installing && s.delegate == nil,
				CanAdopt:   !s.installing && s.delegate == nil && hasAdoptableCandidate(s.candidates[palworldID]),
				Candidates: cloneCandidates(s.candidates[palworldID]), ActiveTaskID: s.activeTask,
			},
			{
				ID: dstID, Name: "饥荒联机版", ShortName: "DST", Support: "planned",
				State:  candidateState(s.candidates[dstID]),
				Detail: dstDetail(s.candidates[dstID]), Candidates: cloneCandidates(s.candidates[dstID]),
			},
		},
		Settings: settings,
	}
}

func (s *Service) systemSettingsLocked(restartRequired bool) panel.SystemSettings {
	revision, _ := config.Revision(s.config)
	game := s.config.Games.Palworld
	return panel.SystemSettings{
		Revision:                  revision,
		InstallRoot:               s.config.Management.InstallRoot,
		SteamCmdRoot:              s.config.Management.SteamCmdRoot,
		DiscoveryRoots:            append([]string(nil), s.config.Management.DiscoveryRoots...),
		BackupRetentionDays:       defaultInt(game.BackupRetentionDays, 30),
		BackupMaxTotalGB:          defaultInt64(game.BackupMaxTotalGB, 20),
		ShutdownWaitSeconds:       defaultInt(game.ShutdownWaitSeconds, 30),
		SteamCmdNoProgressMinutes: defaultInt(game.SteamCmdNoProgressMinutes, 30),
		PalworldPort:              defaultInt(game.Port, 8211),
		SecureCookies:             s.config.SecureCookies,
		TrustedProxyCIDRs:         append([]string(nil), s.config.TrustedProxyCIDRs...),
		RestartRequired:           restartRequired,
	}
}

func (s *Service) AdoptGame(id string, request panel.AdoptGameRequest) (panel.ManagedGame, error) {
	if id != palworldID {
		return panel.ManagedGame{}, panel.ErrNotFound
	}
	if !request.Confirm {
		return panel.ManagedGame{}, fmt.Errorf("%w: 接管现有安装需要管理员明确确认", panel.ErrInvalid)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.delegate != nil || s.installing {
		return panel.ManagedGame{}, panel.ErrBusy
	}
	candidate, ok := candidateByID(s.candidates[palworldID], request.CandidateID)
	if !ok {
		return panel.ManagedGame{}, fmt.Errorf("%w: 安装候选已变化，请重新探测", panel.ErrInvalid)
	}
	if !candidate.SettingsPresent {
		return panel.ManagedGame{}, fmt.Errorf("%w: 现有安装缺少 PalWorldSettings.ini；Hearth 不会在接管时自动创建或覆盖配置", panel.ErrInvalid)
	}
	if strings.TrimSpace(candidate.SteamCmd) == "" {
		return panel.ManagedGame{}, fmt.Errorf("%w: 未找到与该安装配套的 steamcmd.exe", panel.ErrInvalid)
	}
	next := s.config
	next.Games.Palworld = palworldConfig(candidate.InstallDir, candidate.SteamCmd, next.Games.Palworld)
	next.Games.Palworld.Enabled = true
	delegate, err := s.factory(next.Games.Palworld)
	if err != nil {
		return panel.ManagedGame{}, err
	}
	if err := config.Save(s.configPath, next); err != nil {
		closeService(delegate)
		return panel.ManagedGame{}, err
	}
	s.config = next
	s.delegate = delegate
	s.initError = nil
	managed := s.managementLocked().Games[0]
	return managed, nil
}

func (s *Service) UpdateSystemSettings(patch panel.SystemSettingsPatch) (panel.SystemSettings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	currentRevision, err := config.Revision(s.config)
	if err != nil {
		return panel.SystemSettings{}, err
	}
	if patch.Revision == "" || patch.Revision != currentRevision {
		return panel.SystemSettings{}, fmt.Errorf("%w: 后台设置已被其他操作修改，请重新载入", panel.ErrInvalid)
	}
	if err := validateSystemSettingsPatch(patch); err != nil {
		return panel.SystemSettings{}, err
	}
	next := s.config
	next.Management.InstallRoot = filepath.Clean(strings.TrimSpace(patch.InstallRoot))
	next.Management.SteamCmdRoot = filepath.Clean(strings.TrimSpace(patch.SteamCmdRoot))
	next.Management.DiscoveryRoots = cleanUniquePaths(patch.DiscoveryRoots)
	next.Games.Palworld.BackupRetentionDays = patch.BackupRetentionDays
	next.Games.Palworld.BackupMaxTotalGB = patch.BackupMaxTotalGB
	next.Games.Palworld.ShutdownWaitSeconds = patch.ShutdownWaitSeconds
	next.Games.Palworld.SteamCmdNoProgressMinutes = patch.SteamCmdNoProgressMinutes
	next.Games.Palworld.Port = patch.PalworldPort
	next.SecureCookies = patch.SecureCookies
	next.TrustedProxyCIDRs = cleanUniqueStrings(patch.TrustedProxyCIDRs)
	if err := config.Save(s.configPath, next); err != nil {
		return panel.SystemSettings{}, err
	}
	restartRequired := next.SecureCookies != s.config.SecureCookies ||
		!sameStrings(next.TrustedProxyCIDRs, s.config.TrustedProxyCIDRs) ||
		next.Games.Palworld.BackupRetentionDays != s.config.Games.Palworld.BackupRetentionDays ||
		next.Games.Palworld.BackupMaxTotalGB != s.config.Games.Palworld.BackupMaxTotalGB ||
		next.Games.Palworld.ShutdownWaitSeconds != s.config.Games.Palworld.ShutdownWaitSeconds ||
		next.Games.Palworld.SteamCmdNoProgressMinutes != s.config.Games.Palworld.SteamCmdNoProgressMinutes ||
		next.Games.Palworld.Port != s.config.Games.Palworld.Port
	s.config = next
	s.discoverLocked()
	return s.systemSettingsLocked(restartRequired), nil
}

func (s *Service) TaskLogPath(id string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if path, ok := s.logPaths[id]; ok {
		return path, true
	}
	if s.config.Games.Palworld.InstallDir == "" {
		return "", false
	}
	return filepath.Join(s.config.Games.Palworld.InstallDir, "panel-logs", id), true
}

// Close releases adapter background work without changing any game process.
func (s *Service) Close() error {
	s.mu.RLock()
	delegate := s.delegate
	s.mu.RUnlock()
	closeService(delegate)
	return nil
}

func palworldConfig(installDir, steamCmd string, previous config.GameConfig) config.GameConfig {
	previous.InstallDir = filepath.Clean(installDir)
	previous.SteamCmd = filepath.Clean(steamCmd)
	previous.Executable = filepath.Join(previous.InstallDir, "PalServer.exe")
	previous.SettingsFile = filepath.Join(previous.InstallDir, "Pal", "Saved", "Config", "WindowsServer", "PalWorldSettings.ini")
	previous.DefaultSettingsFile = filepath.Join(previous.InstallDir, "DefaultPalWorldSettings.ini")
	previous.BackupDir = filepath.Join(previous.InstallDir, "panel-backups")
	if previous.ProcessName == "" {
		previous.ProcessName = "PalServer-Win64-Shipping-Cmd.exe"
	}
	return previous
}

func candidateByID(candidates []panel.GameCandidate, id string) (panel.GameCandidate, bool) {
	for _, candidate := range candidates {
		if candidate.ID == id {
			return candidate, true
		}
	}
	return panel.GameCandidate{}, false
}

func hasAdoptableCandidate(candidates []panel.GameCandidate) bool {
	for _, candidate := range candidates {
		if candidate.SettingsPresent && candidate.SteamCmd != "" {
			return true
		}
	}
	return false
}

func candidateState(candidates []panel.GameCandidate) string {
	if len(candidates) > 0 {
		return "detected"
	}
	return "not_installed"
}

func dstDetail(candidates []panel.GameCandidate) string {
	if len(candidates) > 0 {
		return "发现现有 DST 文件；生产管理将在 1.3.0 开放"
	}
	return "计划在 1.3.0 提供生产适配器"
}

func cloneCandidates(candidates []panel.GameCandidate) []panel.GameCandidate {
	return append([]panel.GameCandidate(nil), candidates...)
}

func cloneActivities(activities []panel.Activity) []panel.Activity {
	result := append([]panel.Activity(nil), activities...)
	for index := range result {
		result[index].Logs = append([]panel.LogRef(nil), result[index].Logs...)
	}
	return result
}

func safeErrorDetail(err error) string {
	text := strings.TrimSpace(err.Error())
	if len(text) > 240 {
		return text[:240] + "…"
	}
	return text
}

func closeService(service panel.Service) {
	if closer, ok := service.(interface{ Close() error }); ok {
		_ = closer.Close()
	}
}

func defaultInt(value, fallback int) int {
	if value <= 0 {
		return fallback
	}
	return value
}

func defaultInt64(value, fallback int64) int64 {
	if value <= 0 {
		return fallback
	}
	return value
}

func cleanUniqueStrings(values []string) []string {
	seen := make(map[string]struct{})
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func cleanUniquePaths(values []string) []string {
	cleaned := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			cleaned = append(cleaned, filepath.Clean(value))
		}
	}
	return cleanUniqueStrings(cleaned)
}

func sameStrings(left, right []string) bool {
	left = cleanUniqueStrings(left)
	right = cleanUniqueStrings(right)
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func validateSystemSettingsPatch(patch panel.SystemSettingsPatch) error {
	for name, value := range map[string]string{
		"installRoot":  patch.InstallRoot,
		"steamCmdRoot": patch.SteamCmdRoot,
	} {
		if strings.TrimSpace(value) == "" || !filepath.IsAbs(value) {
			return fmt.Errorf("%w: %s 必须是绝对路径", panel.ErrInvalid, name)
		}
	}
	for _, root := range patch.DiscoveryRoots {
		if strings.TrimSpace(root) == "" || !filepath.IsAbs(root) {
			return fmt.Errorf("%w: discoveryRoots 只能包含绝对路径", panel.ErrInvalid)
		}
	}
	if patch.BackupRetentionDays < 1 || patch.BackupRetentionDays > 36_500 {
		return fmt.Errorf("%w: 备份保留天数必须在 1–36500 之间", panel.ErrInvalid)
	}
	if patch.BackupMaxTotalGB < 1 || patch.BackupMaxTotalGB > 1_000_000 {
		return fmt.Errorf("%w: 备份容量必须在 1–1000000 GiB 之间", panel.ErrInvalid)
	}
	if patch.ShutdownWaitSeconds < 5 || patch.ShutdownWaitSeconds > 600 {
		return fmt.Errorf("%w: 安全关闭等待必须在 5–600 秒之间", panel.ErrInvalid)
	}
	if patch.SteamCmdNoProgressMinutes < 1 || patch.SteamCmdNoProgressMinutes > 10_080 {
		return fmt.Errorf("%w: SteamCMD 无进展超时必须在 1–10080 分钟之间", panel.ErrInvalid)
	}
	if patch.PalworldPort < 1 || patch.PalworldPort > 65_535 {
		return fmt.Errorf("%w: Palworld 端口无效", panel.ErrInvalid)
	}
	if len(patch.TrustedProxyCIDRs) == 0 {
		return fmt.Errorf("%w: 至少保留一个可信代理 CIDR", panel.ErrInvalid)
	}
	for _, value := range patch.TrustedProxyCIDRs {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(value))
		if err != nil {
			return fmt.Errorf("%w: 可信代理 CIDR 无效：%s", panel.ErrInvalid, value)
		}
		if prefix.Bits() == 0 {
			return fmt.Errorf("%w: 可信代理范围不能覆盖整个互联网", panel.ErrInvalid)
		}
	}
	return nil
}

var _ panel.Service = (*Service)(nil)
var _ panel.ManagementService = (*Service)(nil)
var _ panel.TaskLogLocator = (*Service)(nil)

package palworld

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"hearth/internal/config"
	"hearth/internal/panel"
)

const (
	palworldID                              = "palworld"
	palworldAppID                           = "2394010"
	defaultBackupRetentionDays              = 30
	defaultBackupMaxTotalGB           int64 = 20
	defaultSteamNoProgressMinutes           = 30
	automaticVersionCheckInterval           = 6 * time.Hour
	automaticVersionCheckFailureRetry       = time.Hour
	automaticVersionCheckPollInterval       = 15 * time.Minute
	automaticVersionCheckInitialDelay       = 30 * time.Second
	emptyServerShutdownWaitSeconds          = 5
	maxBackupRetentionDays                  = 36_500
	maxSafeBackupTotalGB              int64 = (1<<63 - 1) / (1 << 30)
	maxSteamNoProgressMinutes               = 7 * 24 * 60
)

var errSteamProcessTreeUncertain = errors.New("SteamCMD process-tree termination was incomplete")

type Service struct {
	config   config.GameConfig
	platform platformAdapter
	rest     *restClient
	ctx      context.Context
	cancel   context.CancelFunc

	mu             sync.Mutex
	busy           bool
	currentAction  string
	activities     []panel.Activity
	lastBackup     *time.Time
	lastProcess    processSample
	lastHost       hostSample
	lastSampleAt   time.Time
	cpuHistory     []panel.MetricPoint
	memoryHistory  []panel.MetricPoint
	hostCPUHistory []panel.MetricPoint
	hostMemHistory []panel.MetricPoint

	apiMu         sync.Mutex
	apiAt         time.Time
	apiStatus     apiStatus
	apiRefreshing bool
	apiGeneration uint64

	versionMu          sync.Mutex
	versionStatus      steamVersionStatus
	versionBuildID     string
	versionCheckedAt   time.Time
	versionAttemptedAt time.Time
}

type apiStatus struct {
	Info         serverInfo
	Metrics      serverMetrics
	PlayerCount  int
	Players      []panel.OnlinePlayer
	InfoOK       bool
	MetricsOK    bool
	PlayerListOK bool
}

func NewService(gameConfig config.GameConfig) (*Service, error) {
	applyDefaults(&gameConfig)
	if err := validateConfig(gameConfig); err != nil {
		return nil, err
	}
	if err := platformSupported(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	service := &Service{config: gameConfig, platform: nativePlatform{}, ctx: ctx, cancel: cancel}
	client, err := newRESTClient(gameConfig.RESTURL, gameConfig.RESTUsername, func() (string, error) {
		return readAdminPassword(gameConfig.SettingsFile)
	})
	if err != nil {
		cancel()
		return nil, err
	}
	service.rest = client
	service.apiRefreshing = true
	go service.refreshAPIStatus(service.apiGeneration)
	go service.runAutomaticVersionChecks()
	return service, nil
}

// Close stops background status and version-check work. It does not stop or
// otherwise modify the managed Palworld process.
func (s *Service) Close() error {
	if s.cancel != nil {
		s.cancel()
	}
	return nil
}

func applyDefaults(gameConfig *config.GameConfig) {
	if gameConfig.ProcessName == "" {
		gameConfig.ProcessName = "PalServer-Win64-Shipping-Cmd.exe"
	}
	if gameConfig.Executable == "" && gameConfig.InstallDir != "" {
		gameConfig.Executable = filepath.Join(gameConfig.InstallDir, "PalServer.exe")
	}
	if gameConfig.SettingsFile == "" && gameConfig.InstallDir != "" {
		gameConfig.SettingsFile = filepath.Join(gameConfig.InstallDir, "Pal", "Saved", "Config", "WindowsServer", "PalWorldSettings.ini")
	}
	if gameConfig.DefaultSettingsFile == "" && gameConfig.InstallDir != "" {
		gameConfig.DefaultSettingsFile = filepath.Join(gameConfig.InstallDir, "DefaultPalWorldSettings.ini")
	}
	if gameConfig.BackupDir == "" && gameConfig.InstallDir != "" {
		gameConfig.BackupDir = filepath.Join(gameConfig.InstallDir, "panel-backups")
	}
	if gameConfig.BackupRetentionDays <= 0 {
		gameConfig.BackupRetentionDays = defaultBackupRetentionDays
	}
	if gameConfig.BackupMaxTotalGB <= 0 {
		gameConfig.BackupMaxTotalGB = defaultBackupMaxTotalGB
	}
	if gameConfig.RESTURL == "" {
		gameConfig.RESTURL = "http://127.0.0.1:8212"
	}
	if gameConfig.RESTUsername == "" {
		gameConfig.RESTUsername = "admin"
	}
	if gameConfig.ShutdownWaitSeconds <= 0 {
		gameConfig.ShutdownWaitSeconds = 30
	}
	if gameConfig.SteamCmdNoProgressMinutes <= 0 {
		gameConfig.SteamCmdNoProgressMinutes = defaultSteamNoProgressMinutes
	}
	if gameConfig.Port <= 0 {
		gameConfig.Port = 8211
	}
}

func validateConfig(gameConfig config.GameConfig) error {
	requiredFiles := map[string]string{
		"installDir": gameConfig.InstallDir, "steamCmd": gameConfig.SteamCmd,
		"settingsFile": gameConfig.SettingsFile, "executable": gameConfig.Executable,
	}
	for name, value := range requiredFiles {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%w: palworld %s is required", panel.ErrInvalid, name)
		}
		if !filepath.IsAbs(value) {
			return fmt.Errorf("%w: palworld %s must be an absolute path", panel.ErrInvalid, name)
		}
	}
	if gameConfig.ProcessName == "" || strings.ContainsAny(gameConfig.ProcessName, `/\`) {
		return fmt.Errorf("%w: invalid palworld processName", panel.ErrInvalid)
	}
	if gameConfig.BackupRetentionDays > maxBackupRetentionDays {
		return fmt.Errorf("%w: palworld backupRetentionDays is too large", panel.ErrInvalid)
	}
	if gameConfig.BackupMaxTotalGB > maxSafeBackupTotalGB {
		return fmt.Errorf("%w: palworld backupMaxTotalGB is too large", panel.ErrInvalid)
	}
	if gameConfig.SteamCmdNoProgressMinutes > maxSteamNoProgressMinutes {
		return fmt.Errorf("%w: palworld steamCmdNoProgressMinutes is too large", panel.ErrInvalid)
	}
	for _, path := range []string{gameConfig.InstallDir, gameConfig.SteamCmd, gameConfig.SettingsFile, gameConfig.Executable} {
		if _, err := os.Stat(path); err != nil {
			return fmt.Errorf("%w: required Palworld path %s: %v", panel.ErrInvalid, path, err)
		}
	}
	return nil
}

func (s *Service) Overview() panel.Overview {
	game, host := s.snapshot()
	return panel.Overview{Host: host, Games: []panel.Game{game}, Activities: s.activitySnapshot(), UpdatedAt: time.Now()}
}

func (s *Service) activitySnapshot() []panel.Activity {
	s.mu.Lock()
	defer s.mu.Unlock()
	activities := append([]panel.Activity{}, s.activities...)
	for index := range activities {
		activities[index].Logs = append([]panel.LogRef{}, activities[index].Logs...)
	}
	return activities
}

func (s *Service) Game(id string) (panel.Game, error) {
	if id != palworldID {
		return panel.Game{}, panel.ErrNotFound
	}
	game, _ := s.snapshot()
	return game, nil
}

func (s *Service) RunAction(id string, request panel.ActionRequest) (panel.Activity, error) {
	if id != palworldID {
		return panel.Activity{}, panel.ErrNotFound
	}
	action := request.Action
	if action != "start" && action != "stop" && action != "restart" &&
		action != "update" && action != "backup" && action != "check-update" {
		return panel.Activity{}, panel.ErrBadAction
	}
	if request.AllowUnsafe && !actionAllowsUnsafeFallback(action) {
		return panel.Activity{}, fmt.Errorf("%w: 强制回退确认仅适用于停止和重启", panel.ErrInvalid)
	}

	process, _, err := s.platform.sample(s.config.ProcessName, s.config.InstallDir)
	if err != nil {
		return panel.Activity{}, err
	}
	if action == "start" && process.Running {
		return panel.Activity{}, fmt.Errorf("%w: 帕鲁服务器已经在运行", panel.ErrUnsafe)
	}
	if action == "stop" && !process.Running {
		return panel.Activity{}, fmt.Errorf("%w: 帕鲁服务器当前未运行", panel.ErrUnsafe)
	}
	if action == "restart" && !process.Running {
		return panel.Activity{}, fmt.Errorf("%w: 帕鲁服务器当前未运行，请使用启动", panel.ErrUnsafe)
	}
	if action == "start" || action == "restart" || action == "update" || action == "check-update" {
		steamProcess, _, sampleErr := s.platform.sample("steamcmd.exe", s.config.InstallDir)
		if sampleErr != nil {
			return panel.Activity{}, sampleErr
		}
		if steamProcess.Running {
			return panel.Activity{}, fmt.Errorf("%w: 检测到已有 SteamCMD 进程，为避免启动与更新文件冲突，已拒绝操作", panel.ErrUnsafe)
		}
	}

	s.mu.Lock()
	if s.busy {
		s.mu.Unlock()
		return panel.Activity{}, panel.ErrBusy
	}
	s.busy = true
	s.currentAction = action
	now := time.Now()
	detail := "任务已进入执行队列"
	if request.AllowUnsafe {
		detail = "已确认：REST 安全关闭失败时允许强制终止原进程"
	}
	activity := panel.Activity{
		ID: fmt.Sprintf("pal-%d", time.Now().UnixNano()), GameID: palworldID,
		Action: action, Title: productionActionTitle(action), Detail: detail,
		Status: "running", Stage: "排队", Progress: 5, CreatedAt: now, UpdatedAt: now,
	}
	s.activities = append([]panel.Activity{activity}, s.activities...)
	if len(s.activities) > 20 {
		s.activities = s.activities[:20]
	}
	s.mu.Unlock()

	slog.Info("palworld task queued", "action", action, "activity", activity.ID)
	go s.performAction(action, process, request.AllowUnsafe, activity.ID)
	return activity, nil
}

func actionAllowsUnsafeFallback(action string) bool {
	return action == "stop" || action == "restart"
}

func (s *Service) PalworldSettings() (panel.PalworldSettings, error) {
	return readPalworldSettings(s.config.SettingsFile, s.config.DefaultSettingsFile)
}

func (s *Service) UpdatePalworldSettings(patch panel.PalworldSettingsPatch) (panel.PalworldSettings, error) {
	s.mu.Lock()
	if s.busy {
		s.mu.Unlock()
		return panel.PalworldSettings{}, panel.ErrBusy
	}
	s.busy = true
	s.currentAction = "settings"
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.busy = false
		s.currentAction = ""
		s.mu.Unlock()
	}()

	process, _, err := s.platform.sample(s.config.ProcessName, s.config.InstallDir)
	if err != nil {
		return panel.PalworldSettings{}, err
	}
	if process.Running {
		return panel.PalworldSettings{}, fmt.Errorf(
			"%w: 修改 PalWorldSettings.ini 前必须先安全停止帕鲁服务器", panel.ErrUnsafe,
		)
	}
	updated, err := patchPalworldSettings(s.config.SettingsFile, s.config.DefaultSettingsFile, patch)
	if err != nil {
		return panel.PalworldSettings{}, err
	}
	s.addCompletedActivity("INI 配置已保存", "仅写入明确修改的参数；原文件已备份")
	return updated, nil
}

func (s *Service) WorldOption() (panel.WorldOptionDocument, error) {
	world, err := detectActiveWorld(s.config.InstallDir)
	if err != nil {
		return panel.WorldOptionDocument{}, err
	}
	return readWorldOption(world)
}

func (s *Service) UpdateWorldOption(document panel.WorldOptionDocument) (panel.WorldOptionDocument, error) {
	s.mu.Lock()
	if s.busy {
		s.mu.Unlock()
		return panel.WorldOptionDocument{}, panel.ErrBusy
	}
	s.busy = true
	s.currentAction = "settings"
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.busy = false
		s.currentAction = ""
		s.mu.Unlock()
	}()

	process, _, err := s.platform.sample(s.config.ProcessName, s.config.InstallDir)
	if err != nil {
		return panel.WorldOptionDocument{}, err
	}
	if process.Running {
		return panel.WorldOptionDocument{}, fmt.Errorf(
			"%w: 修改 WorldOption.sav 前必须先安全停止帕鲁服务器", panel.ErrUnsafe,
		)
	}
	world, err := detectActiveWorld(s.config.InstallDir)
	if err != nil {
		return panel.WorldOptionDocument{}, err
	}
	if !strings.EqualFold(world.ID, document.WorldID) {
		return panel.WorldOptionDocument{}, fmt.Errorf(
			"%w: 页面中的世界 ID 为 %s，当前检测结果为 %s，请重新载入后再保存",
			panel.ErrUnsafe, document.WorldID, world.ID,
		)
	}
	if changed, revisionErr := worldOptionModified(world.OptionPath, document.Revision); revisionErr != nil {
		return panel.WorldOptionDocument{}, revisionErr
	} else if changed {
		return panel.WorldOptionDocument{}, fmt.Errorf(
			"%w: WorldOption.sav 已被其他程序修改，请重新载入后再保存", panel.ErrUnsafe,
		)
	}
	if err := validateWorldOptionContainer(document.Data); err != nil {
		return panel.WorldOptionDocument{}, err
	}
	if _, err := s.createBackup(); err != nil {
		return panel.WorldOptionDocument{}, fmt.Errorf("backup before WorldOption update: %w", err)
	}
	if err := atomicWriteFile(world.OptionPath, document.Data); err != nil {
		return panel.WorldOptionDocument{}, fmt.Errorf("write WorldOption.sav: %w", err)
	}
	s.addCompletedActivity(
		"世界配置已保存",
		fmt.Sprintf("仅更新 WorldOption.sav；当前存档 %s", world.ID),
	)
	slog.Info("palworld world settings saved", "world_id", world.ID)
	return readWorldOption(world)
}

func (s *Service) snapshot() (panel.Game, panel.ResourceUsage) {
	process, host, err := s.platform.sample(s.config.ProcessName, s.config.InstallDir)
	now := time.Now()
	configuredPlayerMax := readNumericOption(s.config.SettingsFile, "ServerPlayerMaxNum")
	game := panel.Game{
		ID: palworldID, Name: "幻兽帕鲁", ShortName: "PAL", State: "stopped",
		Port: s.config.Port, Tags: []string{"Steam", "REST API", "Windows"},
		PlayersMax:       configuredPlayerMax,
		PlayersMaxKnown:  configuredPlayerMax > 0,
		PlayersAvailable: true, PlayersSource: "进程已停止",
		CPUHistory: []panel.MetricPoint{}, MemoryHistory: []panel.MetricPoint{},
	}
	if management, managementErr := readManagementSettings(s.config.SettingsFile); managementErr == nil {
		game.RESTEnabled = management.RESTEnabled
	}
	if world, worldErr := detectActiveWorld(s.config.InstallDir); worldErr == nil {
		game.SaveID = world.ID
		game.SaveDetection = world.Detection
		if _, optionErr := os.Stat(world.OptionPath); optionErr == nil || !errors.Is(optionErr, os.ErrNotExist) {
			// WorldOption.sav takes precedence over PalWorldSettings.ini. The
			// browser can decode it for editing, but the production backend does
			// not guess values from a compressed save container. Live REST metrics
			// will replace this unknown fallback while the server is running.
			game.PlayersMax = 0
			game.PlayersMaxKnown = false
		}
	}
	resource := panel.ResourceUsage{CPUHistory: []panel.MetricPoint{}, MemoryHistory: []panel.MetricPoint{}}
	if err != nil {
		game.State = "error"
		game.PlayersAvailable = false
		game.PlayersSource = "节点状态采样失败"
		return game, resource
	}

	s.mu.Lock()
	hostCPU, processCPU := calculateCPU(s.lastHost, host, s.lastProcess, process, s.lastSampleAt, now)
	s.lastHost, s.lastProcess, s.lastSampleAt = host, process, now
	memoryUsed := host.MemoryTotal - host.MemoryAvailable
	resource = panel.ResourceUsage{
		CPUPercent: hostCPU, MemoryPercent: percent(memoryUsed, host.MemoryTotal),
		MemoryUsedGB: bytesToGB(memoryUsed), MemoryTotalGB: bytesToGB(host.MemoryTotal),
		DiskPercent: percent(host.DiskTotal-host.DiskAvailable, host.DiskTotal),
		DiskUsedGB:  bytesToGB(host.DiskTotal - host.DiskAvailable), DiskTotalGB: bytesToGB(host.DiskTotal),
	}
	if process.Running {
		game.State = "running"
		game.PlayersAvailable = false
		game.PlayersSource = "REST API 暂不可用"
		game.MemoryGB = bytesToGB(process.MemoryBytes)
		game.CPUPercent = processCPU
		if !process.StartedAt.IsZero() {
			game.UptimeSeconds = int64(now.Sub(process.StartedAt).Seconds())
		}
	}
	if s.busy {
		switch s.currentAction {
		case "start":
			game.State = "starting"
			if !process.Running {
				game.PlayersAvailable = false
				game.PlayersSource = "服务器启动中"
			}
		case "stop", "restart", "update":
			game.State = "stopping"
		}
	}
	s.cpuHistory = appendMetric(s.cpuHistory, game.CPUPercent, now)
	s.memoryHistory = appendMetric(s.memoryHistory, game.MemoryGB, now)
	s.hostCPUHistory = appendMetric(s.hostCPUHistory, resource.CPUPercent, now)
	s.hostMemHistory = appendMetric(s.hostMemHistory, resource.MemoryPercent, now)
	game.CPUHistory = append([]panel.MetricPoint(nil), s.cpuHistory...)
	game.MemoryHistory = append([]panel.MetricPoint(nil), s.memoryHistory...)
	resource.CPUHistory = append([]panel.MetricPoint(nil), s.hostCPUHistory...)
	resource.MemoryHistory = append([]panel.MetricPoint(nil), s.hostMemHistory...)
	if s.lastBackup != nil {
		lastBackup := *s.lastBackup
		game.LastBackupAt = &lastBackup
	}
	s.mu.Unlock()

	if process.Running {
		status := s.cachedAPIStatus()
		applyAPIStatus(&game, status)
	}
	buildID := s.installedBuildID()
	if game.Version == "" {
		game.Version = buildID
	}
	applyVersionStatus(&game, s.versionStatusForBuild(buildID))
	return game, resource
}

func applyAPIStatus(game *panel.Game, status apiStatus) {
	game.RESTAvailable = status.InfoOK
	if status.InfoOK {
		game.Version = status.Info.Version
	}
	if status.MetricsOK {
		game.PlayersOnline = status.Metrics.CurrentPlayerNum
		if status.Metrics.MaxPlayerNum > 0 {
			game.PlayersMax = status.Metrics.MaxPlayerNum
			game.PlayersMaxKnown = true
		}
		game.PlayersAvailable = true
		game.PlayersSource = "REST API 指标"
		if status.Metrics.Uptime > 0 {
			game.UptimeSeconds = status.Metrics.Uptime
		}
	}
	if status.PlayerListOK {
		if status.MetricsOK && status.PlayerCount != status.Metrics.CurrentPlayerNum {
			slog.Debug(
				"palworld REST player count mismatch",
				"players_endpoint", status.PlayerCount,
				"metrics_endpoint", status.Metrics.CurrentPlayerNum,
			)
		}
		game.PlayersOnline = status.PlayerCount
		game.Players = append([]panel.OnlinePlayer(nil), status.Players...)
		game.PlayersAvailable = true
		game.PlayersSource = "REST API 玩家列表"
	}
}

func (s *Service) cachedAPIStatus() apiStatus {
	s.apiMu.Lock()
	status := s.apiStatus
	if time.Since(s.apiAt) >= 5*time.Second && !s.apiRefreshing {
		s.apiRefreshing = true
		generation := s.apiGeneration
		go s.refreshAPIStatus(generation)
	}
	s.apiMu.Unlock()
	return status
}

func (s *Service) refreshAPIStatus(generation uint64) {
	parent := s.ctx
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, 1200*time.Millisecond)
	defer cancel()
	status := apiStatus{}
	var statusMu sync.Mutex
	var wait sync.WaitGroup
	wait.Add(3)
	go func() {
		defer wait.Done()
		if info, err := s.rest.info(ctx); err == nil {
			statusMu.Lock()
			status.Info, status.InfoOK = info, true
			statusMu.Unlock()
		}
	}()
	go func() {
		defer wait.Done()
		if metrics, err := s.rest.metrics(ctx); err == nil {
			statusMu.Lock()
			status.Metrics, status.MetricsOK = metrics, true
			statusMu.Unlock()
		}
	}()
	go func() {
		defer wait.Done()
		if players, err := s.rest.players(ctx); err == nil && players.Players != nil {
			statusMu.Lock()
			status.PlayerCount, status.PlayerListOK = len(players.Players), true
			status.Players = publicOnlinePlayers(players.Players)
			statusMu.Unlock()
		}
	}()
	wait.Wait()

	s.apiMu.Lock()
	if generation != s.apiGeneration {
		s.apiRefreshing = false
		s.apiMu.Unlock()
		return
	}
	s.apiAt = time.Now()
	s.apiStatus = status
	s.apiRefreshing = false
	s.apiMu.Unlock()
}

func publicOnlinePlayers(players []serverPlayer) []panel.OnlinePlayer {
	const (
		maxPlayers    = 128
		maxNameRunes  = 64
		unnamedPlayer = "未命名玩家"
	)
	limit := min(len(players), maxPlayers)
	result := make([]panel.OnlinePlayer, 0, limit)
	for _, player := range players[:limit] {
		name := strings.Map(func(value rune) rune {
			if unicode.IsControl(value) || unicode.In(value, unicode.Cf, unicode.Zl, unicode.Zp) {
				return -1
			}
			return value
		}, strings.TrimSpace(player.Name))
		runes := []rune(name)
		if len(runes) > maxNameRunes {
			name = string(runes[:maxNameRunes])
		}
		if name == "" {
			name = unnamedPlayer
		}
		result = append(result, panel.OnlinePlayer{Name: name})
	}
	return result
}

func (s *Service) invalidateAPIStatus() {
	s.apiMu.Lock()
	s.apiAt = time.Time{}
	s.apiStatus = apiStatus{}
	s.apiGeneration++
	s.apiMu.Unlock()
}

func (s *Service) installedBuildID() string {
	path := s.appManifestPath()
	data, err := os.ReadFile(path)
	if err != nil {
		return "未知"
	}
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) == 2 && strings.Trim(fields[0], `"`) == "buildid" {
			return strings.Trim(fields[1], `"`)
		}
	}
	return "未知"
}

func (s *Service) appManifestPath() string {
	return filepath.Clean(filepath.Join(s.config.InstallDir, "..", "..", "appmanifest_"+palworldAppID+".acf"))
}

func applyVersionStatus(game *panel.Game, status steamVersionStatus) {
	game.VersionCheck = status.State
	game.UpdateAvailable = status.UpdateAvailable
	game.AvailableVersion = status.AvailableVersion
}

func (s *Service) versionStatusForBuild(buildID string) steamVersionStatus {
	if buildID == "" || buildID == "未知" {
		return steamVersionStatus{State: versionCheckUnavailable}
	}

	s.versionMu.Lock()
	defer s.versionMu.Unlock()
	if buildID != s.versionBuildID {
		s.versionBuildID = buildID
		s.versionStatus = steamVersionStatus{State: versionCheckUnchecked}
		s.versionCheckedAt = time.Time{}
		s.versionAttemptedAt = time.Time{}
	}
	return s.versionStatus
}

func (s *Service) setVersionStatus(buildID string, status steamVersionStatus) {
	s.versionMu.Lock()
	s.versionBuildID = buildID
	s.versionStatus = status
	if status.State == versionCheckChecking {
		s.versionAttemptedAt = time.Now()
		s.versionCheckedAt = time.Time{}
	}
	if status.State == versionCheckCurrent || status.State == versionCheckAvailable {
		s.versionCheckedAt = time.Now()
	}
	s.versionMu.Unlock()
}

type taskReporter func(stage string, progress int, detail string)
type taskLogReporter func(id, label string)

type steamUpdateOutcome string

const (
	steamUpdateApplied        steamUpdateOutcome = "applied"
	steamUpdateAlreadyCurrent steamUpdateOutcome = "already_current"
)

var steamProgressPattern = regexp.MustCompile(`(?i)progress:\s*([0-9]+(?:\.[0-9]+)?)`)

func (s *Service) performAction(
	action string,
	process processSample,
	allowUnsafe bool,
	activityID string,
) {
	report := func(stage string, progress int, detail string) {
		s.updateActivity(activityID, stage, progress, detail)
	}
	registerLog := func(id, label string) {
		s.addActivityLog(activityID, panel.LogRef{ID: id, Label: label})
	}
	var (
		err           error
		forced        bool
		updateOutcome steamUpdateOutcome
	)
	switch action {
	case "start":
		err = s.start(scaleReporter(report, 5, 95), registerLog)
	case "stop":
		forced, err = s.stop(process, allowUnsafe, scaleReporter(report, 5, 95))
	case "restart":
		forced, err = s.stop(process, allowUnsafe, scaleReporter(report, 5, 50))
		if err == nil {
			err = s.start(scaleReporter(report, 50, 85), registerLog)
		}
		if err == nil {
			if forced {
				report("检查进程", 95, "游戏进程已恢复；REST 不可用，按进程状态完成检查")
			} else {
				err = s.waitForREST(scaleReporter(report, 85, 98))
			}
		}
	case "update":
		updateOutcome, err = s.update(process.Running, report, registerLog)
	case "backup":
		err = s.backup(process.Running, report)
	case "check-update":
		err = s.checkVersion(report, registerLog)
	}
	s.finishActivity(activityID, action, forced, updateOutcome, err)
}

func scaleReporter(report taskReporter, start, end int) taskReporter {
	return func(stage string, progress int, detail string) {
		progress = max(0, min(progress, 100))
		report(stage, start+(end-start)*progress/100, detail)
	}
}

func (s *Service) stop(
	expectedProcess processSample,
	allowUnsafe bool,
	report taskReporter,
) (bool, error) {
	report("安全关闭", 2, "正在通过 REST API 保存世界并请求安全关闭")
	safeErr := s.gracefulStop(scaleReporter(report, 0, 60))
	if safeErr == nil {
		report("安全关闭", 100, "世界已保存，Palworld 进程已安全退出")
		return false, nil
	}
	if !allowUnsafe {
		return false, safeErr
	}

	slog.Warn(
		"palworld graceful stop failed; using confirmed force stop",
		"pid", expectedProcess.PID,
		"error", safeErr,
	)
	report("强制停止", 62, "REST 安全关闭不可用；正在终止任务创建时识别到的 Palworld 进程")
	if forceErr := s.forceStop(expectedProcess, scaleReporter(report, 60, 100)); forceErr != nil {
		return true, fmt.Errorf(
			"safe shutdown failed: %v; confirmed force stop also failed: %w",
			safeErr,
			forceErr,
		)
	}
	report("强制停止", 100, "Palworld 原进程已强制终止；最近未自动保存的进度可能丢失")
	return true, nil
}

func (s *Service) gracefulStop(report taskReporter) error {
	report("保存世界", 5, "正在通过 REST API 保存当前世界")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	if err := s.rest.save(ctx); err != nil {
		cancel()
		return fmt.Errorf("save world before shutdown: %w", err)
	}
	cancel()

	shutdownWaitSeconds, playerDetail := s.shutdownWaitSeconds()
	report("请求安全关闭", 30, fmt.Sprintf("世界已保存，%s；将在 %d 秒后关闭", playerDetail, shutdownWaitSeconds))
	ctx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
	err := s.rest.shutdown(ctx, shutdownWaitSeconds, "服务器维护中，请稍后重新连接。")
	cancel()
	if err != nil {
		return fmt.Errorf("request graceful shutdown: %w", err)
	}

	waitStarted := time.Now()
	waitDuration := time.Duration(shutdownWaitSeconds+45) * time.Second
	deadline := waitStarted.Add(waitDuration)
	for time.Now().Before(deadline) {
		process, _, sampleErr := s.platform.sample(s.config.ProcessName, s.config.InstallDir)
		if sampleErr != nil {
			return sampleErr
		}
		if !process.Running {
			report("等待进程退出", 100, "Palworld 进程已安全退出")
			return nil
		}
		waitProgress := int(time.Since(waitStarted) * 45 / waitDuration)
		remainingNotice := max(0, shutdownWaitSeconds-int(time.Since(waitStarted).Seconds()))
		detail := "关闭倒计时结束，正在等待游戏进程完成退出"
		if remainingNotice > 0 {
			detail = fmt.Sprintf("安全关闭请求已发送，约 %d 秒后关闭游戏进程", remainingNotice)
		}
		report("等待进程退出", min(95, 50+waitProgress), detail)
		time.Sleep(time.Second)
	}
	return errors.New("Palworld did not exit after the graceful shutdown deadline; process was left untouched")
}

func (s *Service) shutdownWaitSeconds() (int, string) {
	waitSeconds := s.config.ShutdownWaitSeconds
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	players, err := s.rest.players(ctx)
	cancel()
	if err != nil || players.Players == nil {
		return waitSeconds, "暂时无法确认在线人数，按完整通知时间处理"
	}
	if len(players.Players) > 0 {
		return waitSeconds, fmt.Sprintf("当前有 %d 名玩家在线，按完整通知时间处理", len(players.Players))
	}
	return min(waitSeconds, emptyServerShutdownWaitSeconds), "当前没有玩家在线，使用快速安全关闭"
}

func (s *Service) forceStop(expected processSample, report taskReporter) error {
	process, _, err := s.platform.sample(s.config.ProcessName, s.config.InstallDir)
	if err != nil {
		return err
	}
	if !process.Running {
		report("确认进程状态", 100, "Palworld 进程已经退出")
		return nil
	}
	if expected.PID == 0 || expected.StartedAt.IsZero() {
		return errors.New("captured Palworld process identity is incomplete; the running process was left untouched")
	}
	if process.PID != expected.PID || process.StartedAt.IsZero() || !process.StartedAt.Equal(expected.StartedAt) {
		return fmt.Errorf(
			"Palworld process identity changed from PID %d started %s to PID %d started %s; the current process was left untouched",
			expected.PID,
			expected.StartedAt.Format(time.RFC3339Nano),
			process.PID,
			process.StartedAt.Format(time.RFC3339Nano),
		)
	}

	report("终止原进程", 40, fmt.Sprintf("正在强制终止 Palworld PID %d", expected.PID))
	if err := s.platform.terminate(expected.PID, expected.StartedAt); err != nil {
		return fmt.Errorf("terminate Palworld PID %d: %w", expected.PID, err)
	}

	waitStarted := time.Now()
	deadline := waitStarted.Add(15 * time.Second)
	for time.Now().Before(deadline) {
		current, _, sampleErr := s.platform.sample(s.config.ProcessName, s.config.InstallDir)
		if sampleErr != nil {
			return sampleErr
		}
		if !current.Running {
			report("确认进程状态", 100, "已确认原 Palworld 进程退出")
			return nil
		}
		if current.PID != expected.PID ||
			current.StartedAt.IsZero() ||
			!current.StartedAt.Equal(expected.StartedAt) {
			return fmt.Errorf(
				"Palworld PID %d exited but a different process appeared as PID %d; the new process was left untouched",
				expected.PID,
				current.PID,
			)
		}
		waitProgress := int(time.Since(waitStarted) * 50 / (15 * time.Second))
		report("确认进程状态", min(95, 45+waitProgress), "终止信号已发送，正在确认进程退出")
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("Palworld PID %d did not exit after force termination", expected.PID)
}

func (s *Service) start(report taskReporter, registerLog taskLogReporter) error {
	report("准备启动", 5, "正在准备 Palworld 启动日志")
	logDirectory := filepath.Join(s.config.InstallDir, "panel-logs")
	if err := os.MkdirAll(logDirectory, 0o700); err != nil {
		return err
	}
	logName := taskLogName("palworld")
	logPath := filepath.Join(logDirectory, logName)
	registerLog(logName, "帕鲁启动日志")
	report("启动进程", 25, "正在启动 PalServer.exe")
	if err := s.platform.startDetached(s.config.Executable, s.config.InstallDir, s.config.StartArgs, logPath); err != nil {
		return fmt.Errorf("start Palworld: %w", err)
	}

	waitStarted := time.Now()
	deadline := waitStarted.Add(90 * time.Second)
	for time.Now().Before(deadline) {
		process, _, err := s.platform.sample(s.config.ProcessName, s.config.InstallDir)
		if err != nil {
			return err
		}
		if process.Running {
			s.invalidateAPIStatus()
			report("确认进程状态", 100, fmt.Sprintf("已检测到 Palworld 进程 PID %d", process.PID))
			return nil
		}
		waitProgress := int(time.Since(waitStarted) * 40 / (90 * time.Second))
		report("等待进程出现", min(95, 55+waitProgress), "PalServer.exe 已启动，正在等待游戏进程出现")
		time.Sleep(time.Second)
	}
	return errors.New("Palworld process did not appear within 90 seconds")
}

func (s *Service) update(
	wasRunning bool,
	report taskReporter,
	registerLog taskLogReporter,
) (steamUpdateOutcome, error) {
	if wasRunning {
		if err := s.gracefulStop(scaleReporter(report, 0, 25)); err != nil {
			return "", err
		}
	} else {
		report("确认停服状态", 25, "Palworld 当前未运行，可以直接备份并更新")
	}

	report("更新前备份", 30, "正在创建更新前的完整 ZIP 备份")
	if _, err := s.createBackup(); err != nil {
		if wasRunning {
			_ = s.start(scaleReporter(report, 75, 90), registerLog)
		}
		return "", fmt.Errorf("backup before update: %w", err)
	}
	report("更新前备份", 40, "更新前备份已完成")

	logDirectory := filepath.Join(s.config.InstallDir, "panel-logs")
	if err := os.MkdirAll(logDirectory, 0o700); err != nil {
		return "", err
	}
	logName := taskLogName("steamcmd-update")
	logPath := filepath.Join(logDirectory, logName)
	registerLog(logName, "SteamCMD 更新日志")
	updateOutcome, updateErr := s.runSteamCMD(logPath, scaleReporter(report, 40, 75))
	if updateErr != nil {
		if wasRunning && errors.Is(updateErr, errSteamProcessTreeUncertain) {
			report("更新失败", 76, "无法确认 SteamCMD 子进程已全部退出；为避免文件冲突，Palworld 保持停止")
			return "", fmt.Errorf("SteamCMD update failed and the server was left stopped for safety: %w", updateErr)
		}
		if wasRunning {
			report("更新失败回退", 76, "SteamCMD 更新失败，正在尝试恢复原服务器进程")
			restartErr := s.start(scaleReporter(report, 76, 90), registerLog)
			if restartErr == nil {
				restartErr = s.waitForREST(scaleReporter(report, 90, 98))
			}
			if restartErr != nil {
				return "", fmt.Errorf("SteamCMD update failed: %v; rollback restart also failed: %w", updateErr, restartErr)
			}
		}
		return "", fmt.Errorf("SteamCMD update failed: %w", updateErr)
	}
	installedBuildID := s.installedBuildID()
	s.setVersionStatus(installedBuildID, steamVersionStatus{State: versionCheckCurrent})
	if wasRunning {
		if err := s.start(scaleReporter(report, 75, 90), registerLog); err != nil {
			return updateOutcome, err
		}
		return updateOutcome, s.waitForREST(scaleReporter(report, 90, 98))
	}
	if updateOutcome == steamUpdateAlreadyCurrent {
		report("确认更新结果", 98, "Palworld Dedicated Server 已是最新版；服务器保持停止状态")
	} else {
		report("确认更新结果", 98, "SteamCMD 更新完成；服务器保持停止状态")
	}
	return updateOutcome, nil
}

func (s *Service) runSteamCMD(logPath string, report taskReporter) (steamUpdateOutcome, error) {
	logFile, err := os.OpenFile(logPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL|os.O_APPEND, 0o600)
	if err != nil {
		return "", err
	}
	defer logFile.Close()

	noProgressTimeout := s.steamNoProgressTimeout()
	for attempt := 1; attempt <= 2; attempt++ {
		if attempt > 1 {
			if _, err := fmt.Fprintln(logFile, "\n[Hearth] SteamCMD exited cleanly without an app completion marker; retrying once after a possible self-update."); err != nil {
				return "", err
			}
			report("SteamCMD 自更新", 15, "首次运行可能完成了 SteamCMD 自身更新，正在自动重试一次")
		}
		if err := s.runSteamCMDAttempt(logFile, logPath, noProgressTimeout, report); err != nil {
			return "", err
		}
		if outcome, completed := steamUpdateResult(logPath); completed {
			if outcome == steamUpdateAlreadyCurrent {
				report("SteamCMD 更新", 100, "Palworld Dedicated Server 已是最新版，无需下载")
			} else {
				report("SteamCMD 更新", 100, "SteamCMD 已完成下载和文件校验")
			}
			return outcome, nil
		}
	}
	return "", errors.New("SteamCMD exited successfully twice without confirming the Palworld app update")
}

func (s *Service) steamNoProgressTimeout() time.Duration {
	timeout := time.Duration(s.config.SteamCmdNoProgressMinutes) * time.Minute
	if timeout <= 0 {
		return defaultSteamNoProgressMinutes * time.Minute
	}
	return timeout
}

func (s *Service) runSteamCMDAttempt(
	logFile *os.File,
	logPath string,
	noProgressTimeout time.Duration,
	report taskReporter,
) error {
	initialSize := logFileSize(logPath)
	command := exec.Command(s.config.SteamCmd,
		"+force_install_dir", s.config.InstallDir,
		"+login", "anonymous",
		"+app_update", palworldAppID,
		"+quit",
	)
	command.Dir = filepath.Dir(s.config.SteamCmd)
	command.Stdout, command.Stderr = logFile, logFile
	prepareManagedCommand(command)
	report("SteamCMD 更新", 5, "正在启动 SteamCMD")
	if err := command.Start(); err != nil {
		return err
	}
	report("SteamCMD 更新", 10, "SteamCMD 已启动，正在检查、下载并校验服务端文件")

	done := make(chan error, 1)
	go func() {
		done <- command.Wait()
	}()
	pollInterval := 2 * time.Second
	if noProgressTimeout < 8*time.Second {
		pollInterval = max(50*time.Millisecond, noProgressTimeout/4)
	}
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	lastSize := initialSize
	lastProgressAt := time.Now()
	for {
		select {
		case err := <-done:
			return err
		case <-ticker.C:
			size := logFileSize(logPath)
			if size > lastSize {
				lastSize = size
				lastProgressAt = time.Now()
			}
			if time.Since(lastProgressAt) >= noProgressTimeout {
				terminateErr := terminateManagedCommand(command)
				waitErr := <-done
				if terminateErr != nil {
					return fmt.Errorf(
						"SteamCMD produced no log progress for %s; terminate process tree: %w",
						noProgressTimeout,
						terminateErr,
					)
				}
				if waitErr != nil {
					slog.Warn("SteamCMD exited after no-progress termination", "error", waitErr)
				}
				return fmt.Errorf(
					"SteamCMD produced no log progress for %s and was terminated; retry the update",
					noProgressTimeout,
				)
			}
			line := latestLogLine(logPath)
			if line == "" {
				report("SteamCMD 更新", 40, "SteamCMD 正在检查自身更新、下载并校验服务端文件")
				continue
			}
			progress := 40
			if match := steamProgressPattern.FindStringSubmatch(line); len(match) == 2 {
				if value, parseErr := strconv.ParseFloat(match[1], 64); parseErr == nil {
					progress = max(progress, min(95, int(math.Round(value))))
				}
			}
			report("SteamCMD 更新", progress, line)
		}
	}
}

func logFileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}

func steamUpdateResult(path string) (steamUpdateOutcome, bool) {
	const maxCompletionTailBytes int64 = 256 << 10
	file, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", false
	}
	start := max(int64(0), info.Size()-maxCompletionTailBytes)
	data := make([]byte, info.Size()-start)
	read, err := file.ReadAt(data, start)
	if err != nil && !errors.Is(err, io.EOF) {
		return "", false
	}
	data = data[:read]
	text := strings.ToLower(string(data))
	appMarker := "success! app '" + palworldAppID + "'"
	for _, line := range strings.Split(text, "\n") {
		if !strings.Contains(line, appMarker) {
			continue
		}
		if strings.Contains(line, "already up to date") {
			return steamUpdateAlreadyCurrent, true
		}
		if strings.Contains(line, "fully installed") {
			return steamUpdateApplied, true
		}
	}
	return "", false
}

func taskLogName(prefix string) string {
	return prefix + "-" + time.Now().Format("20060102-150405.000000000") + ".log"
}

func latestLogLine(path string) string {
	const maxTailBytes int64 = 16 << 10
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.Size() == 0 {
		return ""
	}
	start := max(int64(0), info.Size()-maxTailBytes)
	buffer := make([]byte, info.Size()-start)
	read, _ := file.ReadAt(buffer, start)
	lines := strings.Split(strings.ReplaceAll(string(buffer[:read]), "\r", ""), "\n")
	for index := len(lines) - 1; index >= 0; index-- {
		line := strings.TrimSpace(lines[index])
		if line == "" {
			continue
		}
		if len(line) > 200 {
			line = line[:200] + "…"
		}
		return line
	}
	return ""
}

func (s *Service) waitForREST(report taskReporter) error {
	waitStarted := time.Now()
	deadline := waitStarted.Add(90 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		report("REST 健康检查", 10, "游戏进程已启动，正在等待 REST API 恢复")
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		_, lastErr = s.rest.info(ctx)
		cancel()
		if lastErr == nil {
			s.invalidateAPIStatus()
			report("REST 健康检查", 100, "游戏进程与 REST API 均已恢复")
			return nil
		}
		waitProgress := int(time.Since(waitStarted) * 80 / (90 * time.Second))
		report("REST 健康检查", min(95, 10+waitProgress), "游戏进程已启动，REST API 尚未恢复")
		time.Sleep(time.Second)
	}
	return fmt.Errorf("Palworld process started but REST API did not become healthy: %w", lastErr)
}

func (s *Service) backup(running bool, report taskReporter) error {
	if running {
		report("保存世界", 15, "正在通过 REST API 保存当前世界")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		err := s.rest.save(ctx)
		cancel()
		if err != nil {
			return fmt.Errorf("save world before backup: %w", err)
		}
		report("保存世界", 45, "世界已保存")
	} else {
		report("确认停服状态", 35, "Palworld 当前未运行，可以直接读取存档")
	}
	report("创建备份", 60, "正在压缩存档和关键配置")
	_, err := s.createBackup()
	if err == nil {
		report("创建备份", 98, "完整 ZIP 备份已创建")
	}
	return err
}

func (s *Service) createBackup() (string, error) {
	path, err := createBackup(s.config.InstallDir, s.config.SettingsFile, s.config.BackupDir)
	if err == nil {
		now := time.Now()
		retention, retentionErr := pruneBackups(
			s.config.BackupDir,
			path,
			time.Duration(s.config.BackupRetentionDays)*24*time.Hour,
			s.config.BackupMaxTotalGB*(1<<30),
			now,
		)
		if retention.Removed > 0 {
			slog.Info(
				"pruned Palworld backups",
				"removed", retention.Removed,
				"freed_bytes", retention.FreedBytes,
			)
		}
		if retentionErr != nil {
			slog.Warn("prune Palworld backups", "error", retentionErr)
		}
		s.mu.Lock()
		s.lastBackup = &now
		s.mu.Unlock()
	}
	return path, err
}

func (s *Service) updateActivity(id, stage string, progress int, detail string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for index := range s.activities {
		if s.activities[index].ID != id {
			continue
		}
		progress = max(s.activities[index].Progress, min(progress, 99))
		s.activities[index].Stage = stage
		s.activities[index].Progress = progress
		s.activities[index].Detail = detail
		s.activities[index].UpdatedAt = time.Now()
		break
	}
}

func (s *Service) addActivityLog(id string, ref panel.LogRef) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for index := range s.activities {
		if s.activities[index].ID != id {
			continue
		}
		s.activities[index].Logs = append(s.activities[index].Logs, ref)
		s.activities[index].UpdatedAt = time.Now()
		break
	}
}

func (s *Service) finishActivity(
	id, action string,
	forced bool,
	updateOutcome steamUpdateOutcome,
	taskErr error,
) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.busy = false
	s.currentAction = ""
	for index := range s.activities {
		if s.activities[index].ID != id {
			continue
		}
		if taskErr != nil {
			s.activities[index].Status = "error"
			s.activities[index].Title = failedActionTitle(action)
			s.activities[index].Detail = taskErr.Error()
			s.activities[index].Stage = "失败"
			slog.Error("palworld task failed", "activity", id, "error", taskErr)
		} else {
			s.activities[index].Status = "success"
			s.activities[index].Title, s.activities[index].Detail = completedActionResult(action, forced, updateOutcome)
			s.activities[index].Stage = "完成"
			s.activities[index].Progress = 100
			slog.Info("palworld task completed", "activity", id, "title", s.activities[index].Title)
		}
		s.activities[index].UpdatedAt = time.Now()
		break
	}
}

func (s *Service) addCompletedActivity(title, detail string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	s.activities = append([]panel.Activity{{
		ID: fmt.Sprintf("pal-%d", time.Now().UnixNano()), GameID: palworldID,
		Title: title, Detail: detail, Status: "success", Stage: "完成", Progress: 100,
		CreatedAt: now, UpdatedAt: now,
	}}, s.activities...)
	if len(s.activities) > 20 {
		s.activities = s.activities[:20]
	}
}

func productionActionTitle(action string) string {
	return map[string]string{
		"start": "正在启动服务器", "stop": "正在停止服务器",
		"restart": "正在重启服务器", "update": "正在备份并更新服务器",
		"backup": "正在备份服务器", "check-update": "正在检查服务端版本",
	}[action]
}

func failedActionTitle(action string) string {
	return map[string]string{
		"start": "服务器启动失败", "stop": "服务器停止失败",
		"restart": "服务器重启失败", "update": "服务器更新失败",
		"backup": "服务器备份失败", "check-update": "版本检查失败",
	}[action]
}

func completedActionResult(action string, forced bool, updateOutcome steamUpdateOutcome) (string, string) {
	if forced {
		switch action {
		case "stop":
			return "服务器已强制停止", "REST 安全关闭不可用，原进程已终止；最近未自动保存的进度可能丢失"
		case "restart":
			return "服务器已强制重启", "REST 安全关闭不可用，原进程已终止并重新启动；健康状态按进程确认"
		}
	}
	if action == "update" && updateOutcome == steamUpdateAlreadyCurrent {
		return "服务器已是最新版", "SteamCMD 已确认 App 2394010 无需下载；备份与运行状态恢复检查已完成"
	}
	results := map[string][2]string{
		"start":        {"服务器已启动", "Palworld 游戏进程已启动"},
		"stop":         {"服务器已安全停止", "世界已保存，Palworld 进程已安全退出"},
		"restart":      {"服务器已安全重启", "世界已保存，游戏进程与 REST API 均已恢复"},
		"update":       {"服务器更新完成", "备份、SteamCMD 更新和恢复检查已完成"},
		"backup":       {"服务器备份完成", "完整 ZIP 备份已创建"},
		"check-update": {"版本检查完成", "已通过 SteamCMD 对比本机与 public 分支 depot manifest"},
	}
	result := results[action]
	return result[0], result[1]
}

func calculateCPU(previousHost, currentHost hostSample, previousProcess, currentProcess processSample, previousAt, currentAt time.Time) (float64, float64) {
	if previousAt.IsZero() || !currentAt.After(previousAt) {
		return 0, 0
	}
	kernelDelta := delta(currentHost.Kernel100NS, previousHost.Kernel100NS)
	userDelta := delta(currentHost.User100NS, previousHost.User100NS)
	idleDelta := delta(currentHost.Idle100NS, previousHost.Idle100NS)
	total := kernelDelta + userDelta
	hostCPU := 0.0
	if total > 0 && total >= idleDelta {
		hostCPU = float64(total-idleDelta) / float64(total) * 100
	}
	processCPU := 0.0
	if previousProcess.Running && currentProcess.Running && previousProcess.PID == currentProcess.PID {
		processDelta := delta(currentProcess.CPU100NS, previousProcess.CPU100NS)
		wall100NS := uint64(currentAt.Sub(previousAt).Nanoseconds() / 100)
		if wall100NS > 0 {
			processCPU = float64(processDelta) / float64(wall100NS*uint64(runtime.NumCPU())) * 100
		}
	}
	return roundPercent(hostCPU), roundPercent(processCPU)
}

func delta(current, previous uint64) uint64 {
	if current < previous {
		return 0
	}
	return current - previous
}

func percent(used, total uint64) float64 {
	if total == 0 {
		return 0
	}
	return roundPercent(float64(used) / float64(total) * 100)
}

func roundPercent(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 100 {
		value = 100
	}
	return math.Round(value*100) / 100
}

func bytesToGB(value uint64) float64 {
	return math.Round(float64(value)/(1<<30)*100) / 100
}

func appendMetric(history []panel.MetricPoint, value float64, at time.Time) []panel.MetricPoint {
	history = append(history, panel.MetricPoint{At: at, Value: value})
	if len(history) > 36 {
		history = history[len(history)-36:]
	}
	return history
}

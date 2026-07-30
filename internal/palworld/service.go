package palworld

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"hearth/internal/config"
	"hearth/internal/panel"
)

const (
	palworldID    = "palworld"
	palworldAppID = "2394010"
)

type Service struct {
	config   config.GameConfig
	platform platformAdapter
	rest     *restClient

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

	apiMu     sync.Mutex
	apiAt     time.Time
	apiStatus apiStatus
}

type apiStatus struct {
	Info         serverInfo
	Metrics      serverMetrics
	PlayerCount  int
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
	service := &Service{config: gameConfig, platform: nativePlatform{}}
	client, err := newRESTClient(gameConfig.RESTURL, gameConfig.RESTUsername, func() (string, error) {
		return readAdminPassword(gameConfig.SettingsFile)
	})
	if err != nil {
		return nil, err
	}
	service.rest = client
	return service, nil
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
	if gameConfig.RESTURL == "" {
		gameConfig.RESTURL = "http://127.0.0.1:8212"
	}
	if gameConfig.RESTUsername == "" {
		gameConfig.RESTUsername = "admin"
	}
	if gameConfig.ShutdownWaitSeconds <= 0 {
		gameConfig.ShutdownWaitSeconds = 30
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
	return activities
}

func (s *Service) Game(id string) (panel.Game, error) {
	if id != palworldID {
		return panel.Game{}, panel.ErrNotFound
	}
	game, _ := s.snapshot()
	return game, nil
}

func (s *Service) RunAction(id, action string) (panel.Activity, error) {
	if id != palworldID {
		return panel.Activity{}, panel.ErrNotFound
	}
	if action != "start" && action != "stop" && action != "restart" && action != "update" && action != "backup" {
		return panel.Activity{}, panel.ErrBadAction
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
	if actionRequiresREST(action, process.Running) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, preflightErr := s.rest.info(ctx)
		cancel()
		if preflightErr != nil {
			return panel.Activity{}, fmt.Errorf("%w: 无法验证帕鲁本机 REST API，已拒绝可能损坏存档的操作：%v", panel.ErrUnsafe, preflightErr)
		}
	}
	if action == "start" || action == "restart" || action == "update" {
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
	activity := panel.Activity{
		ID: fmt.Sprintf("pal-%d", time.Now().UnixNano()), GameID: palworldID,
		Title: productionActionTitle(action), Detail: "任务已进入安全执行队列",
		Status: "running", CreatedAt: time.Now(),
	}
	s.activities = append([]panel.Activity{activity}, s.activities...)
	if len(s.activities) > 20 {
		s.activities = s.activities[:20]
	}
	s.mu.Unlock()

	slog.Info("palworld task queued", "action", action, "activity", activity.ID)
	go s.performAction(action, process.Running, activity.ID)
	return activity, nil
}

func actionRequiresREST(action string, running bool) bool {
	return running && action != "start"
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
	game := panel.Game{
		ID: palworldID, Name: "幻兽帕鲁", ShortName: "PAL", State: "stopped",
		Port: s.config.Port, Tags: []string{"Steam", "REST API", "Windows"},
		PlayersMax:       readNumericOption(s.config.SettingsFile, "ServerPlayerMaxNum"),
		PlayersAvailable: true, PlayersSource: "进程已停止",
		CPUHistory: []panel.MetricPoint{}, MemoryHistory: []panel.MetricPoint{},
	}
	if management, managementErr := readManagementSettings(s.config.SettingsFile); managementErr == nil {
		game.RESTEnabled = management.RESTEnabled
	}
	if world, worldErr := detectActiveWorld(s.config.InstallDir); worldErr == nil {
		game.SaveID = world.ID
		game.SaveDetection = world.Detection
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
	if game.Version == "" {
		game.Version = s.installedBuildID()
	}
	return game, resource
}

func applyAPIStatus(game *panel.Game, status apiStatus) {
	game.RESTAvailable = status.InfoOK
	if status.InfoOK {
		game.Version = status.Info.Version
	}
	if status.MetricsOK {
		game.PlayersOnline = status.Metrics.CurrentPlayerNum
		game.PlayersMax = status.Metrics.MaxPlayerNum
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
		game.PlayersAvailable = true
		game.PlayersSource = "REST API 玩家列表"
	}
}

func (s *Service) cachedAPIStatus() apiStatus {
	s.apiMu.Lock()
	defer s.apiMu.Unlock()
	if time.Since(s.apiAt) < 5*time.Second {
		return s.apiStatus
	}
	ctx, cancel := context.WithTimeout(context.Background(), 1200*time.Millisecond)
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
		if players, err := s.rest.players(ctx); err == nil {
			statusMu.Lock()
			status.PlayerCount, status.PlayerListOK = len(players.Players), true
			statusMu.Unlock()
		}
	}()
	wait.Wait()
	s.apiAt = time.Now()
	s.apiStatus = status
	return status
}

func (s *Service) installedBuildID() string {
	path := filepath.Clean(filepath.Join(s.config.InstallDir, "..", "..", "appmanifest_"+palworldAppID+".acf"))
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

func (s *Service) performAction(action string, wasRunning bool, activityID string) {
	var err error
	switch action {
	case "start":
		err = s.start()
	case "stop":
		err = s.gracefulStop()
	case "restart":
		if err = s.gracefulStop(); err == nil {
			if err = s.start(); err == nil {
				err = s.waitForREST()
			}
		}
	case "update":
		err = s.update(wasRunning)
	case "backup":
		err = s.backup(wasRunning)
	}
	s.finishActivity(activityID, err)
}

func (s *Service) gracefulStop() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	if err := s.rest.save(ctx); err != nil {
		cancel()
		return fmt.Errorf("save world before shutdown: %w", err)
	}
	cancel()

	ctx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
	err := s.rest.shutdown(ctx, s.config.ShutdownWaitSeconds, "服务器维护中，请稍后重新连接。")
	cancel()
	if err != nil {
		return fmt.Errorf("request graceful shutdown: %w", err)
	}
	deadline := time.Now().Add(time.Duration(s.config.ShutdownWaitSeconds+45) * time.Second)
	for time.Now().Before(deadline) {
		process, _, sampleErr := s.platform.sample(s.config.ProcessName, s.config.InstallDir)
		if sampleErr != nil {
			return sampleErr
		}
		if !process.Running {
			return nil
		}
		time.Sleep(time.Second)
	}
	return errors.New("Palworld did not exit after the graceful shutdown deadline; process was left untouched")
}

func (s *Service) start() error {
	logDirectory := filepath.Join(s.config.InstallDir, "panel-logs")
	if err := os.MkdirAll(logDirectory, 0o700); err != nil {
		return err
	}
	logPath := filepath.Join(logDirectory, "palworld-"+time.Now().Format("20060102")+".log")
	if err := s.platform.startDetached(s.config.Executable, s.config.InstallDir, s.config.StartArgs, logPath); err != nil {
		return fmt.Errorf("start Palworld: %w", err)
	}
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		process, _, err := s.platform.sample(s.config.ProcessName, s.config.InstallDir)
		if err != nil {
			return err
		}
		if process.Running {
			s.apiMu.Lock()
			s.apiAt = time.Time{}
			s.apiMu.Unlock()
			return nil
		}
		time.Sleep(time.Second)
	}
	return errors.New("Palworld process did not appear within 90 seconds")
}

func (s *Service) update(wasRunning bool) error {
	if wasRunning {
		if err := s.gracefulStop(); err != nil {
			return err
		}
	}
	if _, err := s.createBackup(); err != nil {
		if wasRunning {
			_ = s.start()
		}
		return fmt.Errorf("backup before update: %w", err)
	}

	logDirectory := filepath.Join(s.config.InstallDir, "panel-logs")
	if err := os.MkdirAll(logDirectory, 0o700); err != nil {
		return err
	}
	logPath := filepath.Join(logDirectory, "steamcmd-"+time.Now().Format("20060102-150405")+".log")
	logFile, err := os.OpenFile(logPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer logFile.Close()

	command := exec.Command(s.config.SteamCmd,
		"+force_install_dir", s.config.InstallDir,
		"+login", "anonymous",
		"+app_update", palworldAppID,
		"+quit",
	)
	command.Dir = filepath.Dir(s.config.SteamCmd)
	command.Stdout, command.Stderr = logFile, logFile
	updateErr := command.Run()
	if updateErr != nil {
		if wasRunning {
			restartErr := s.start()
			if restartErr == nil {
				restartErr = s.waitForREST()
			}
			if restartErr != nil {
				return fmt.Errorf("SteamCMD update failed: %v; rollback restart also failed: %w", updateErr, restartErr)
			}
		}
		return fmt.Errorf("SteamCMD update failed: %w", updateErr)
	}
	if wasRunning {
		if err := s.start(); err != nil {
			return err
		}
		return s.waitForREST()
	}
	return nil
}

func (s *Service) waitForREST() error {
	deadline := time.Now().Add(90 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		_, lastErr = s.rest.info(ctx)
		cancel()
		if lastErr == nil {
			s.apiMu.Lock()
			s.apiAt = time.Time{}
			s.apiMu.Unlock()
			return nil
		}
		time.Sleep(time.Second)
	}
	return fmt.Errorf("Palworld process started but REST API did not become healthy: %w", lastErr)
}

func (s *Service) backup(running bool) error {
	if running {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		err := s.rest.save(ctx)
		cancel()
		if err != nil {
			return fmt.Errorf("save world before backup: %w", err)
		}
	}
	_, err := s.createBackup()
	return err
}

func (s *Service) createBackup() (string, error) {
	path, err := createBackup(s.config.InstallDir, s.config.SettingsFile, s.config.BackupDir)
	if err == nil {
		now := time.Now()
		s.mu.Lock()
		s.lastBackup = &now
		s.mu.Unlock()
	}
	return path, err
}

func (s *Service) finishActivity(id string, taskErr error) {
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
			s.activities[index].Detail = taskErr.Error()
			slog.Error("palworld task failed", "activity", id, "error", taskErr)
		} else {
			s.activities[index].Status = "success"
			s.activities[index].Detail = "任务执行完成"
			slog.Info("palworld task completed", "activity", id, "title", s.activities[index].Title)
		}
		break
	}
}

func (s *Service) addCompletedActivity(title, detail string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.activities = append([]panel.Activity{{
		ID: fmt.Sprintf("pal-%d", time.Now().UnixNano()), GameID: palworldID,
		Title: title, Detail: detail, Status: "success", CreatedAt: time.Now(),
	}}, s.activities...)
	if len(s.activities) > 20 {
		s.activities = s.activities[:20]
	}
}

func productionActionTitle(action string) string {
	return map[string]string{
		"start": "正在启动服务器", "stop": "正在安全停止服务器",
		"restart": "正在安全重启服务器", "update": "正在备份并更新服务器",
		"backup": "正在备份服务器",
	}[action]
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

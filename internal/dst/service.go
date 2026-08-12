package dst

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"hearth/internal/config"
	"hearth/internal/panel"
)

const gameID = "dont-starve-together"

type Service struct {
	mu     sync.Mutex
	config config.GameConfig
	ctx    context.Context
	cancel context.CancelFunc

	master        *exec.Cmd
	caves         *exec.Cmd
	masterRunning bool
	cavesRunning  bool
	busy          bool
	currentAction string
	activities    []panel.Activity

	versionMu          sync.Mutex
	versionStatus      steamVersionStatus
	versionBuildID     string
	versionCheckedAt   time.Time
	versionAttemptedAt time.Time
}

func NewService(gameConfig config.GameConfig) (*Service, error) {
	applyDefaults(&gameConfig)
	if err := validateConfig(gameConfig); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	service := &Service{config: gameConfig, ctx: ctx, cancel: cancel}
	if strings.TrimSpace(gameConfig.SteamCmd) != "" {
		go service.runAutomaticVersionChecks()
	}
	return service, nil
}

func applyDefaults(gameConfig *config.GameConfig) {
	if gameConfig.ProcessName == "" {
		gameConfig.ProcessName = "dontstarve_dedicated_server_nullrenderer_x64.exe"
	}
	if gameConfig.Executable == "" && gameConfig.InstallDir != "" {
		gameConfig.Executable = filepath.Join(gameConfig.InstallDir, "bin64", gameConfig.ProcessName)
	}
	if gameConfig.BackupDir == "" && gameConfig.ClusterDir != "" {
		gameConfig.BackupDir = filepath.Join(gameConfig.ClusterDir, "panel-backups")
	}
	if gameConfig.ShutdownWaitSeconds <= 0 {
		gameConfig.ShutdownWaitSeconds = 30
	}
	if gameConfig.Port <= 0 {
		gameConfig.Port = readINIInt(filepath.Join(gameConfig.ClusterDir, "Master", "server.ini"), "server_port", 11000)
	}
}

func validateConfig(gameConfig config.GameConfig) error {
	paths := map[string]string{
		"installDir": gameConfig.InstallDir, "executable": gameConfig.Executable,
		"clusterDir": gameConfig.ClusterDir,
	}
	for name, value := range paths {
		if strings.TrimSpace(value) == "" || !filepath.IsAbs(value) {
			return fmt.Errorf("%w: DST %s must be an absolute path", panel.ErrInvalid, name)
		}
	}
	if strings.ContainsAny(gameConfig.ProcessName, `/\\`) {
		return fmt.Errorf("%w: invalid DST processName", panel.ErrInvalid)
	}
	for name, path := range map[string]string{
		"executable":        gameConfig.Executable,
		"cluster.ini":       filepath.Join(gameConfig.ClusterDir, "cluster.ini"),
		"Master/server.ini": filepath.Join(gameConfig.ClusterDir, "Master", "server.ini"),
		"Caves/server.ini":  filepath.Join(gameConfig.ClusterDir, "Caves", "server.ini"),
	} {
		if info, err := os.Stat(path); err != nil || info.IsDir() {
			return fmt.Errorf("%w: required DST %s is unavailable: %s", panel.ErrInvalid, name, path)
		}
	}
	return nil
}

func (s *Service) Close() error {
	if s.cancel != nil {
		s.cancel()
	}
	return nil
}

func (s *Service) Overview() panel.Overview {
	game := s.snapshot()
	return panel.Overview{
		Games:      []panel.Game{game},
		Activities: s.activitySnapshot(),
		UpdatedAt:  time.Now(),
		Host: panel.ResourceUsage{
			CPUHistory: []panel.MetricPoint{}, MemoryHistory: []panel.MetricPoint{},
		},
	}
}

func (s *Service) Game(id string) (panel.Game, error) {
	if id != gameID {
		return panel.Game{}, panel.ErrNotFound
	}
	return s.snapshot(), nil
}

func (s *Service) RunAction(id string, request panel.ActionRequest) (panel.Activity, error) {
	if id != gameID {
		return panel.Activity{}, panel.ErrNotFound
	}
	if request.Action != "start" && request.Action != "stop" && request.Action != "restart" && request.Action != "check-update" {
		return panel.Activity{}, panel.ErrBadAction
	}
	if request.Action == "check-update" {
		if err := s.validateSteamVersionCheck(); err != nil {
			return panel.Activity{}, err
		}
		if processRunning(filepath.Base(s.config.SteamCmd)) {
			return panel.Activity{}, fmt.Errorf("%w: 检测到已有 SteamCMD 进程，已拒绝并行版本检查", panel.ErrUnsafe)
		}
	}
	s.mu.Lock()
	if s.busy {
		s.mu.Unlock()
		return panel.Activity{}, panel.ErrBusy
	}
	masterRunning, cavesRunning := s.masterRunning, s.cavesRunning
	externalRunning := processRunning(s.config.ProcessName)
	if request.Action == "start" && (masterRunning || cavesRunning) {
		s.mu.Unlock()
		return panel.Activity{}, fmt.Errorf("%w: DST 已有分片进程运行", panel.ErrUnsafe)
	}
	if request.Action == "start" && externalRunning && !masterRunning && !cavesRunning {
		s.mu.Unlock()
		return panel.Activity{}, fmt.Errorf("%w: 检测到已有 DST 进程；当前只允许管理 Hearth 启动的 Master/Caves", panel.ErrUnsafe)
	}
	if (request.Action == "stop" || request.Action == "restart") && !masterRunning && !cavesRunning {
		s.mu.Unlock()
		if externalRunning {
			return panel.Activity{}, fmt.Errorf("%w: 检测到外部 DST 进程；请先让 Hearth 重新接管后再操作", panel.ErrUnsafe)
		}
		return panel.Activity{}, fmt.Errorf("%w: DST 当前未运行", panel.ErrUnsafe)
	}
	if (request.Action == "stop" || request.Action == "restart") && !request.AllowUnsafe {
		s.mu.Unlock()
		return panel.Activity{}, fmt.Errorf("%w: DST 没有 REST 安全关闭通道；请明确确认强制终止 Master/Caves", panel.ErrUnsafe)
	}
	s.busy = true
	s.currentAction = request.Action
	now := time.Now()
	logs := []panel.LogRef{{ID: fmt.Sprintf("dst-%d-master.log", now.UnixNano()), Label: "DST Master 日志"}, {ID: fmt.Sprintf("dst-%d-caves.log", now.UnixNano()), Label: "DST Caves 日志"}}
	if request.Action == "check-update" {
		logs = []panel.LogRef{{ID: fmt.Sprintf("dst-%d-version.log", now.UnixNano()), Label: "DST Steam 版本检查日志"}}
	}
	activity := panel.Activity{
		ID: fmt.Sprintf("dst-%d", now.UnixNano()), GameID: gameID,
		Action: request.Action, Title: actionTitle(request.Action),
		Detail: "DST Master/Caves 任务已进入执行队列", Status: "running",
		Stage: "排队", Progress: 5, CreatedAt: now, UpdatedAt: now,
		Logs: logs,
	}
	if request.AllowUnsafe {
		activity.Detail = "已确认：DST 没有 REST 安全关闭通道，将按分片终止进程"
	}
	s.activities = append([]panel.Activity{activity}, s.activities...)
	if len(s.activities) > 20 {
		s.activities = s.activities[:20]
	}
	s.mu.Unlock()

	go s.performAction(request.Action, request.AllowUnsafe, activity.ID, activity.Logs)
	return activity, nil
}

func (s *Service) PalworldSettings() (panel.PalworldSettings, error) {
	return panel.PalworldSettings{}, panel.ErrNotFound
}

func (s *Service) UpdatePalworldSettings(panel.PalworldSettingsPatch) (panel.PalworldSettings, error) {
	return panel.PalworldSettings{}, panel.ErrNotFound
}

func (s *Service) WorldOption() (panel.WorldOptionDocument, error) {
	return panel.WorldOptionDocument{}, panel.ErrNotFound
}

func (s *Service) UpdateWorldOption(panel.WorldOptionDocument) (panel.WorldOptionDocument, error) {
	return panel.WorldOptionDocument{}, panel.ErrNotFound
}

func (s *Service) TaskLogPath(id string) (string, bool) {
	if !strings.HasPrefix(id, "dst-") || filepath.Base(id) != id || s.config.ClusterDir == "" {
		return "", false
	}
	return filepath.Join(s.config.ClusterDir, "panel-logs", id), true
}

func (s *Service) activitySnapshot() []panel.Activity {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := append([]panel.Activity(nil), s.activities...)
	for index := range result {
		result[index].Logs = append([]panel.LogRef(nil), result[index].Logs...)
	}
	return result
}

func (s *Service) snapshot() panel.Game {
	s.mu.Lock()
	masterRunning, cavesRunning := s.masterRunning, s.cavesRunning
	busy, action := s.busy, s.currentAction
	s.mu.Unlock()
	state := "stopped"
	externalRunning := processRunning(s.config.ProcessName)
	switch {
	case masterRunning && cavesRunning:
		state = "running"
	case masterRunning || cavesRunning:
		state = "error"
	case externalRunning:
		state = "error"
	}
	if busy {
		switch action {
		case "start":
			state = "starting"
		case "stop", "restart":
			state = "stopping"
		}
	}
	clusterName := filepath.Base(s.config.ClusterDir)
	tokenStatus := "cluster token 未配置"
	if dstClusterTokenPresent(s.config.ClusterDir) {
		tokenStatus = "cluster token 已配置"
	}
	game := panel.Game{
		ID: gameID, Name: "饥荒联机版", ShortName: "DST", State: state,
		Version: s.installedBuildID(), VersionSource: "Steam appmanifest", Port: s.config.Port,
		SaveID: clusterName, SaveDetection: "cluster.ini；" + tokenStatus,
		UpdateSupported: false, BackupSupported: false,
		Tags:             []string{"Steam", "Master/Caves", "无 REST API"},
		PlayersAvailable: false, PlayersSource: "DST 适配器第一阶段暂不读取玩家列表",
		CPUHistory: []panel.MetricPoint{}, MemoryHistory: []panel.MetricPoint{},
	}
	if externalRunning && !masterRunning && !cavesRunning {
		game.PlayersSource = "检测到外部 DST 进程；当前仅管理 Hearth 启动的分片"
	}
	applyVersionStatus(&game, s.versionStatusForBuild(game.Version))
	return game
}

func (s *Service) performAction(action string, allowUnsafe bool, activityID string, logs []panel.LogRef) {
	var err error
	s.updateActivity(activityID, "准备 DST 分片", 15, "检查 cluster.ini、Master 与 Caves 配置")
	s.writeLogs(logs, "Hearth DST task: "+action+"\n")
	switch action {
	case "start":
		err = s.startShards(logs)
	case "stop":
		err = s.stopShards(allowUnsafe)
	case "restart":
		err = s.stopShards(allowUnsafe)
		if err == nil {
			err = s.startShards(logs)
		}
	case "check-update":
		err = s.checkVersion(func(stage string, progress int, detail string) {
			s.updateActivity(activityID, stage, progress, detail)
		}, logs[0].ID)
	}
	if err != nil {
		s.finishActivity(activityID, false, err)
		return
	}
	s.finishActivity(activityID, true, nil)
}

func (s *Service) startShards(logs []panel.LogRef) error {
	if !dstClusterTokenPresent(s.config.ClusterDir) {
		return errors.New("DST 缺少 cluster_token.txt；请先写入集群 Token，Hearth 不会读取或回显 Token 内容")
	}
	master, err := s.startShard("Master", logs[0].ID)
	if err != nil {
		return fmt.Errorf("start DST Master: %w", err)
	}
	s.mu.Lock()
	s.master, s.masterRunning = master, true
	s.mu.Unlock()
	s.updateActivityByLog(logs[0].ID, "Master 分片已启动，启动 Caves", 55)
	caves, err := s.startShard("Caves", logs[1].ID)
	if err != nil {
		_ = terminateCommand(master)
		return fmt.Errorf("start DST Caves: %w", err)
	}
	s.mu.Lock()
	s.caves, s.cavesRunning = caves, true
	s.mu.Unlock()
	s.updateActivityByLog(logs[1].ID, "Master/Caves 已启动", 90)
	return nil
}

func (s *Service) startShard(shard, logID string) (*exec.Cmd, error) {
	logPath, ok := s.TaskLogPath(logID)
	if !ok {
		return nil, errors.New("DST task log path is unavailable")
	}
	if err := os.MkdirAll(filepath.Dir(logPath), 0o700); err != nil {
		return nil, err
	}
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	args := []string{"-console", "-cluster", filepath.Base(s.config.ClusterDir), "-shard", shard,
		"-persistent_storage_root", filepath.Dir(filepath.Dir(s.config.ClusterDir)),
		"-conf_dir", filepath.Base(filepath.Dir(s.config.ClusterDir))}
	command := exec.Command(s.config.Executable, args...)
	command.Dir = s.config.InstallDir
	command.Stdout, command.Stderr = logFile, logFile
	prepareCommand(command)
	if err := command.Start(); err != nil {
		_ = logFile.Close()
		return nil, err
	}
	go func() {
		_ = command.Wait()
		_ = logFile.Close()
		s.mu.Lock()
		if s.master == command {
			s.masterRunning = false
		}
		if s.caves == command {
			s.cavesRunning = false
		}
		s.mu.Unlock()
	}()
	return command, nil
}

func (s *Service) stopShards(_ bool) error {
	s.updateActivityByLog("", "终止 DST Master/Caves 进程", 55)
	s.mu.Lock()
	master, caves := s.master, s.caves
	s.mu.Unlock()
	var firstErr error
	for _, command := range []*exec.Cmd{master, caves} {
		if command == nil || command.Process == nil {
			continue
		}
		if err := terminateCommand(command); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	deadline := time.Now().Add(time.Duration(s.config.ShutdownWaitSeconds) * time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		running := s.masterRunning || s.cavesRunning
		s.mu.Unlock()
		if !running {
			return firstErr
		}
		time.Sleep(100 * time.Millisecond)
	}
	if firstErr != nil {
		return firstErr
	}
	return errors.New("DST 分片进程未在等待时间内退出")
}

func (s *Service) updateActivity(id, stage string, progress int, detail string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for index := range s.activities {
		if s.activities[index].ID == id {
			s.activities[index].Stage, s.activities[index].Progress = stage, progress
			s.activities[index].Detail, s.activities[index].UpdatedAt = detail, time.Now()
			return
		}
	}
}

func (s *Service) updateActivityByLog(_ string, stage string, progress int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for index := range s.activities {
		if s.activities[index].Status == "running" {
			s.activities[index].Stage, s.activities[index].Progress = stage, progress
			s.activities[index].UpdatedAt = time.Now()
			return
		}
	}
}

func (s *Service) finishActivity(id string, success bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.busy, s.currentAction = false, ""
	for index := range s.activities {
		if s.activities[index].ID != id {
			continue
		}
		s.activities[index].Status, s.activities[index].Progress = "success", 100
		s.activities[index].Stage, s.activities[index].UpdatedAt = "完成", time.Now()
		if !success {
			s.activities[index].Status, s.activities[index].Stage = "error", "失败"
			s.activities[index].Detail = err.Error()
		}
	}
}

func (s *Service) writeLogs(logs []panel.LogRef, text string) {
	for _, logRef := range logs {
		path, ok := s.TaskLogPath(logRef.ID)
		if !ok {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			continue
		}
		file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			continue
		}
		_, _ = io.WriteString(file, text)
		_ = file.Close()
	}
}

func actionTitle(action string) string {
	return map[string]string{
		"start": "启动饥荒联机版", "stop": "停止饥荒联机版", "restart": "重启饥荒联机版",
		"check-update": "检查饥荒联机版服务端版本",
	}[action]
}

func readINIInt(path, key string, fallback int) int {
	file, err := os.Open(path)
	if err != nil {
		return fallback
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, ";") || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 || strings.TrimSpace(parts[0]) != key {
			continue
		}
		if value, err := strconv.Atoi(strings.TrimSpace(parts[1])); err == nil && value > 0 {
			return value
		}
	}
	return fallback
}

func dstClusterTokenPresent(directory string) bool {
	info, err := os.Stat(filepath.Join(directory, "cluster_token.txt"))
	return err == nil && !info.IsDir()
}

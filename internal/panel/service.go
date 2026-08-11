package panel

import (
	"errors"
	"fmt"
	"math"
	"sync"
	"time"
)

var (
	ErrNotFound  = errors.New("not found")
	ErrBusy      = errors.New("game already has a running task")
	ErrBadAction = errors.New("unsupported action")
	ErrUnsafe    = errors.New("operation cannot be completed safely")
	ErrInvalid   = errors.New("invalid configuration")
)

type Service interface {
	Overview() Overview
	Game(id string) (Game, error)
	RunAction(id string, request ActionRequest) (Activity, error)
	PalworldSettings() (PalworldSettings, error)
	UpdatePalworldSettings(patch PalworldSettingsPatch) (PalworldSettings, error)
	WorldOption() (WorldOptionDocument, error)
	UpdateWorldOption(document WorldOptionDocument) (WorldOptionDocument, error)
	DSTConfig() (DSTConfigDocument, error)
	UpdateDSTConfig(patch DSTConfigPatch) (DSTConfigDocument, error)
}

type DemoService struct {
	mu         sync.RWMutex
	games      map[string]Game
	activities []Activity
	busy       map[string]bool
	settings   PalworldSettings
	startedAt  time.Time
}

func NewDemoService() *DemoService {
	now := time.Now()
	backup := now.Add(-3*time.Hour - 18*time.Minute)
	return &DemoService{
		startedAt: now.Add(-5*time.Hour - 42*time.Minute),
		busy:      make(map[string]bool),
		games: map[string]Game{
			"palworld": {
				ID: "palworld", Name: "幻兽帕鲁", ShortName: "PAL",
				State: "running", Version: "1.0.1.76890", AvailableVersion: "77142",
				UpdateAvailable: true, VersionCheck: "update_available", PlayersOnline: 2, PlayersMax: 8,
				PlayersMaxKnown: true, Players: []OnlinePlayer{{Name: "Moss"}, {Name: "Nia"}},
				PlayersAvailable: true, PlayersSource: "演示数据",
				UptimeSeconds: 5*3600 + 42*60, CPUPercent: 36.8, MemoryGB: 5.72,
				Port: 8211, SaveID: "E67C6D5A4D25543748EBC2BAB926DC80",
				SaveDetection: "GameUserSettings.ini", LastBackupAt: &backup, Tags: []string{"Steam", "REST API"},
				RESTEnabled: true, RESTAvailable: true,
			},
		},
		activities: []Activity{
			{ID: "a-1", GameID: "palworld", Title: "自动备份完成", Detail: "世界存档与服务器配置已归档", Status: "success", Stage: "完成", Progress: 100, CreatedAt: backup, UpdatedAt: backup},
			{ID: "a-3", GameID: "palworld", Title: "检测到新版本", Detail: "1.0.2.77142 可更新", Status: "warning", Stage: "完成", Progress: 100, CreatedAt: now.Add(-26 * time.Minute), UpdatedAt: now.Add(-26 * time.Minute)},
		},
		settings: demoPalworldSettings(now),
	}
}

func (s *DemoService) Overview() Overview {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refreshDemoMetricsLocked()

	games := make([]Game, 0, len(s.games))
	for _, id := range []string{"palworld"} {
		game := s.games[id]
		game.CPUHistory = history(game.CPUPercent, 7.5, 24)
		game.MemoryHistory = history(game.MemoryGB, .28, 24)
		games = append(games, game)
	}
	return Overview{
		Host: ResourceUsage{
			CPUPercent: 43.2, MemoryPercent: 78.4, MemoryUsedGB: 6.27, MemoryTotalGB: 8,
			DiskPercent: 38.7, DiskUsedGB: 46.4, DiskTotalGB: 120, LoadOne: 1.74,
			CPUHistory: history(43.2, 13, 36), MemoryHistory: history(78.4, 5, 36),
		},
		Games: games, Activities: append([]Activity(nil), s.activities...), UpdatedAt: time.Now(),
	}
}

func (s *DemoService) Game(id string) (Game, error) {
	overview := s.Overview()
	for _, game := range overview.Games {
		if game.ID == id {
			return game, nil
		}
	}
	return Game{}, ErrNotFound
}

func (s *DemoService) RunAction(id string, request ActionRequest) (Activity, error) {
	action := request.Action
	if action != "start" && action != "stop" && action != "restart" &&
		action != "update" && action != "backup" && action != "check-update" {
		return Activity{}, ErrBadAction
	}
	if request.AllowUnsafe && action != "stop" && action != "restart" {
		return Activity{}, fmt.Errorf("%w: 强制回退确认仅适用于停止和重启", ErrInvalid)
	}

	s.mu.Lock()
	game, ok := s.games[id]
	if !ok {
		s.mu.Unlock()
		return Activity{}, ErrNotFound
	}
	if s.busy[id] {
		s.mu.Unlock()
		return Activity{}, ErrBusy
	}
	s.busy[id] = true
	now := time.Now()
	activity := Activity{
		ID: fmt.Sprintf("a-%d", time.Now().UnixNano()), GameID: id,
		Action: action, Title: actionTitle(action), Detail: "任务已进入执行队列",
		Status: "running", Stage: "排队", Progress: 5, CreatedAt: now, UpdatedAt: now,
	}
	s.activities = append([]Activity{activity}, s.activities...)
	if len(s.activities) > 12 {
		s.activities = s.activities[:12]
	}
	if action == "stop" || action == "restart" || action == "update" {
		game.State = "stopping"
	} else if action == "start" {
		game.State = "starting"
	}
	s.games[id] = game
	s.mu.Unlock()

	go s.completeAction(id, action, activity.ID)
	return activity, nil
}

func (s *DemoService) PalworldSettings() (PalworldSettings, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneSettings(s.settings), nil
}

func (s *DemoService) UpdatePalworldSettings(patch PalworldSettingsPatch) (PalworldSettings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for groupIndex := range s.settings.Groups {
		for settingIndex := range s.settings.Groups[groupIndex].Settings {
			setting := &s.settings.Groups[groupIndex].Settings[settingIndex]
			if value, ok := patch.Changes[setting.Key]; ok {
				setting.Value = value
			}
		}
	}
	s.settings.Version = "1.0"
	s.settings.Revision = fmt.Sprintf("demo-%d", time.Now().UnixNano())
	s.settings.LastModified = time.Now()
	now := time.Now()
	s.activities = append([]Activity{{
		ID: fmt.Sprintf("a-%d", time.Now().UnixNano()), GameID: "palworld",
		Title: "INI 配置已保存", Detail: "演示模式未写入服务器文件",
		Status: "success", Stage: "完成", Progress: 100, CreatedAt: now, UpdatedAt: now,
	}}, s.activities...)
	return cloneSettings(s.settings), nil
}

func (s *DemoService) WorldOption() (WorldOptionDocument, error) {
	return WorldOptionDocument{}, ErrNotFound
}

func (s *DemoService) UpdateWorldOption(WorldOptionDocument) (WorldOptionDocument, error) {
	return WorldOptionDocument{}, ErrNotFound
}

func (s *DemoService) DSTConfig() (DSTConfigDocument, error) {
	return DSTConfigDocument{}, ErrNotFound
}

func (s *DemoService) UpdateDSTConfig(DSTConfigPatch) (DSTConfigDocument, error) {
	return DSTConfigDocument{}, ErrNotFound
}

func (s *DemoService) Management() Management {
	return Management{
		Games: []ManagedGame{
			{
				ID: "palworld", Name: "幻兽帕鲁", ShortName: "PAL", Support: "available",
				State: "managed", Detail: "演示模式中的已管理服务器",
				InstallDir: `C:\GameServers\PalServer`, SteamCmd: `C:\SteamCMD\steamcmd.exe`,
			},
			{
				ID: "dont-starve-together", Name: "饥荒联机版", ShortName: "DST",
				Support: "available", State: "not_installed", Detail: "1.3.0 第一阶段支持现有安装接管与 Master/Caves 生命周期",
			},
		},
		Settings: SystemSettings{
			Revision: "demo", InstallRoot: `C:\GameServers`, SteamCmdRoot: `C:\SteamCMD`,
			DiscoveryRoots:      []string{`C:\GameServers`, `C:\SteamCMD`},
			BackupRetentionDays: 30, BackupMaxTotalGB: 20, ShutdownWaitSeconds: 30,
			SteamCmdNoProgressMinutes: 30, PalworldPort: 8211,
			TrustedProxyCIDRs: []string{"127.0.0.0/8", "::1/128"},
			UpdateChannel:     "stable",
		},
	}
}

func (s *DemoService) RefreshDiscovery() (Management, error) {
	return s.Management(), nil
}

func (s *DemoService) AdoptGame(string, AdoptGameRequest) (ManagedGame, error) {
	return ManagedGame{}, ErrInvalid
}

func (s *DemoService) InstallGame(string, InstallGameRequest) (Activity, error) {
	return Activity{}, ErrInvalid
}

func (s *DemoService) UpdateDSTToken(string) (ManagedGame, error) {
	return ManagedGame{}, ErrInvalid
}

func (s *DemoService) UpdateSystemSettings(patch SystemSettingsPatch) (SystemSettings, error) {
	settings := s.Management().Settings
	if patch.Revision != settings.Revision {
		return SystemSettings{}, ErrInvalid
	}
	if patch.UpdateChannel == "" {
		patch.UpdateChannel = settings.UpdateChannel
	}
	settings.InstallRoot = patch.InstallRoot
	settings.SteamCmdRoot = patch.SteamCmdRoot
	settings.DiscoveryRoots = append([]string{}, patch.DiscoveryRoots...)
	settings.BackupRetentionDays = patch.BackupRetentionDays
	settings.BackupMaxTotalGB = patch.BackupMaxTotalGB
	settings.ShutdownWaitSeconds = patch.ShutdownWaitSeconds
	settings.SteamCmdNoProgressMinutes = patch.SteamCmdNoProgressMinutes
	settings.PalworldPort = patch.PalworldPort
	settings.SecureCookies = patch.SecureCookies
	settings.TrustedProxyCIDRs = append([]string{}, patch.TrustedProxyCIDRs...)
	settings.UpdateChannel = patch.UpdateChannel
	settings.RestartRequired = true
	return settings, nil
}

func (s *DemoService) completeAction(id, action, activityID string) {
	time.Sleep(500 * time.Millisecond)
	s.mu.Lock()
	for i := range s.activities {
		if s.activities[i].ID == activityID {
			s.activities[i].Stage = "执行中"
			s.activities[i].Progress = 55
			s.activities[i].Detail = "演示任务正在执行"
			s.activities[i].UpdatedAt = time.Now()
			break
		}
	}
	s.mu.Unlock()

	time.Sleep(900 * time.Millisecond)
	s.mu.Lock()
	defer s.mu.Unlock()

	game := s.games[id]
	switch action {
	case "stop":
		game.State, game.PlayersOnline, game.CPUPercent, game.MemoryGB = "stopped", 0, 0, 0
	case "update":
		game.State = "running"
		game.Version = "1.0.2.77142"
		game.AvailableVersion = ""
		game.UpdateAvailable = false
		game.VersionCheck = "current"
		game.UptimeSeconds = 3
	case "backup":
		now := time.Now()
		game.LastBackupAt = &now
	case "check-update":
		game.VersionCheck = "update_available"
	default:
		game.State = "running"
		game.UptimeSeconds = 3
	}
	s.games[id] = game
	s.busy[id] = false
	for i := range s.activities {
		if s.activities[i].ID == activityID {
			s.activities[i].Status = "success"
			s.activities[i].Stage = "完成"
			s.activities[i].Progress = 100
			s.activities[i].Detail = "演示任务执行完成"
			s.activities[i].UpdatedAt = time.Now()
			break
		}
	}
}

func (s *DemoService) refreshDemoMetricsLocked() {
	elapsed := time.Since(s.startedAt).Seconds()
	if game := s.games["palworld"]; game.State == "running" {
		game.CPUPercent = 35 + math.Sin(elapsed/17)*7
		game.MemoryGB = 5.64 + math.Sin(elapsed/41)*.14
		game.UptimeSeconds = int64(elapsed)
		s.games["palworld"] = game
	}
}

func actionTitle(action string) string {
	return map[string]string{
		"start": "正在启动服务器", "stop": "正在安全停止服务器",
		"restart": "正在重启服务器", "update": "正在更新服务器",
		"backup": "正在备份服务器", "check-update": "正在检查服务端版本",
	}[action]
}

func history(center, spread float64, count int) []MetricPoint {
	points := make([]MetricPoint, count)
	now := time.Now()
	for i := range points {
		value := center + math.Sin(float64(i)*.7)*spread + math.Cos(float64(i)*.31)*spread*.28
		if value < 0 {
			value = 0
		}
		points[i] = MetricPoint{At: now.Add(time.Duration(i-count+1) * 5 * time.Minute), Value: math.Round(value*100) / 100}
	}
	return points
}

func cloneSettings(input PalworldSettings) PalworldSettings {
	output := input
	output.Groups = make([]SettingGroup, len(input.Groups))
	for i, group := range input.Groups {
		output.Groups[i] = group
		output.Groups[i].Settings = append([]Setting(nil), group.Settings...)
	}
	return output
}

func number(value float64) *float64 { return &value }

func demoPalworldSettings(now time.Time) PalworldSettings {
	boolean := func(key, label, description string, value bool, risk string) Setting {
		return Setting{Key: key, Label: label, Description: description, Type: "boolean", Value: value, Default: value, Risk: risk, MemberEditable: IsMemberEditablePalworldSetting(key), RestartRequired: true}
	}
	numeric := func(key, label, description string, value, min, max, step float64, risk string) Setting {
		return Setting{Key: key, Label: label, Description: description, Type: "number", Value: value, Default: value, Min: number(min), Max: number(max), Step: number(step), Risk: risk, MemberEditable: IsMemberEditablePalworldSetting(key), RestartRequired: true}
	}
	return PalworldSettings{
		Version:      "1.0",
		LastModified: now.Add(-2 * 24 * time.Hour),
		Raw: `[/Script/Pal.PalGameWorldSettings]
OptionSettings=(Difficulty=None,DayTimeSpeedRate=1.000000,NightTimeSpeedRate=1.000000,ExpRate=1.000000,ServerPlayerMaxNum=8,ServerName="四人小队",RESTAPIEnabled=True)`,
		Groups: []SettingGroup{
			{ID: "server", Label: "服务器", Description: "名称、人数与访问控制", Settings: []Setting{
				{Key: "ServerName", Label: "服务器名称", Description: "显示在服务器列表中的名称", Type: "text", Value: "四人小队", Default: "Default Palworld Server", MemberEditable: true, RestartRequired: true},
				{Key: "ServerDescription", Label: "服务器描述", Description: "服务器列表和详情中显示的描述", Type: "text", Value: "朋友联机专用", Default: "", MemberEditable: true, RestartRequired: true},
				numeric("ServerPlayerMaxNum", "最大玩家数", "允许同时加入服务器的玩家数量", 8, 1, 32, 1, ""),
				{Key: "ServerPassword", Label: "加入密码", Description: "留空表示无需密码", Type: "password", Value: "", Default: "", Sensitive: true, RestartRequired: true},
				{Key: "AdminPassword", Label: "管理员密码", Description: "REST API、RCON 和游戏内管理使用", Type: "password", Value: "••••••••••", Default: "", Sensitive: true, RestartRequired: true},
			}},
			{ID: "rates", Label: "世界倍率", Description: "时间、经验、生成和采集倍率", Settings: []Setting{
				numeric("DayTimeSpeedRate", "白天流逝速度", "数值越大，白天结束得越快", 1, .1, 5, .1, ""),
				numeric("NightTimeSpeedRate", "夜晚流逝速度", "数值越大，夜晚结束得越快", 1, .1, 5, .1, ""),
				numeric("ExpRate", "经验倍率", "玩家与帕鲁获得经验的倍率", 1, .1, 20, .1, ""),
				numeric("PalCaptureRate", "捕获概率倍率", "影响帕鲁捕获概率", 1, .5, 2, .1, ""),
				numeric("PalSpawnNumRate", "帕鲁生成数量", "提高会显著增加服务器计算量", 1, .5, 3, .1, "performance"),
				numeric("CollectionDropRate", "采集掉落倍率", "采集资源的掉落数量倍率", 1, .5, 5, .1, ""),
			}},
			{ID: "performance", Label: "性能与据点", Description: "会影响内存、CPU 或磁盘压力的选项", Settings: []Setting{
				numeric("BaseCampMaxNumInGuild", "每公会据点上限", "官方范围最高为 10", 4, 1, 10, 1, "performance"),
				numeric("BaseCampWorkerMaxNum", "每据点工作帕鲁数", "提高会增加服务器负载，官方上限为 50", 15, 1, 50, 1, "performance"),
				numeric("MaxBuildingLimitNum", "每玩家建筑上限", "0 表示不限制", 0, 0, 10000, 100, "performance"),
				numeric("ServerReplicatePawnCullDistance", "帕鲁同步距离", "单位厘米，官方范围 5000–15000", 15000, 5000, 15000, 500, "performance"),
				boolean("bIsUseBackupSaveData", "启用世界备份", "游戏自身定期创建世界备份，会增加磁盘写入", true, "disk"),
			}},
			{ID: "access", Label: "连接与管理 API", Description: "跨平台、REST API 和 RCON", Settings: []Setting{
				{Key: "CrossplayPlatforms", Label: "允许平台", Description: "用逗号分隔：Steam、Xbox、PS5、Mac", Type: "text", Value: "Steam,Xbox,PS5,Mac", Default: "Steam,Xbox,PS5,Mac", RestartRequired: true},
				boolean("RESTAPIEnabled", "启用 REST API", "用于玩家、指标、保存和安全关服", true, ""),
				numeric("RESTAPIPort", "REST API 端口", "建议仅允许本机访问", 8212, 1024, 65535, 1, "security"),
				boolean("RCONEnabled", "启用 RCON", "仅在需要兼容旧管理命令时开启", false, "security"),
				numeric("RCONPort", "RCON 端口", "不要直接暴露到公网", 25575, 1024, 65535, 1, "security"),
				boolean("bAllowClientMod", "允许客户端模组", "允许启用模组的客户端加入", false, "security"),
			}},
		},
	}
}

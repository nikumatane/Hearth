package panel

import (
	"context"
	"time"
)

type MetricPoint struct {
	At    time.Time `json:"at"`
	Value float64   `json:"value"`
}

type ResourceUsage struct {
	CPUPercent    float64       `json:"cpuPercent"`
	MemoryPercent float64       `json:"memoryPercent"`
	MemoryUsedGB  float64       `json:"memoryUsedGB"`
	MemoryTotalGB float64       `json:"memoryTotalGB"`
	DiskPercent   float64       `json:"diskPercent"`
	DiskUsedGB    float64       `json:"diskUsedGB"`
	DiskTotalGB   float64       `json:"diskTotalGB"`
	LoadOne       float64       `json:"loadOne"`
	CPUHistory    []MetricPoint `json:"cpuHistory"`
	MemoryHistory []MetricPoint `json:"memoryHistory"`
}

type Game struct {
	ID                       string         `json:"id"`
	Name                     string         `json:"name"`
	ShortName                string         `json:"shortName"`
	State                    string         `json:"state"`
	Version                  string         `json:"version"`
	VersionSource            string         `json:"versionSource,omitempty"`
	AvailableVersion         string         `json:"availableVersion,omitempty"`
	UpdateAvailable          bool           `json:"updateAvailable"`
	UpdateSupported          bool           `json:"updateSupported"`
	BackupSupported          bool           `json:"backupSupported"`
	BackupRequiresStopped    bool           `json:"backupRequiresStopped"`
	UpdateRequiresUnsafeStop bool           `json:"updateRequiresUnsafeStop"`
	VersionCheck             string         `json:"versionCheck,omitempty"`
	PlayersOnline            int            `json:"playersOnline"`
	PlayersMax               int            `json:"playersMax"`
	PlayersMaxKnown          bool           `json:"playersMaxKnown"`
	PlayersAvailable         bool           `json:"playersAvailable"`
	PlayersSource            string         `json:"playersSource,omitempty"`
	Players                  []OnlinePlayer `json:"players,omitempty"`
	UptimeSeconds            int64          `json:"uptimeSeconds"`
	CPUPercent               float64        `json:"cpuPercent"`
	MemoryGB                 float64        `json:"memoryGB"`
	Port                     int            `json:"port"`
	SaveID                   string         `json:"saveId,omitempty"`
	SaveDetection            string         `json:"saveDetection,omitempty"`
	LastBackupAt             *time.Time     `json:"lastBackupAt,omitempty"`
	CPUHistory               []MetricPoint  `json:"cpuHistory"`
	MemoryHistory            []MetricPoint  `json:"memoryHistory"`
	Tags                     []string       `json:"tags"`
	RESTEnabled              bool           `json:"restEnabled"`
	RESTAvailable            bool           `json:"restAvailable"`
}

// OnlinePlayer intentionally contains only the in-game display name. REST
// account names and platform/player identifiers never cross the panel API.
type OnlinePlayer struct {
	Name string `json:"name"`
}

type Activity struct {
	ID        string    `json:"id"`
	GameID    string    `json:"gameId,omitempty"`
	Action    string    `json:"action,omitempty"`
	Title     string    `json:"title"`
	Detail    string    `json:"detail"`
	Status    string    `json:"status"`
	Stage     string    `json:"stage,omitempty"`
	Progress  int       `json:"progress"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
	Logs      []LogRef  `json:"logs,omitempty"`
}

// LogRef links a task record to a log created by that exact execution. Keeping
// only the opaque file ID here lets the log contents remain an on-demand admin
// concern instead of inflating every overview response.
type LogRef struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type Overview struct {
	Host       ResourceUsage `json:"host"`
	Games      []Game        `json:"games"`
	Activities []Activity    `json:"activities"`
	UpdatedAt  time.Time     `json:"updatedAt"`
}

type ActionRequest struct {
	Action      string `json:"action"`
	AllowUnsafe bool   `json:"allowUnsafe,omitempty"`
}

type SettingOption struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

type Setting struct {
	Key             string          `json:"key"`
	Label           string          `json:"label"`
	Description     string          `json:"description"`
	Type            string          `json:"type"`
	Value           any             `json:"value"`
	Default         any             `json:"default"`
	Min             *float64        `json:"min,omitempty"`
	Max             *float64        `json:"max,omitempty"`
	Step            *float64        `json:"step,omitempty"`
	Options         []SettingOption `json:"options,omitempty"`
	Sensitive       bool            `json:"sensitive,omitempty"`
	Risk            string          `json:"risk,omitempty"`
	MemberEditable  bool            `json:"memberEditable,omitempty"`
	RestartRequired bool            `json:"restartRequired"`
	Configured      bool            `json:"configured"`
	ApplyMode       string          `json:"applyMode,omitempty"`
}

var memberEditablePalworldSettings = map[string]struct{}{
	"ServerName":         {},
	"ServerDescription":  {},
	"ServerPlayerMaxNum": {},
	"DayTimeSpeedRate":   {},
	"NightTimeSpeedRate": {},
	"ExpRate":            {},
	"PalCaptureRate":     {},
	"CollectionDropRate": {},
	"Difficulty":         {},
	"DeathPenalty":       {},
	"RandomizerType":     {},
	"bEnableVoiceChat":   {},
	"bEnableFastTravel":  {},
	"bShowPlayerList":    {},
	"DenyTechnologyList": {},
}

// IsMemberEditablePalworldSetting is the server-side allowlist for gameplay
// parameters that a member credential may change. Unknown, sensitive,
// performance, storage, networking, and management parameters fail closed.
func IsMemberEditablePalworldSetting(key string) bool {
	_, ok := memberEditablePalworldSettings[key]
	return ok
}

type SettingGroup struct {
	ID          string    `json:"id"`
	Label       string    `json:"label"`
	Description string    `json:"description"`
	Settings    []Setting `json:"settings"`
}

type PalworldSettings struct {
	Version      string         `json:"version"`
	Revision     string         `json:"revision"`
	Groups       []SettingGroup `json:"groups"`
	Raw          string         `json:"raw"`
	LastModified time.Time      `json:"lastModified"`
}

type PalworldSettingsPatch struct {
	Revision string         `json:"revision"`
	Changes  map[string]any `json:"changes"`
}

type WorldOptionDocument struct {
	WorldID      string    `json:"worldId"`
	Revision     string    `json:"revision"`
	LastModified time.Time `json:"lastModified"`
	Data         []byte    `json:"data"`
}

// DSTConfigFile is one of the fixed, server-side configuration files managed
// by the DST adapter. Content is returned only to an authenticated admin.
type DSTConfigFile struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Format       string    `json:"format"`
	Exists       bool      `json:"exists"`
	Revision     string    `json:"revision"`
	LastModified time.Time `json:"lastModified"`
	Content      string    `json:"content"`
}

type DSTConfigDocument struct {
	Revision     string          `json:"revision"`
	LastModified time.Time       `json:"lastModified"`
	Files        []DSTConfigFile `json:"files"`
}

type DSTConfigPatch struct {
	Revision string            `json:"revision"`
	Files    map[string]string `json:"files"`
}

type DSTSettings struct {
	Version      string         `json:"version"`
	Revision     string         `json:"revision"`
	Groups       []SettingGroup `json:"groups"`
	LastModified time.Time      `json:"lastModified"`
}

type DSTSettingsPatch struct {
	Revision string         `json:"revision"`
	Changes  map[string]any `json:"changes"`
}

type DSTWorldShard struct {
	ID         string         `json:"id"`
	Name       string         `json:"name"`
	Preset     string         `json:"preset"`
	Configured bool           `json:"configured"`
	Groups     []SettingGroup `json:"groups"`
}

type DSTWorldSettings struct {
	Version      string          `json:"version"`
	Revision     string          `json:"revision"`
	Shards       []DSTWorldShard `json:"shards"`
	LastModified time.Time       `json:"lastModified"`
}

type DSTWorldSettingsPatch struct {
	Revision string         `json:"revision"`
	Changes  map[string]any `json:"changes"`
}

type LogFile struct {
	ID        string    `json:"id"`
	Label     string    `json:"label"`
	UpdatedAt time.Time `json:"updatedAt"`
	Content   string    `json:"content,omitempty"`
	Truncated bool      `json:"truncated"`
}

type Logs struct {
	Activities []Activity `json:"activities"`
	Files      []LogFile  `json:"files"`
}

type GameCandidate struct {
	ID                  string `json:"id"`
	InstallDir          string `json:"installDir"`
	ClusterDir          string `json:"clusterDir,omitempty"`
	ClusterTokenPresent bool   `json:"clusterTokenPresent"`
	SteamCmd            string `json:"steamCmd,omitempty"`
	SettingsPresent     bool   `json:"settingsPresent"`
	CanAdopt            bool   `json:"canAdopt"`
	Detail              string `json:"detail"`
}

type ManagedGame struct {
	ID                     string          `json:"id"`
	Name                   string          `json:"name"`
	ShortName              string          `json:"shortName"`
	Support                string          `json:"support"`
	State                  string          `json:"state"`
	Detail                 string          `json:"detail"`
	InstallDir             string          `json:"installDir,omitempty"`
	ClusterDir             string          `json:"clusterDir,omitempty"`
	SteamCmd               string          `json:"steamCmd,omitempty"`
	ClusterTokenConfigured bool            `json:"clusterTokenConfigured"`
	CanInstall             bool            `json:"canInstall"`
	CanAdopt               bool            `json:"canAdopt"`
	Candidates             []GameCandidate `json:"candidates,omitempty"`
	ActiveTaskID           string          `json:"activeTaskId,omitempty"`
	SuggestedClusterDir    string          `json:"suggestedClusterDir,omitempty"`
}

type SystemSettings struct {
	Revision                  string   `json:"revision"`
	InstallRoot               string   `json:"installRoot"`
	SteamCmdRoot              string   `json:"steamCmdRoot"`
	DiscoveryRoots            []string `json:"discoveryRoots"`
	BackupRetentionDays       int      `json:"backupRetentionDays"`
	BackupMaxTotalGB          int64    `json:"backupMaxTotalGB"`
	ShutdownWaitSeconds       int      `json:"shutdownWaitSeconds"`
	SteamCmdNoProgressMinutes int      `json:"steamCmdNoProgressMinutes"`
	PalworldPort              int      `json:"palworldPort"`
	SecureCookies             bool     `json:"secureCookies"`
	TrustedProxyCIDRs         []string `json:"trustedProxyCidrs"`
	UpdateChannel             string   `json:"updateChannel"`
	RestartRequired           bool     `json:"restartRequired"`
}

type Management struct {
	Games    []ManagedGame  `json:"games"`
	Settings SystemSettings `json:"settings"`
}

type AdoptGameRequest struct {
	CandidateID string `json:"candidateId"`
	Confirm     bool   `json:"confirm"`
}

type InstallGameRequest struct {
	SteamCmdRoot string             `json:"steamCmdRoot"`
	Confirm      bool               `json:"confirm"`
	DST          *DSTInstallOptions `json:"dst,omitempty"`
}

// DSTInstallOptions contains only first-run inputs. ClusterToken is written
// directly to Klei's cluster_token.txt and must never be persisted by Hearth.
type DSTInstallOptions struct {
	ClusterDir   string `json:"clusterDir"`
	ClusterName  string `json:"clusterName"`
	ClusterToken string `json:"clusterToken,omitempty"`
}

// DSTTokenPatch replaces the Klei cluster token without persisting it in
// Hearth configuration or returning it from the API.
type DSTTokenPatch struct {
	Token string `json:"token"`
}

type SystemSettingsPatch struct {
	Revision                  string   `json:"revision"`
	InstallRoot               string   `json:"installRoot"`
	SteamCmdRoot              string   `json:"steamCmdRoot"`
	DiscoveryRoots            []string `json:"discoveryRoots"`
	BackupRetentionDays       int      `json:"backupRetentionDays"`
	BackupMaxTotalGB          int64    `json:"backupMaxTotalGB"`
	ShutdownWaitSeconds       int      `json:"shutdownWaitSeconds"`
	SteamCmdNoProgressMinutes int      `json:"steamCmdNoProgressMinutes"`
	PalworldPort              int      `json:"palworldPort"`
	SecureCookies             bool     `json:"secureCookies"`
	TrustedProxyCIDRs         []string `json:"trustedProxyCidrs"`
	UpdateChannel             string   `json:"updateChannel"`
}

type ManagementService interface {
	Management() Management
	RefreshDiscovery() (Management, error)
	AdoptGame(id string, request AdoptGameRequest) (ManagedGame, error)
	InstallGame(id string, request InstallGameRequest) (Activity, error)
	UpdateDSTToken(token string) (ManagedGame, error)
	UpdateSystemSettings(patch SystemSettingsPatch) (SystemSettings, error)
}

type TaskLogLocator interface {
	TaskLogPath(id string) (string, bool)
}

type PanelUpdateStatus struct {
	CurrentVersion  string     `json:"currentVersion"`
	LatestVersion   string     `json:"latestVersion,omitempty"`
	Channel         string     `json:"channel"`
	State           string     `json:"state"`
	Stage           string     `json:"stage"`
	Progress        int        `json:"progress"`
	UpdateAvailable bool       `json:"updateAvailable"`
	CanApply        bool       `json:"canApply"`
	CheckedAt       *time.Time `json:"checkedAt,omitempty"`
	Message         string     `json:"message,omitempty"`
}

type PanelUpdateRequest struct {
	Version           string `json:"version"`
	Confirm           bool   `json:"confirm"`
	ActorCredentialID string `json:"-"`
	ActorRole         string `json:"-"`
	ActorIP           string `json:"-"`
}

type PanelUpdateResult struct {
	State             string
	Version           string
	PreviousVersion   string
	Message           string
	ActorCredentialID string
	ActorRole         string
	ActorIP           string
}

type PanelUpdateService interface {
	UpdateStatus() PanelUpdateStatus
	CheckForUpdate(context.Context) (PanelUpdateStatus, error)
	ApplyUpdate(PanelUpdateRequest) (PanelUpdateStatus, error)
}

type PanelUpdateResultConsumer interface {
	ConsumeUpdateResult() *PanelUpdateResult
	CompleteUpdateResultImport(success bool) error
}

package panel

import "time"

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
	ID               string         `json:"id"`
	Name             string         `json:"name"`
	ShortName        string         `json:"shortName"`
	State            string         `json:"state"`
	Version          string         `json:"version"`
	AvailableVersion string         `json:"availableVersion,omitempty"`
	UpdateAvailable  bool           `json:"updateAvailable"`
	VersionCheck     string         `json:"versionCheck,omitempty"`
	PlayersOnline    int            `json:"playersOnline"`
	PlayersMax       int            `json:"playersMax"`
	PlayersMaxKnown  bool           `json:"playersMaxKnown"`
	PlayersAvailable bool           `json:"playersAvailable"`
	PlayersSource    string         `json:"playersSource,omitempty"`
	Players          []OnlinePlayer `json:"players,omitempty"`
	UptimeSeconds    int64          `json:"uptimeSeconds"`
	CPUPercent       float64        `json:"cpuPercent"`
	MemoryGB         float64        `json:"memoryGB"`
	Port             int            `json:"port"`
	SaveID           string         `json:"saveId,omitempty"`
	SaveDetection    string         `json:"saveDetection,omitempty"`
	LastBackupAt     *time.Time     `json:"lastBackupAt,omitempty"`
	CPUHistory       []MetricPoint  `json:"cpuHistory"`
	MemoryHistory    []MetricPoint  `json:"memoryHistory"`
	Tags             []string       `json:"tags"`
	RESTEnabled      bool           `json:"restEnabled"`
	RESTAvailable    bool           `json:"restAvailable"`
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

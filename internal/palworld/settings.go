package palworld

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"hearth/internal/panel"
)

const (
	settingsVersion = "1.0"
	secretMask      = "••••••••"
	maxSettingsSize = 2 << 20
)

type iniOption struct {
	Key   string
	Value string
}

type iniDocument struct {
	Prefix  string
	Options []iniOption
	Suffix  string
}

type settingDefinition struct {
	Group       string
	Label       string
	Description string
	Type        string
	Min         *float64
	Max         *float64
	Step        *float64
	Options     []panel.SettingOption
	Sensitive   bool
	Risk        string
	RawText     bool
}

type managementSettings struct {
	AdminPassword string
	RESTEnabled   bool
	RESTPort      int
}

var settingDefinitions = map[string]settingDefinition{
	"ServerName":        {Group: "server", Label: "服务器名称", Description: "显示在服务器列表中的名称", Type: "text"},
	"ServerDescription": {Group: "server", Label: "服务器描述", Description: "服务器列表和详情中显示的描述", Type: "text"},
	"ServerPlayerMaxNum": {
		Group: "server", Label: "最大玩家数", Description: "允许同时加入服务器的玩家数量",
		Type: "number", Min: number(1), Max: number(32), Step: number(1),
	},
	"ServerPassword": {Group: "server", Label: "加入密码", Description: "留空表示无需密码", Type: "password", Sensitive: true},
	"AdminPassword":  {Group: "server", Label: "管理员密码", Description: "用于本机 REST API 的管理凭据", Type: "password", Sensitive: true, Risk: "security"},
	"DayTimeSpeedRate": {
		Group: "rates", Label: "白天流逝速度", Description: "数值越大，白天结束得越快",
		Type: "number", Min: number(.1), Max: number(5), Step: number(.1),
	},
	"NightTimeSpeedRate": {
		Group: "rates", Label: "夜晚流逝速度", Description: "数值越大，夜晚结束得越快",
		Type: "number", Min: number(.1), Max: number(5), Step: number(.1),
	},
	"ExpRate": {
		Group: "rates", Label: "经验倍率", Description: "玩家与帕鲁获得经验的倍率",
		Type: "number", Min: number(.1), Max: number(20), Step: number(.1),
	},
	"PalCaptureRate": {
		Group: "rates", Label: "捕获概率倍率", Description: "影响帕鲁捕获概率",
		Type: "number", Min: number(.1), Max: number(10), Step: number(.1),
	},
	"PalSpawnNumRate": {
		Group: "rates", Label: "帕鲁生成数量", Description: "提高会显著增加服务器计算量",
		Type: "number", Min: number(.5), Max: number(3), Step: number(.1), Risk: "performance",
	},
	"CollectionDropRate": {
		Group: "rates", Label: "采集掉落倍率", Description: "采集资源的掉落数量倍率",
		Type: "number", Min: number(.1), Max: number(10), Step: number(.1),
	},
	"BaseCampMaxNum": {
		Group: "performance", Label: "服务器据点总数", Description: "提高会增加服务器计算负载",
		Type: "number", Min: number(1), Max: number(256), Step: number(1), Risk: "performance",
	},
	"BaseCampMaxNumInGuild": {
		Group: "performance", Label: "每公会据点上限", Description: "官方 1.0 文档标注最大值为 10",
		Type: "number", Min: number(1), Max: number(10), Step: number(1), Risk: "performance",
	},
	"BaseCampWorkerMaxNum": {
		Group: "performance", Label: "每据点工作帕鲁数", Description: "提高会增加服务器负载，官方最大值为 50",
		Type: "number", Min: number(1), Max: number(50), Step: number(1), Risk: "performance",
	},
	"MaxBuildingLimitNum": {
		Group: "performance", Label: "每玩家建筑上限", Description: "0 表示不限制",
		Type: "number", Min: number(0), Max: number(10000), Step: number(100), Risk: "performance",
	},
	"ServerReplicatePawnCullDistance": {
		Group: "performance", Label: "帕鲁同步距离", Description: "官方范围为 5000–15000 厘米",
		Type: "number", Min: number(5000), Max: number(15000), Step: number(500), Risk: "performance",
	},
	"bIsUseBackupSaveData": {Group: "performance", Label: "启用世界备份", Description: "游戏自身创建分层世界备份，会增加磁盘写入", Type: "boolean", Risk: "disk"},
	"CrossplayPlatforms":   {Group: "access", Label: "允许平台", Description: "例如 (Steam,Xbox,PS5,Mac)", Type: "text", RawText: true},
	"RESTAPIEnabled":       {Group: "access", Label: "启用 REST API", Description: "面板安全保存和关服所必需", Type: "boolean"},
	"RESTAPIPort": {
		Group: "access", Label: "REST API 端口", Description: "只允许本机访问",
		Type: "number", Min: number(1024), Max: number(65535), Step: number(1), Risk: "security",
	},
	"RCONEnabled": {Group: "access", Label: "启用 RCON", Description: "首版面板不依赖 RCON", Type: "boolean", Risk: "security"},
	"RCONPort": {
		Group: "access", Label: "RCON 端口", Description: "不要直接暴露到公网",
		Type: "number", Min: number(1024), Max: number(65535), Step: number(1), Risk: "security",
	},
	"bAllowClientMod": {Group: "access", Label: "允许客户端模组", Description: "允许启用模组的客户端加入", Type: "boolean", Risk: "security"},
	"Difficulty": {
		Group: "world", Label: "难度", Description: "世界难度预设", Type: "select",
		Options: options("None", "Normal", "Hard"),
	},
	"DeathPenalty": {
		Group: "world", Label: "死亡惩罚", Description: "玩家死亡时掉落的内容", Type: "select",
		Options: options("None", "Item", "ItemAndEquipment", "All"),
	},
	"RandomizerType": {
		Group: "world", Label: "帕鲁随机化", Description: "帕鲁生成随机化模式", Type: "select",
		Options: options("None", "Region", "All"),
	},
	"bHardcore":          {Group: "world", Label: "硬核模式", Description: "死亡后无法复活", Type: "boolean", Risk: "security"},
	"bEnableVoiceChat":   {Group: "world", Label: "语音聊天", Description: "启用游戏内语音聊天", Type: "boolean"},
	"bEnableFastTravel":  {Group: "world", Label: "快速传送", Description: "允许使用快速传送", Type: "boolean"},
	"bShowPlayerList":    {Group: "world", Label: "显示玩家列表", Description: "在 ESC 菜单显示玩家列表", Type: "boolean"},
	"DenyTechnologyList": {Group: "world", Label: "禁用科技列表", Description: "使用官方 Technology ID 列表", Type: "text", RawText: true},
}

var groupMetadata = map[string]struct {
	Label       string
	Description string
}{
	"server":      {"服务器", "名称、人数与访问控制"},
	"rates":       {"世界倍率", "时间、经验、生成和采集倍率"},
	"performance": {"性能与据点", "会影响内存、CPU 或磁盘压力的选项"},
	"access":      {"连接与管理 API", "跨平台、REST API 和 RCON"},
	"world":       {"世界规则", "难度、死亡规则与 1.0 世界功能"},
	"advanced":    {"其他 1.0 参数", "从当前配置自动识别并保留的参数"},
}

var worldOptionManagementKeys = map[string]bool{
	"ServerName": true, "ServerDescription": true, "ServerPlayerMaxNum": true,
	"ServerPassword": true, "AdminPassword": true, "PublicIP": true, "PublicPort": true,
	"CrossplayPlatforms": true, "LogFormatType": true, "Region": true,
	"RESTAPIEnabled": true, "RESTAPIPort": true, "RCONEnabled": true, "RCONPort": true,
	"bUseAuth": true, "BanListURL": true, "bAllowClientMod": true,
}

func parseINI(raw string) (iniDocument, error) {
	marker := "OptionSettings=("
	start := strings.Index(raw, marker)
	if start < 0 {
		return iniDocument{}, errors.New("OptionSettings section is missing")
	}
	contentStart := start + len(marker)
	depth := 1
	inQuote := false
	escaped := false
	end := -1
	for index := contentStart; index < len(raw); index++ {
		char := raw[index]
		if escaped {
			escaped = false
			continue
		}
		if inQuote && char == '\\' {
			escaped = true
			continue
		}
		if char == '"' {
			inQuote = !inQuote
			continue
		}
		if inQuote {
			continue
		}
		switch char {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				end = index
			}
		}
		if end >= 0 {
			break
		}
	}
	if end < 0 || inQuote {
		return iniDocument{}, errors.New("OptionSettings section is not balanced")
	}

	parts, err := splitTopLevel(raw[contentStart:end])
	if err != nil {
		return iniDocument{}, err
	}
	optionsList := make([]iniOption, 0, len(parts))
	seen := make(map[string]bool, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		equal := strings.IndexByte(part, '=')
		if equal <= 0 {
			return iniDocument{}, fmt.Errorf("invalid option %q", part)
		}
		key := strings.TrimSpace(part[:equal])
		if !validOptionKey(key) {
			return iniDocument{}, fmt.Errorf("invalid option key %q", key)
		}
		if seen[key] {
			return iniDocument{}, fmt.Errorf("duplicate option %q", key)
		}
		seen[key] = true
		optionsList = append(optionsList, iniOption{Key: key, Value: strings.TrimSpace(part[equal+1:])})
	}
	return iniDocument{Prefix: raw[:contentStart], Options: optionsList, Suffix: raw[end:]}, nil
}

func splitTopLevel(value string) ([]string, error) {
	var parts []string
	start, depth := 0, 0
	inQuote, escaped := false, false
	for index := 0; index < len(value); index++ {
		char := value[index]
		if escaped {
			escaped = false
			continue
		}
		if inQuote && char == '\\' {
			escaped = true
			continue
		}
		if char == '"' {
			inQuote = !inQuote
			continue
		}
		if inQuote {
			continue
		}
		switch char {
		case '(':
			depth++
		case ')':
			depth--
			if depth < 0 {
				return nil, errors.New("unexpected closing parenthesis")
			}
		case ',':
			if depth == 0 {
				parts = append(parts, value[start:index])
				start = index + 1
			}
		}
	}
	if inQuote || depth != 0 {
		return nil, errors.New("option value is not balanced")
	}
	return append(parts, value[start:]), nil
}

func validOptionKey(value string) bool {
	if value == "" {
		return false
	}
	for _, char := range value {
		if char != '_' && !unicode.IsLetter(char) && !unicode.IsDigit(char) {
			return false
		}
	}
	return true
}

func (document iniDocument) render() string {
	parts := make([]string, len(document.Options))
	for index, option := range document.Options {
		parts[index] = option.Key + "=" + option.Value
	}
	return document.Prefix + strings.Join(parts, ",") + document.Suffix
}

func (document iniDocument) values() map[string]string {
	values := make(map[string]string, len(document.Options))
	for _, option := range document.Options {
		values[option.Key] = option.Value
	}
	return values
}

func readPalworldSettings(path, defaultPath string) (panel.PalworldSettings, error) {
	raw, info, err := readSettingsFile(path)
	if err != nil {
		return panel.PalworldSettings{}, err
	}
	document, err := parseINI(raw)
	if err != nil {
		return panel.PalworldSettings{}, fmt.Errorf("%w: parse PalWorldSettings.ini: %v", panel.ErrInvalid, err)
	}

	defaultValues := map[string]string{}
	if defaultPath != "" {
		if defaultRaw, _, defaultErr := readSettingsFile(defaultPath); defaultErr == nil {
			if defaultDocument, parseErr := parseINI(defaultRaw); parseErr == nil {
				defaultValues = defaultDocument.values()
			}
		}
	}

	groups := buildSettingGroups(document, defaultValues)
	redacted := document
	redacted.Options = append([]iniOption(nil), document.Options...)
	for index := range redacted.Options {
		if definition, ok := settingDefinitions[redacted.Options[index].Key]; ok && definition.Sensitive {
			redacted.Options[index].Value = strconv.Quote(secretMask)
		}
	}
	return panel.PalworldSettings{
		Version: settingsVersion, Groups: groups, Raw: redacted.render(), LastModified: info.ModTime(),
	}, nil
}

func readSettingsFile(path string) (string, os.FileInfo, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", nil, fmt.Errorf("open Palworld settings: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", nil, err
	}
	if info.Size() > maxSettingsSize {
		return "", nil, fmt.Errorf("%w: Palworld settings file is too large", panel.ErrInvalid)
	}
	data, err := io.ReadAll(io.LimitReader(file, maxSettingsSize+1))
	if err != nil {
		return "", nil, err
	}
	return string(data), info, nil
}

func buildSettingGroups(document iniDocument, defaults map[string]string) []panel.SettingGroup {
	groupOrder := []string{"server", "rates", "performance", "access", "world", "advanced"}
	groupSettings := make(map[string][]panel.Setting, len(groupOrder))
	optionsToExpose := append([]iniOption(nil), document.Options...)
	seen := document.values()
	var missingKeys []string
	for key := range defaults {
		if _, ok := seen[key]; !ok {
			missingKeys = append(missingKeys, key)
		}
	}
	sort.Strings(missingKeys)
	for _, key := range missingKeys {
		optionsToExpose = append(optionsToExpose, iniOption{Key: key, Value: defaults[key]})
	}
	for _, option := range optionsToExpose {
		definition, curated := settingDefinitions[option.Key]
		if !curated {
			definition = inferDefinition(option)
		}
		value := decodeOptionValue(option.Value, definition)
		defaultValue := value
		rawDefault, hasDefault := defaults[option.Key]
		if hasDefault {
			defaultValue = decodeOptionValue(rawDefault, definition)
		}
		if definition.Sensitive {
			value = secretMask
			if hasDefault {
				defaultValue = secretMask
			}
		}
		groupSettings[definition.Group] = append(groupSettings[definition.Group], panel.Setting{
			Key: option.Key, Label: definition.Label, Description: definition.Description,
			Type: definition.Type, Value: value, Default: defaultValue,
			Min: definition.Min, Max: definition.Max, Step: definition.Step,
			Options: definition.Options, Sensitive: definition.Sensitive,
			Risk: definition.Risk, RestartRequired: true,
		})
	}

	groups := make([]panel.SettingGroup, 0, len(groupOrder))
	for _, id := range groupOrder {
		settings := groupSettings[id]
		if len(settings) == 0 {
			continue
		}
		metadata := groupMetadata[id]
		groups = append(groups, panel.SettingGroup{
			ID: id, Label: metadata.Label, Description: metadata.Description, Settings: settings,
		})
	}
	return groups
}

func inferDefinition(option iniOption) settingDefinition {
	definition := settingDefinition{
		Group: "advanced", Label: option.Key,
		Description: "当前 Palworld 1.0 配置中的参数；保存时保留原始类型", Type: "text",
	}
	value := strings.TrimSpace(option.Value)
	if strings.EqualFold(value, "True") || strings.EqualFold(value, "False") {
		definition.Type = "boolean"
	} else if _, err := strconv.ParseFloat(value, 64); err == nil {
		definition.Type = "number"
	} else if strings.HasPrefix(value, "(") {
		definition.RawText = true
	}
	return definition
}

func decodeOptionValue(raw string, definition settingDefinition) any {
	raw = strings.TrimSpace(raw)
	switch definition.Type {
	case "boolean":
		return strings.EqualFold(raw, "True")
	case "number":
		if value, err := strconv.ParseFloat(raw, 64); err == nil {
			return value
		}
	case "text", "password", "select":
		if definition.RawText {
			return raw
		}
		if value, err := strconv.Unquote(raw); err == nil {
			return value
		}
	}
	return raw
}

func readAdminPassword(path string) (string, error) {
	settings, err := readManagementSettings(path)
	if err != nil {
		return "", err
	}
	return settings.AdminPassword, nil
}

func readManagementSettings(path string) (managementSettings, error) {
	raw, _, err := readSettingsFile(path)
	if err != nil {
		return managementSettings{}, err
	}
	document, err := parseINI(raw)
	if err != nil {
		return managementSettings{}, err
	}
	values := document.values()
	rawPassword, ok := values["AdminPassword"]
	if !ok {
		return managementSettings{}, errors.New("AdminPassword is missing from PalWorldSettings.ini")
	}
	if password, unquoteErr := strconv.Unquote(rawPassword); unquoteErr == nil {
		rawPassword = password
	}
	restPort, _ := strconv.Atoi(strings.TrimSpace(values["RESTAPIPort"]))
	return managementSettings{
		AdminPassword: rawPassword,
		RESTEnabled:   strings.EqualFold(strings.TrimSpace(values["RESTAPIEnabled"]), "True"),
		RESTPort:      restPort,
	}, nil
}

func readNumericOption(path, key string) int {
	raw, _, err := readSettingsFile(path)
	if err != nil {
		return 0
	}
	document, err := parseINI(raw)
	if err != nil {
		return 0
	}
	value, ok := document.values()[key]
	if !ok {
		return 0
	}
	numberValue, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil {
		return 0
	}
	return int(numberValue)
}

func writePalworldSettings(path, defaultPath string, input panel.PalworldSettings, running bool) (panel.PalworldSettings, error) {
	currentRaw, _, err := readSettingsFile(path)
	if err != nil {
		return panel.PalworldSettings{}, err
	}
	current, err := parseINI(currentRaw)
	if err != nil {
		return panel.PalworldSettings{}, fmt.Errorf("%w: current settings are invalid: %v", panel.ErrInvalid, err)
	}

	base := current
	if strings.TrimSpace(input.Raw) != "" {
		base, err = parseINI(input.Raw)
		if err != nil {
			return panel.PalworldSettings{}, fmt.Errorf("%w: raw settings are invalid: %v", panel.ErrInvalid, err)
		}
	}
	currentValues := current.values()
	for index := range base.Options {
		definition := settingDefinitions[base.Options[index].Key]
		if definition.Sensitive && decodeOptionValue(base.Options[index].Value, definition) == secretMask {
			if existing, ok := currentValues[base.Options[index].Key]; ok {
				base.Options[index].Value = existing
			}
		}
	}

	positions := make(map[string]int, len(base.Options))
	for index, option := range base.Options {
		positions[option.Key] = index
	}
	defaultValues := map[string]string{}
	if defaultPath != "" {
		if defaultRaw, _, defaultErr := readSettingsFile(defaultPath); defaultErr == nil {
			if defaultDocument, parseErr := parseINI(defaultRaw); parseErr == nil {
				defaultValues = defaultDocument.values()
			}
		}
	}
	for _, group := range input.Groups {
		for _, setting := range group.Settings {
			position, ok := positions[setting.Key]
			if !ok {
				defaultValue, existsInDefault := defaultValues[setting.Key]
				if !existsInDefault {
					continue
				}
				base.Options = append(base.Options, iniOption{Key: setting.Key, Value: defaultValue})
				position = len(base.Options) - 1
				positions[setting.Key] = position
			}
			definition, curated := settingDefinitions[setting.Key]
			if !curated {
				definition = inferDefinition(base.Options[position])
			}
			if definition.Sensitive && fmt.Sprint(setting.Value) == secretMask {
				continue
			}
			encoded, encodeErr := encodeSettingValue(setting.Value, definition)
			if encodeErr != nil {
				return panel.PalworldSettings{}, fmt.Errorf("%w: %s: %v", panel.ErrInvalid, setting.Key, encodeErr)
			}
			base.Options[position].Value = encoded
		}
	}

	beforePassword := decodeOptionValue(currentValues["AdminPassword"], settingDefinitions["AdminPassword"])
	afterPassword := decodeOptionValue(base.values()["AdminPassword"], settingDefinitions["AdminPassword"])
	if running && beforePassword != afterPassword {
		return panel.PalworldSettings{}, fmt.Errorf("%w: 服务器运行时不能修改管理员密码，请先安全停服", panel.ErrUnsafe)
	}

	rendered := base.render()
	if _, parseErr := parseINI(rendered); parseErr != nil {
		return panel.PalworldSettings{}, fmt.Errorf("%w: rendered settings failed validation: %v", panel.ErrInvalid, parseErr)
	}
	if err := atomicWriteWithBackup(path, []byte(rendered)); err != nil {
		return panel.PalworldSettings{}, err
	}
	return readPalworldSettings(path, defaultPath)
}

func renderManagementSettings(path string, updates map[string]any) ([]byte, error) {
	raw, _, err := readSettingsFile(path)
	if err != nil {
		return nil, err
	}
	document, err := parseINI(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: current settings are invalid: %v", panel.ErrInvalid, err)
	}
	for index := range document.Options {
		option := &document.Options[index]
		if !worldOptionManagementKeys[option.Key] {
			continue
		}
		value, ok := updates[option.Key]
		if !ok {
			continue
		}
		definition, curated := settingDefinitions[option.Key]
		if !curated {
			definition = inferDefinition(*option)
		}
		encoded, encodeErr := encodeSettingValue(value, definition)
		if encodeErr != nil {
			return nil, fmt.Errorf("%w: %s: %v", panel.ErrInvalid, option.Key, encodeErr)
		}
		option.Value = encoded
	}
	rendered := []byte(document.render())
	if _, parseErr := parseINI(string(rendered)); parseErr != nil {
		return nil, fmt.Errorf("%w: synchronized management settings are invalid: %v", panel.ErrInvalid, parseErr)
	}
	return rendered, nil
}

func encodeSettingValue(value any, definition settingDefinition) (string, error) {
	switch definition.Type {
	case "boolean":
		boolean, ok := value.(bool)
		if !ok {
			return "", errors.New("expected a boolean")
		}
		if boolean {
			return "True", nil
		}
		return "False", nil
	case "number":
		numberValue, err := toFloat(value)
		if err != nil {
			return "", err
		}
		if definition.Min != nil && numberValue < *definition.Min {
			return "", fmt.Errorf("must be at least %v", *definition.Min)
		}
		if definition.Max != nil && numberValue > *definition.Max {
			return "", fmt.Errorf("must be at most %v", *definition.Max)
		}
		return strconv.FormatFloat(numberValue, 'f', -1, 64), nil
	case "select":
		text := fmt.Sprint(value)
		for _, option := range definition.Options {
			if text == option.Value {
				return text, nil
			}
		}
		return "", errors.New("unsupported option")
	case "text", "password":
		text := fmt.Sprint(value)
		if strings.ContainsAny(text, "\r\n") {
			return "", errors.New("line breaks are not allowed")
		}
		if definition.RawText {
			if strings.Contains(text, ",") && !strings.HasPrefix(strings.TrimSpace(text), "(") {
				return "", errors.New("composite values containing commas must be wrapped in parentheses")
			}
			return text, nil
		}
		return strconv.Quote(text), nil
	default:
		return "", errors.New("unsupported setting type")
	}
}

func toFloat(value any) (float64, error) {
	switch typed := value.(type) {
	case float64:
		return typed, nil
	case float32:
		return float64(typed), nil
	case int:
		return float64(typed), nil
	case string:
		return strconv.ParseFloat(typed, 64)
	default:
		return 0, errors.New("expected a number")
	}
}

func atomicWriteWithBackup(path string, data []byte) error {
	backupPath := path + ".panel-backup-" + time.Now().Format("20060102-150405.000000000")
	if err := copyFile(path, backupPath); err != nil {
		return fmt.Errorf("backup current settings: %w", err)
	}
	return atomicWriteFile(path, data)
}

func atomicWriteFile(path string, data []byte) error {
	directory := filepath.Dir(path)
	temp, err := os.CreateTemp(directory, ".palworld-settings-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary settings file: %w", err)
	}
	tempPath := temp.Name()
	cleanup := func() {
		_ = temp.Close()
		_ = os.Remove(tempPath)
	}
	defer cleanup()

	if err := temp.Chmod(0o600); err != nil {
		return err
	}
	if _, err := temp.Write(data); err != nil {
		return err
	}
	if err := temp.Sync(); err != nil {
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}

	if err := replaceFile(tempPath, path); err != nil {
		return fmt.Errorf("replace settings file: %w", err)
	}
	return nil
}

func replaceWorldAndManagement(worldPath string, worldData []byte, settingsPath string, settingsData []byte) error {
	stamp := time.Now().Format("20060102-150405.000000000")
	worldBackup := worldPath + ".panel-backup-" + stamp
	settingsBackup := settingsPath + ".panel-backup-" + stamp
	if err := copyFile(worldPath, worldBackup); err != nil {
		return fmt.Errorf("backup WorldOption.sav: %w", err)
	}
	if err := copyFile(settingsPath, settingsBackup); err != nil {
		return fmt.Errorf("backup PalWorldSettings.ini: %w", err)
	}
	if err := atomicWriteFile(settingsPath, settingsData); err != nil {
		return fmt.Errorf("write synchronized PalWorldSettings.ini: %w", err)
	}
	if err := atomicWriteFile(worldPath, worldData); err != nil {
		rollbackData, rollbackReadErr := os.ReadFile(settingsBackup)
		if rollbackReadErr == nil {
			rollbackReadErr = atomicWriteFile(settingsPath, rollbackData)
		}
		if rollbackReadErr != nil {
			return fmt.Errorf(
				"write WorldOption.sav: %v; PalWorldSettings.ini rollback also failed: %w",
				err, rollbackReadErr,
			)
		}
		return fmt.Errorf("write WorldOption.sav: %w; PalWorldSettings.ini was rolled back", err)
	}
	return nil
}

func copyFile(source, destination string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		_ = output.Close()
		if !ok {
			_ = os.Remove(destination)
		}
	}()
	if _, err := io.Copy(output, input); err != nil {
		return err
	}
	if err := output.Sync(); err != nil {
		return err
	}
	ok = true
	return nil
}

func number(value float64) *float64 {
	return &value
}

func options(values ...string) []panel.SettingOption {
	result := make([]panel.SettingOption, len(values))
	for index, value := range values {
		result[index] = panel.SettingOption{Label: value, Value: value}
	}
	return result
}

package dst

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"hearth/internal/panel"
)

const (
	dstSettingsVersion = "1.0"
	dstSecretMask      = "••••••••"
)

type dstSettingDefinition struct {
	key, fileID, section, iniKey string
	group, label, description    string
	kind                         string
	defaultValue                 any
	min, max, step               *float64
	options                      []panel.SettingOption
	sensitive                    bool
	risk                         string
	maxRunes                     int
}

var dstSettingDefinitions = []dstSettingDefinition{
	{key: "cluster.network.cluster_name", fileID: "cluster", section: "NETWORK", iniKey: "cluster_name", group: "server", label: "服务器名称", description: "显示在服务器浏览器中的名称", kind: "text", defaultValue: "", maxRunes: 128},
	{key: "cluster.network.cluster_description", fileID: "cluster", section: "NETWORK", iniKey: "cluster_description", group: "server", label: "服务器描述", description: "显示在服务器详情中的介绍", kind: "text", defaultValue: "", maxRunes: 1024},
	{key: "cluster.network.cluster_password", fileID: "cluster", section: "NETWORK", iniKey: "cluster_password", group: "server", label: "加入密码", description: "留空表示无需密码；已配置时不会回显明文", kind: "password", defaultValue: "", sensitive: true, risk: "security", maxRunes: 128},
	{key: "cluster.network.cluster_intention", fileID: "cluster", section: "NETWORK", iniKey: "cluster_intention", group: "server", label: "服务器风格", description: "服务器浏览器中展示的玩法倾向", kind: "select", defaultValue: "cooperative", options: dstLabeledOptions("合作", "cooperative", "竞技", "competitive", "社交", "social", "疯狂", "madness")},
	{key: "cluster.network.lan_only_cluster", fileID: "cluster", section: "NETWORK", iniKey: "lan_only_cluster", group: "server", label: "仅局域网", description: "启用后只接受同一局域网内的连接", kind: "boolean", defaultValue: false, risk: "security"},

	{key: "cluster.gameplay.game_mode", fileID: "cluster", section: "GAMEPLAY", iniKey: "game_mode", group: "gameplay", label: "游戏模式", description: "生存、无尽或荒野模式", kind: "select", defaultValue: "survival", options: dstLabeledOptions("生存", "survival", "无尽", "endless", "荒野", "wilderness")},
	{key: "cluster.gameplay.max_players", fileID: "cluster", section: "GAMEPLAY", iniKey: "max_players", group: "gameplay", label: "最大玩家数", description: "允许同时加入集群的玩家数量", kind: "number", defaultValue: 16, min: dstNumber(1), max: dstNumber(64), step: dstNumber(1)},
	{key: "cluster.gameplay.pvp", fileID: "cluster", section: "GAMEPLAY", iniKey: "pvp", group: "gameplay", label: "启用 PvP", description: "允许玩家互相造成伤害", kind: "boolean", defaultValue: false},
	{key: "cluster.gameplay.pause_when_empty", fileID: "cluster", section: "GAMEPLAY", iniKey: "pause_when_empty", group: "gameplay", label: "无人时暂停", description: "没有在线玩家时暂停世界模拟", kind: "boolean", defaultValue: false},
	{key: "cluster.gameplay.vote_enabled", fileID: "cluster", section: "GAMEPLAY", iniKey: "vote_enabled", group: "gameplay", label: "允许投票", description: "启用游戏内投票功能", kind: "boolean", defaultValue: true},
	{key: "cluster.gameplay.autosaver_enabled", fileID: "cluster", section: "GAMEPLAY", iniKey: "autosaver_enabled", group: "gameplay", label: "自动保存", description: "每天结束时自动保存世界", kind: "boolean", defaultValue: true, risk: "disk"},

	{key: "cluster.misc.console_enabled", fileID: "cluster", section: "MISC", iniKey: "console_enabled", group: "maintenance", label: "启用控制台", description: "允许在专服控制台执行 Lua 命令", kind: "boolean", defaultValue: true, risk: "security"},
	{key: "cluster.misc.max_snapshots", fileID: "cluster", section: "MISC", iniKey: "max_snapshots", group: "maintenance", label: "快照保留数", description: "游戏自身可回滚的存档快照数量", kind: "number", defaultValue: 6, min: dstNumber(1), max: dstNumber(20), step: dstNumber(1), risk: "disk"},

	{key: "cluster.shard.bind_ip", fileID: "cluster", section: "SHARD", iniKey: "bind_ip", group: "shard", label: "分片监听地址", description: "同机双分片通常使用 127.0.0.1", kind: "text", defaultValue: "127.0.0.1", risk: "security", maxRunes: 64},
	{key: "cluster.shard.master_ip", fileID: "cluster", section: "SHARD", iniKey: "master_ip", group: "shard", label: "Master 地址", description: "Caves 连接 Master 使用的地址", kind: "text", defaultValue: "127.0.0.1", risk: "security", maxRunes: 64},
	{key: "cluster.shard.master_port", fileID: "cluster", section: "SHARD", iniKey: "master_port", group: "shard", label: "分片通信端口", description: "Master 与其他分片之间的 UDP 通信端口", kind: "number", defaultValue: 10888, min: dstNumber(1024), max: dstNumber(65535), step: dstNumber(1), risk: "security"},
	{key: "cluster.shard.cluster_key", fileID: "cluster", section: "SHARD", iniKey: "cluster_key", group: "shard", label: "分片密钥", description: "分片间认证密钥；已配置时不会回显明文", kind: "password", defaultValue: "", sensitive: true, risk: "security", maxRunes: 256},

	{key: "master.network.server_port", fileID: "master", section: "NETWORK", iniKey: "server_port", group: "master", label: "Master 游戏端口", description: "Master 分片对外使用的 UDP 端口", kind: "number", defaultValue: 10999, min: dstNumber(1024), max: dstNumber(65535), step: dstNumber(1), risk: "security"},
	{key: "master.steam.master_server_port", fileID: "master", section: "STEAM", iniKey: "master_server_port", group: "master", label: "Master Steam 查询端口", description: "同机各分片必须使用不同端口", kind: "number", defaultValue: 27016, min: dstNumber(1024), max: dstNumber(65535), step: dstNumber(1), risk: "security"},
	{key: "master.steam.authentication_port", fileID: "master", section: "STEAM", iniKey: "authentication_port", group: "master", label: "Master Steam 认证端口", description: "同机各分片必须使用不同端口", kind: "number", defaultValue: 8766, min: dstNumber(1024), max: dstNumber(65535), step: dstNumber(1), risk: "security"},

	{key: "caves.network.server_port", fileID: "caves", section: "NETWORK", iniKey: "server_port", group: "caves", label: "Caves 游戏端口", description: "Caves 分片对外使用的 UDP 端口，不能与 Master 相同", kind: "number", defaultValue: 11000, min: dstNumber(1024), max: dstNumber(65535), step: dstNumber(1), risk: "security"},
	{key: "caves.steam.master_server_port", fileID: "caves", section: "STEAM", iniKey: "master_server_port", group: "caves", label: "Caves Steam 查询端口", description: "不能与 Master 的 Steam 查询端口相同", kind: "number", defaultValue: 27017, min: dstNumber(1024), max: dstNumber(65535), step: dstNumber(1), risk: "security"},
	{key: "caves.steam.authentication_port", fileID: "caves", section: "STEAM", iniKey: "authentication_port", group: "caves", label: "Caves Steam 认证端口", description: "不能与 Master 的 Steam 认证端口相同", kind: "number", defaultValue: 8767, min: dstNumber(1024), max: dstNumber(65535), step: dstNumber(1), risk: "security"},
	{key: "caves.shard.name", fileID: "caves", section: "SHARD", iniKey: "name", group: "caves", label: "Caves 分片名称", description: "显示在日志中的非主分片名称", kind: "text", defaultValue: "Caves", maxRunes: 64},
}

var dstGroupMetadata = []struct {
	id, label, description string
}{
	{id: "server", label: "服务器信息", description: "名称、描述、密码与服务器风格"},
	{id: "gameplay", label: "玩法规则", description: "模式、人数、PvP、暂停与保存"},
	{id: "maintenance", label: "维护与快照", description: "控制台和游戏自身快照"},
	{id: "shard", label: "分片通信", description: "Master/Caves 的内部连接与认证；分片角色由托管结构固定"},
	{id: "master", label: "Master 分片", description: "Master 对外端口；主分片角色在高级文件中查看"},
	{id: "caves", label: "Caves 分片", description: "Caves 名称与端口；从分片角色在高级文件中查看"},
}

func (s *Service) DSTSettings() (panel.DSTSettings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	document, err := s.readDSTConfigLocked()
	if err != nil {
		return panel.DSTSettings{}, err
	}
	return buildDSTSettings(document), nil
}

func (s *Service) UpdateDSTSettings(patch panel.DSTSettingsPatch) (panel.DSTSettings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureDSTConfigWritableLocked(); err != nil {
		return panel.DSTSettings{}, err
	}
	current, err := s.readDSTConfigLocked()
	if err != nil {
		return panel.DSTSettings{}, err
	}
	if strings.TrimSpace(patch.Revision) == "" || patch.Revision != current.Revision {
		return panel.DSTSettings{}, fmt.Errorf("%w: DST 配置已变化，请重新读取后再保存", panel.ErrInvalid)
	}
	if len(patch.Changes) == 0 {
		return panel.DSTSettings{}, fmt.Errorf("%w: 至少需要修改一个 DST 参数", panel.ErrInvalid)
	}
	definitions := make(map[string]dstSettingDefinition, len(dstSettingDefinitions))
	for _, definition := range dstSettingDefinitions {
		definitions[definition.key] = definition
	}
	contents := make(map[string]string, len(current.Files))
	for _, file := range current.Files {
		contents[file.ID] = file.Content
	}
	keys := make([]string, 0, len(patch.Changes))
	for key := range patch.Changes {
		if _, ok := definitions[key]; !ok {
			return panel.DSTSettings{}, fmt.Errorf("%w: 不支持的 DST 参数 %q", panel.ErrInvalid, key)
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	changedFiles := make(map[string]string)
	portsChanged := false
	for _, key := range keys {
		definition := definitions[key]
		value, skip, err := encodeDSTSettingValue(definition, patch.Changes[key])
		if err != nil {
			return panel.DSTSettings{}, fmt.Errorf("%w: %s: %v", panel.ErrInvalid, definition.label, err)
		}
		if skip {
			continue
		}
		contents[definition.fileID] = setDSTINIValue(contents[definition.fileID], definition.section, definition.iniKey, value)
		changedFiles[definition.fileID] = contents[definition.fileID]
		portsChanged = portsChanged || strings.HasSuffix(definition.iniKey, "_port") || definition.iniKey == "server_port"
	}
	if len(changedFiles) == 0 {
		return buildDSTSettings(current), nil
	}
	if portsChanged {
		if err := validateDSTPortTopology(contents); err != nil {
			return panel.DSTSettings{}, fmt.Errorf("%w: %v", panel.ErrInvalid, err)
		}
	}
	updated, err := s.writeDSTConfigLocked(current, changedFiles)
	if err != nil {
		return panel.DSTSettings{}, err
	}
	return buildDSTSettings(updated), nil
}

func validateDSTPortTopology(contents map[string]string) error {
	type portValue struct {
		label string
		value int
	}
	definitions := []struct {
		fileID, section, key, label string
		fallback                    int
	}{
		{fileID: "cluster", section: "SHARD", key: "master_port", label: "分片通信端口", fallback: 10888},
		{fileID: "master", section: "NETWORK", key: "server_port", label: "Master 游戏端口", fallback: 10999},
		{fileID: "caves", section: "NETWORK", key: "server_port", label: "Caves 游戏端口", fallback: 11000},
		{fileID: "master", section: "STEAM", key: "master_server_port", label: "Master Steam 查询端口", fallback: 27016},
		{fileID: "caves", section: "STEAM", key: "master_server_port", label: "Caves Steam 查询端口", fallback: 27017},
		{fileID: "master", section: "STEAM", key: "authentication_port", label: "Master Steam 认证端口", fallback: 8766},
		{fileID: "caves", section: "STEAM", key: "authentication_port", label: "Caves Steam 认证端口", fallback: 8767},
	}
	ports := make([]portValue, 0, len(definitions))
	for _, definition := range definitions {
		value := definition.fallback
		if raw, ok := lookupDSTINI(parseDSTINI(contents[definition.fileID]), definition.section, definition.key); ok {
			parsed, err := strconv.Atoi(strings.TrimSpace(raw))
			if err != nil {
				return fmt.Errorf("%s 不是有效端口", definition.label)
			}
			value = parsed
		}
		ports = append(ports, portValue{label: definition.label, value: value})
	}
	for index := range ports {
		for other := index + 1; other < len(ports); other++ {
			if ports[index].value == ports[other].value {
				return fmt.Errorf("%s 与 %s 不能使用相同端口 %d", ports[index].label, ports[other].label, ports[index].value)
			}
		}
	}
	return nil
}

func buildDSTSettings(document panel.DSTConfigDocument) panel.DSTSettings {
	contents := make(map[string]map[string]map[string]string, len(document.Files))
	for _, file := range document.Files {
		contents[file.ID] = parseDSTINI(file.Content)
	}
	groups := make([]panel.SettingGroup, 0, len(dstGroupMetadata))
	for _, metadata := range dstGroupMetadata {
		group := panel.SettingGroup{ID: metadata.id, Label: metadata.label, Description: metadata.description}
		for _, definition := range dstSettingDefinitions {
			if definition.group != metadata.id {
				continue
			}
			raw, configured := lookupDSTINI(contents[definition.fileID], definition.section, definition.iniKey)
			value := definition.defaultValue
			if configured {
				value = decodeDSTSettingValue(definition, raw)
			}
			if definition.sensitive && configured && strings.TrimSpace(raw) != "" {
				value = dstSecretMask
			}
			group.Settings = append(group.Settings, panel.Setting{
				Key: definition.key, Label: definition.label, Description: definition.description,
				Type: definition.kind, Value: value, Default: definition.defaultValue,
				Min: definition.min, Max: definition.max, Step: definition.step,
				Options: definition.options, Sensitive: definition.sensitive, Risk: definition.risk,
				RestartRequired: true, Configured: configured,
			})
		}
		groups = append(groups, group)
	}
	return panel.DSTSettings{Version: dstSettingsVersion, Revision: document.Revision, Groups: groups, LastModified: document.LastModified}
}

func parseDSTINI(content string) map[string]map[string]string {
	result := make(map[string]map[string]string)
	section := ""
	for lineNumber, line := range strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if lineNumber == 0 {
			line = strings.TrimPrefix(line, "\uFEFF")
		}
		if line == "" || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.ToUpper(strings.TrimSpace(line[1 : len(line)-1]))
			if _, ok := result[section]; !ok {
				result[section] = make(map[string]string)
			}
			continue
		}
		if equal := strings.IndexByte(line, '='); equal > 0 && section != "" {
			result[section][strings.ToLower(strings.TrimSpace(line[:equal]))] = strings.TrimSpace(line[equal+1:])
		}
	}
	return result
}

func lookupDSTINI(document map[string]map[string]string, section, key string) (string, bool) {
	values, ok := document[strings.ToUpper(section)]
	if !ok {
		return "", false
	}
	value, ok := values[strings.ToLower(key)]
	return value, ok
}

func setDSTINIValue(content, section, key, value string) string {
	newline := "\n"
	if strings.Contains(content, "\r\n") {
		newline = "\r\n"
	}
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	hadTrailingNewline := strings.HasSuffix(normalized, "\n")
	lines := strings.Split(strings.TrimSuffix(normalized, "\n"), "\n")
	targetSection := strings.ToUpper(section)
	inTarget, sectionFound, updated := false, false, false
	insertAt := len(lines)
	for index, line := range lines {
		trimmed := strings.TrimSpace(strings.TrimPrefix(line, "\uFEFF"))
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			if inTarget && insertAt == len(lines) {
				insertAt = index
			}
			inTarget = strings.EqualFold(strings.TrimSpace(trimmed[1:len(trimmed)-1]), targetSection)
			sectionFound = sectionFound || inTarget
			continue
		}
		if !inTarget {
			continue
		}
		if equal := strings.IndexByte(trimmed, '='); equal > 0 && strings.EqualFold(strings.TrimSpace(trimmed[:equal]), key) {
			lines[index] = key + " = " + value
			updated = true
		}
	}
	if !updated && sectionFound {
		lines = append(lines[:insertAt], append([]string{key + " = " + value}, lines[insertAt:]...)...)
	}
	if !sectionFound {
		if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) != "" {
			lines = append(lines, "")
		}
		lines = append(lines, "["+section+"]", key+" = "+value)
	}
	rendered := strings.Join(lines, newline)
	if hadTrailingNewline || rendered != "" {
		rendered += newline
	}
	return rendered
}

func decodeDSTSettingValue(definition dstSettingDefinition, raw string) any {
	switch definition.kind {
	case "boolean":
		value, err := strconv.ParseBool(strings.TrimSpace(raw))
		if err == nil {
			return value
		}
	case "number":
		value, err := strconv.Atoi(strings.TrimSpace(raw))
		if err == nil {
			return value
		}
	default:
		return raw
	}
	return raw
}

func encodeDSTSettingValue(definition dstSettingDefinition, value any) (string, bool, error) {
	if definition.sensitive {
		text, ok := value.(string)
		if ok && text == dstSecretMask {
			return "", true, nil
		}
	}
	switch definition.kind {
	case "boolean":
		boolean, ok := value.(bool)
		if !ok {
			return "", false, fmt.Errorf("必须是布尔值")
		}
		return strconv.FormatBool(boolean), false, nil
	case "number":
		number, ok := dstNumericValue(value)
		if !ok || math.Trunc(number) != number {
			return "", false, fmt.Errorf("必须是整数")
		}
		if definition.min != nil && number < *definition.min || definition.max != nil && number > *definition.max {
			return "", false, fmt.Errorf("必须在 %.0f–%.0f 之间", *definition.min, *definition.max)
		}
		return strconv.FormatInt(int64(number), 10), false, nil
	case "select":
		text, ok := value.(string)
		if !ok {
			return "", false, fmt.Errorf("必须是文本选项")
		}
		for _, option := range definition.options {
			if text == option.Value {
				return text, false, nil
			}
		}
		return "", false, fmt.Errorf("不是支持的选项")
	default:
		text, ok := value.(string)
		if !ok {
			return "", false, fmt.Errorf("必须是文本")
		}
		if strings.ContainsAny(text, "\r\n\x00") || !utf8.ValidString(text) {
			return "", false, fmt.Errorf("不能包含换行或无效字符")
		}
		if definition.maxRunes > 0 && utf8.RuneCountInString(text) > definition.maxRunes {
			return "", false, fmt.Errorf("最多允许 %d 个字符", definition.maxRunes)
		}
		return text, false, nil
	}
}

func dstNumericValue(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case int:
		return float64(typed), true
	case string:
		number, err := strconv.ParseFloat(typed, 64)
		return number, err == nil
	default:
		return 0, false
	}
}

func dstNumber(value float64) *float64 { return &value }

func dstLabeledOptions(values ...string) []panel.SettingOption {
	result := make([]panel.SettingOption, 0, len(values)/2)
	for index := 0; index+1 < len(values); index += 2 {
		result = append(result, panel.SettingOption{Label: values[index], Value: values[index+1]})
	}
	return result
}

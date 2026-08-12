package dst

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"hearth/internal/panel"
)

const dstWorldSettingsVersion = "1.0"

type dstWorldSettingDefinition struct {
	shards             string
	overrideKey, group string
	label, description string
	defaultValue       string
	options            []panel.SettingOption
}

var (
	dstFrequencyOptions        = dstLabeledOptions("无", "never", "较少", "rare", "默认", "default", "较多", "often", "大量", "always")
	dstSeasonOptions           = dstLabeledOptions("无此季节", "noseason", "极短", "veryshortseason", "短", "shortseason", "默认", "default", "长", "longseason", "极长", "verylongseason", "随机", "random")
	dstWorldSettingDefinitions = []dstWorldSettingDefinition{
		{shards: "both", overrideKey: "world_size", group: "generation", label: "世界大小", description: "影响新世界生成范围和生成耗时", defaultValue: "default", options: dstLabeledOptions("小", "small", "中", "medium", "默认", "default", "大", "huge")},
		{shards: "both", overrideKey: "branching", group: "generation", label: "道路分支", description: "控制世界地形分支复杂度", defaultValue: "default", options: dstLabeledOptions("无", "never", "较少", "least", "默认", "default", "较多", "most")},
		{shards: "both", overrideKey: "loop", group: "generation", label: "世界环路", description: "控制地形是否形成环路", defaultValue: "default", options: dstLabeledOptions("无", "never", "默认", "default", "总是", "always")},
		{shards: "master", overrideKey: "roads", group: "generation", label: "道路", description: "控制地表道路生成", defaultValue: "default", options: dstLabeledOptions("无", "never", "默认", "default")},
		{shards: "master", overrideKey: "touchstone", group: "generation", label: "试金石", description: "控制试金石生成数量", defaultValue: "default", options: dstFrequencyOptions},
		{shards: "master", overrideKey: "boons", group: "generation", label: "前人遗物", description: "控制骸骨与资源组合生成频率", defaultValue: "default", options: dstFrequencyOptions},

		{shards: "master", overrideKey: "day", group: "time", label: "昼夜构成", description: "控制白天、黄昏和夜晚的比例", defaultValue: "default", options: dstLabeledOptions("默认", "default", "长白天", "longday", "长黄昏", "longdusk", "长夜晚", "longnight", "无白天", "noday", "无黄昏", "nodusk", "无夜晚", "nonight", "仅白天", "onlyday", "仅黄昏", "onlydusk", "仅夜晚", "onlynight")},
		{shards: "master", overrideKey: "season_start", group: "time", label: "起始季节", description: "新世界生成时的初始季节", defaultValue: "default", options: dstLabeledOptions("默认", "default", "秋季", "autumn", "冬季", "winter", "春季", "spring", "夏季", "summer", "随机", "random")},
		{shards: "master", overrideKey: "autumn", group: "time", label: "秋季长度", description: "控制秋季持续时间", defaultValue: "default", options: dstSeasonOptions},
		{shards: "master", overrideKey: "winter", group: "time", label: "冬季长度", description: "控制冬季持续时间", defaultValue: "default", options: dstSeasonOptions},
		{shards: "master", overrideKey: "spring", group: "time", label: "春季长度", description: "控制春季持续时间", defaultValue: "default", options: dstSeasonOptions},
		{shards: "master", overrideKey: "summer", group: "time", label: "夏季长度", description: "控制夏季持续时间", defaultValue: "default", options: dstSeasonOptions},

		{shards: "master", overrideKey: "weather", group: "weather", label: "降雨", description: "控制普通降雨频率", defaultValue: "default", options: dstFrequencyOptions},
		{shards: "master", overrideKey: "lightning", group: "weather", label: "闪电", description: "控制闪电频率", defaultValue: "default", options: dstFrequencyOptions},
		{shards: "master", overrideKey: "frograin", group: "weather", label: "青蛙雨", description: "控制青蛙雨事件频率", defaultValue: "default", options: dstFrequencyOptions},
		{shards: "master", overrideKey: "wildfires", group: "weather", label: "野火", description: "控制夏季野火发生频率", defaultValue: "default", options: dstFrequencyOptions},

		{shards: "master", overrideKey: "grass", group: "resources", label: "草", description: "控制草簇数量", defaultValue: "default", options: dstFrequencyOptions},
		{shards: "master", overrideKey: "sapling", group: "resources", label: "树枝", description: "控制树苗数量", defaultValue: "default", options: dstFrequencyOptions},
		{shards: "master", overrideKey: "berrybush", group: "resources", label: "浆果丛", description: "控制浆果丛数量", defaultValue: "default", options: dstFrequencyOptions},
		{shards: "master", overrideKey: "carrot", group: "resources", label: "胡萝卜", description: "控制地面胡萝卜数量", defaultValue: "default", options: dstFrequencyOptions},
		{shards: "master", overrideKey: "flint", group: "resources", label: "燧石", description: "控制地面燧石数量", defaultValue: "default", options: dstFrequencyOptions},
		{shards: "master", overrideKey: "rock", group: "resources", label: "岩石", description: "控制矿石资源数量", defaultValue: "default", options: dstFrequencyOptions},
		{shards: "master", overrideKey: "rock_ice", group: "resources", label: "冰矿", description: "控制冰矿数量", defaultValue: "default", options: dstFrequencyOptions},
		{shards: "master", overrideKey: "trees", group: "resources", label: "常青树", description: "控制常青树数量", defaultValue: "default", options: dstFrequencyOptions},
		{shards: "master", overrideKey: "flowers", group: "resources", label: "花", description: "控制花朵数量", defaultValue: "default", options: dstFrequencyOptions},
		{shards: "master", overrideKey: "reeds", group: "resources", label: "芦苇", description: "控制芦苇数量", defaultValue: "default", options: dstFrequencyOptions},

		{shards: "master", overrideKey: "beefalo", group: "creatures", label: "皮弗娄牛", description: "控制牛群数量", defaultValue: "default", options: dstFrequencyOptions},
		{shards: "master", overrideKey: "bees", group: "creatures", label: "蜜蜂", description: "控制蜂巢和蜜蜂数量", defaultValue: "default", options: dstFrequencyOptions},
		{shards: "master", overrideKey: "birds", group: "creatures", label: "鸟", description: "控制鸟类出现频率", defaultValue: "default", options: dstFrequencyOptions},
		{shards: "master", overrideKey: "butterfly", group: "creatures", label: "蝴蝶", description: "控制蝴蝶出现频率", defaultValue: "default", options: dstFrequencyOptions},
		{shards: "master", overrideKey: "rabbits", group: "creatures", label: "兔子", description: "控制兔洞数量", defaultValue: "default", options: dstFrequencyOptions},
		{shards: "master", overrideKey: "pigs", group: "creatures", label: "猪人", description: "控制猪人房数量", defaultValue: "default", options: dstFrequencyOptions},
		{shards: "master", overrideKey: "moles", group: "creatures", label: "鼹鼠", description: "控制鼹鼠数量", defaultValue: "default", options: dstFrequencyOptions},

		{shards: "master", overrideKey: "hounds", group: "threats", label: "猎犬袭击", description: "控制猎犬袭击频率", defaultValue: "default", options: dstFrequencyOptions},
		{shards: "master", overrideKey: "spiders", group: "threats", label: "蜘蛛", description: "控制蜘蛛巢数量", defaultValue: "default", options: dstFrequencyOptions},
		{shards: "master", overrideKey: "merm", group: "threats", label: "鱼人", description: "控制鱼人房数量", defaultValue: "default", options: dstFrequencyOptions},
		{shards: "master", overrideKey: "tentacles", group: "threats", label: "触手", description: "控制沼泽触手数量", defaultValue: "default", options: dstFrequencyOptions},
		{shards: "master", overrideKey: "krampus", group: "threats", label: "坎普斯", description: "控制坎普斯出现频率", defaultValue: "default", options: dstFrequencyOptions},
		{shards: "master", overrideKey: "deerclops", group: "bosses", label: "独眼巨鹿", description: "控制独眼巨鹿出现频率", defaultValue: "default", options: dstFrequencyOptions},
		{shards: "master", overrideKey: "bearger", group: "bosses", label: "熊獾", description: "控制熊獾出现频率", defaultValue: "default", options: dstFrequencyOptions},
		{shards: "master", overrideKey: "goosemoose", group: "bosses", label: "麋鹿鹅", description: "控制麋鹿鹅出现频率", defaultValue: "default", options: dstFrequencyOptions},
		{shards: "master", overrideKey: "dragonfly", group: "bosses", label: "龙蝇", description: "控制龙蝇出现频率", defaultValue: "default", options: dstFrequencyOptions},

		{shards: "caves", overrideKey: "grass", group: "resources", label: "草", description: "控制洞穴草簇数量", defaultValue: "default", options: dstFrequencyOptions},
		{shards: "caves", overrideKey: "sapling", group: "resources", label: "树枝", description: "控制洞穴树苗数量", defaultValue: "default", options: dstFrequencyOptions},
		{shards: "caves", overrideKey: "mushtree", group: "resources", label: "蘑菇树", description: "控制蘑菇树数量", defaultValue: "default", options: dstFrequencyOptions},
		{shards: "caves", overrideKey: "fern", group: "resources", label: "蕨类", description: "控制蕨类植物数量", defaultValue: "default", options: dstFrequencyOptions},
		{shards: "caves", overrideKey: "flower_cave", group: "resources", label: "荧光花", description: "控制荧光花数量", defaultValue: "default", options: dstFrequencyOptions},
		{shards: "caves", overrideKey: "wormlights", group: "resources", label: "发光浆果", description: "控制发光浆果数量", defaultValue: "default", options: dstFrequencyOptions},
		{shards: "caves", overrideKey: "cave_ponds", group: "resources", label: "洞穴池塘", description: "控制洞穴池塘数量", defaultValue: "default", options: dstFrequencyOptions},

		{shards: "caves", overrideKey: "bunnymen", group: "creatures", label: "兔人", description: "控制兔人房数量", defaultValue: "default", options: dstFrequencyOptions},
		{shards: "caves", overrideKey: "rocky", group: "creatures", label: "石虾", description: "控制石虾数量", defaultValue: "default", options: dstFrequencyOptions},
		{shards: "caves", overrideKey: "monkey", group: "creatures", label: "穴居猴", description: "控制穴居猴数量", defaultValue: "default", options: dstFrequencyOptions},
		{shards: "caves", overrideKey: "slurtles", group: "creatures", label: "蛞蝓龟", description: "控制蛞蝓龟数量", defaultValue: "default", options: dstFrequencyOptions},
		{shards: "caves", overrideKey: "slurper", group: "creatures", label: "缀食者", description: "控制缀食者数量", defaultValue: "default", options: dstFrequencyOptions},

		{shards: "caves", overrideKey: "cave_spiders", group: "threats", label: "洞穴蜘蛛", description: "控制洞穴蜘蛛巢数量", defaultValue: "default", options: dstFrequencyOptions},
		{shards: "caves", overrideKey: "bats", group: "threats", label: "洞穴蝙蝠", description: "控制蝙蝠出现频率", defaultValue: "default", options: dstFrequencyOptions},
		{shards: "caves", overrideKey: "caveworm", group: "threats", label: "深渊蠕虫", description: "控制深渊蠕虫袭击频率", defaultValue: "default", options: dstFrequencyOptions},
		{shards: "caves", overrideKey: "nightmarecreatures", group: "threats", label: "梦魇生物", description: "控制梦魇生物出现频率", defaultValue: "default", options: dstFrequencyOptions},
		{shards: "caves", overrideKey: "earthquakes", group: "threats", label: "地震", description: "控制洞穴地震频率", defaultValue: "default", options: dstFrequencyOptions},
	}
)

var dstWorldGroupMetadata = []struct{ id, label, description string }{
	{id: "generation", label: "世界生成", description: "世界大小、分支、环路和初始地形内容"},
	{id: "time", label: "时间与季节", description: "昼夜比例、起始季节和四季长度"},
	{id: "weather", label: "天气与环境", description: "降雨、闪电、青蛙雨和野火"},
	{id: "resources", label: "资源", description: "基础资源和植物的生成密度"},
	{id: "creatures", label: "中立生物", description: "常见中立生物和对应巢穴"},
	{id: "threats", label: "敌对生物", description: "袭击、敌对生物和环境威胁"},
	{id: "bosses", label: "季节 Boss", description: "地表季节 Boss 出现频率"},
}

func (s *Service) DSTWorldSettings() (panel.DSTWorldSettings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	document, err := s.readDSTConfigLocked()
	if err != nil {
		return panel.DSTWorldSettings{}, err
	}
	return buildDSTWorldSettings(document)
}

func (s *Service) UpdateDSTWorldSettings(patch panel.DSTWorldSettingsPatch) (panel.DSTWorldSettings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureDSTConfigWritableLocked(); err != nil {
		return panel.DSTWorldSettings{}, err
	}
	current, err := s.readDSTConfigLocked()
	if err != nil {
		return panel.DSTWorldSettings{}, err
	}
	if strings.TrimSpace(patch.Revision) == "" || patch.Revision != current.Revision {
		return panel.DSTWorldSettings{}, fmt.Errorf("%w: DST 配置已变化，请重新读取后再保存", panel.ErrInvalid)
	}
	if len(patch.Changes) == 0 {
		return panel.DSTWorldSettings{}, fmt.Errorf("%w: 至少需要修改一项世界规则", panel.ErrInvalid)
	}
	definitions := make(map[string]dstWorldSettingDefinition)
	for _, shard := range []string{"master", "caves"} {
		for _, definition := range dstWorldSettingDefinitions {
			if dstWorldDefinitionApplies(definition, shard) {
				definitions[shard+".world."+definition.overrideKey] = definition
			}
		}
	}
	contents := make(map[string]string, len(current.Files))
	for _, file := range current.Files {
		contents[file.ID] = file.Content
	}
	keys := make([]string, 0, len(patch.Changes))
	for key := range patch.Changes {
		if _, ok := definitions[key]; !ok {
			return panel.DSTWorldSettings{}, fmt.Errorf("%w: 不支持的世界规则 %q", panel.ErrInvalid, key)
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	changedFiles := make(map[string]string)
	for _, key := range keys {
		definition := definitions[key]
		value, ok := patch.Changes[key].(string)
		if !ok || !dstWorldOptionAllowed(definition.options, value) {
			return panel.DSTWorldSettings{}, fmt.Errorf("%w: %s 不是支持的选项", panel.ErrInvalid, definition.label)
		}
		shard := strings.SplitN(key, ".", 2)[0]
		fileID := shard + "-world"
		updated, err := setDSTWorldgenOverride(contents[fileID], definition.overrideKey, strconv.Quote(value))
		if err != nil {
			return panel.DSTWorldSettings{}, fmt.Errorf("%w: 更新 %s: %v", panel.ErrInvalid, definition.label, err)
		}
		contents[fileID] = updated
		changedFiles[fileID] = updated
	}
	updated, err := s.writeDSTConfigLocked(current, changedFiles)
	if err != nil {
		return panel.DSTWorldSettings{}, err
	}
	return buildDSTWorldSettings(updated)
}

func buildDSTWorldSettings(document panel.DSTConfigDocument) (panel.DSTWorldSettings, error) {
	result := panel.DSTWorldSettings{Version: dstWorldSettingsVersion, Revision: document.Revision, LastModified: document.LastModified}
	for _, shardID := range []string{"master", "caves"} {
		file := dstConfigFileByID(document, shardID+"-world")
		parsed, err := parseDSTWorldgen(file.Content)
		if err != nil {
			return panel.DSTWorldSettings{}, fmt.Errorf("%w: %s 世界规则无法解析: %v", panel.ErrInvalid, shardID, err)
		}
		shard := panel.DSTWorldShard{ID: shardID, Name: map[string]string{"master": "地表世界", "caves": "洞穴世界"}[shardID], Preset: parsed.preset, Configured: file.Exists}
		for _, metadata := range dstWorldGroupMetadata {
			group := panel.SettingGroup{ID: metadata.id, Label: metadata.label, Description: metadata.description}
			for _, definition := range dstWorldSettingDefinitions {
				if definition.group != metadata.id || !dstWorldDefinitionApplies(definition, shardID) {
					continue
				}
				value, configured := definition.defaultValue, false
				if parsed.overrides != nil {
					if entry, ok := parsed.overrides.entries[definition.overrideKey]; ok {
						value, configured = entry.value.text, true
					}
				}
				options := append([]panel.SettingOption(nil), definition.options...)
				if configured && !dstWorldOptionAllowed(options, value) {
					options = append(options, panel.SettingOption{Label: "当前文件值：" + value, Value: value})
				}
				group.Settings = append(group.Settings, panel.Setting{
					Key: shardID + ".world." + definition.overrideKey, Label: definition.label,
					Description: definition.description, Type: "select", Value: value, Default: definition.defaultValue,
					Options: options, Configured: configured, RestartRequired: true, ApplyMode: "regenerate", Risk: "worldgen",
				})
			}
			if len(group.Settings) > 0 {
				shard.Groups = append(shard.Groups, group)
			}
		}
		result.Shards = append(result.Shards, shard)
	}
	return result, nil
}

func dstWorldDefinitionApplies(definition dstWorldSettingDefinition, shard string) bool {
	return definition.shards == "both" || definition.shards == shard
}

func dstWorldOptionAllowed(options []panel.SettingOption, value string) bool {
	for _, option := range options {
		if option.Value == value {
			return true
		}
	}
	return false
}

func dstConfigFileByID(document panel.DSTConfigDocument, id string) panel.DSTConfigFile {
	for _, file := range document.Files {
		if file.ID == id {
			return file
		}
	}
	return panel.DSTConfigFile{ID: id, Content: defaultDSTWorldgen(id)}
}

export type WorldOptionCategory = {
  id: string;
  label: string;
  description: string;
  keys: string[];
};

export const WORLD_OPTION_CATEGORIES: WorldOptionCategory[] = [
  {
    id: "server_access",
    label: "服务器与连接",
    description: "名称、人数、密码、跨平台与本机管理接口",
    keys: [
      "ServerName", "ServerDescription", "ServerPlayerMaxNum", "CoopPlayerMaxNum",
      "ServerPassword", "AdminPassword", "PublicIP", "PublicPort", "Region",
      "CrossplayPlatforms", "RESTAPIEnabled", "RESTAPIPort", "RCONEnabled", "RCONPort",
      "bUseAuth", "BanListURL", "LogFormatType", "ChatPostLimitPerMinute",
      "bShowPlayerList", "bIsShowJoinLeftMessage", "bAllowClientMod",
      "bEnableVoiceChat", "VoiceChatMaxVolumeDistance", "VoiceChatZeroVolumeDistance"
    ]
  },
  {
    id: "time_progression",
    label: "时间、成长与生成",
    description: "昼夜、经验、捕获、生成、随机化与自动保存",
    keys: [
      "Difficulty", "DayTimeSpeedRate", "NightTimeSpeedRate", "ExpRate",
      "PalCaptureRate", "PalSpawnNumRate", "RandomizerType", "RandomizerSeed",
      "bIsRandomizerPalLevelRandom", "AutoSaveSpan", "bIsUseBackupSaveData",
      "SupplyDropSpan"
    ]
  },
  {
    id: "player_pal",
    label: "玩家与帕鲁",
    description: "伤害、生存消耗、回复、能力成长与硬核规则",
    keys: [
      "PalDamageRateAttack", "PalDamageRateDefense", "PalStomachDecreaceRate",
      "PalStaminaDecreaceRate", "PalAutoHPRegeneRate", "PalAutoHpRegeneRateInSleep",
      "PlayerDamageRateAttack", "PlayerDamageRateDefense", "PlayerStomachDecreaceRate",
      "PlayerStaminaDecreaceRate", "PlayerAutoHPRegeneRate",
      "PlayerAutoHpRegeneRateInSleep", "bEnableAimAssistPad",
      "bEnableAimAssistKeyboard", "bHardcore", "bPalLost",
      "bCharacterRecreateInHardcore", "bAllowEnhanceStat_Health",
      "bAllowEnhanceStat_Attack", "bAllowEnhanceStat_Stamina",
      "bAllowEnhanceStat_Weight", "bAllowEnhanceStat_WorkSpeed"
    ]
  },
  {
    id: "combat_events",
    label: "战斗、事件与死亡",
    description: "PvP、友伤、袭击、猛兽、死亡和重生规则",
    keys: [
      "DeathPenalty", "bEnablePlayerToPlayerDamage", "bEnableFriendlyFire",
      "bEnableInvaderEnemy", "EnablePredatorBossPal", "bIsPvP", "bIsMultiplay",
      "BlockRespawnTime", "RespawnPenaltyDurationThreshold", "RespawnPenaltyTimeScale",
      "bDisplayPvPItemNumOnWorldMap_BaseCamp", "bDisplayPvPItemNumOnWorldMap_Player",
      "AdditionalDropItemWhenPlayerKillingInPvPMode",
      "AdditionalDropItemNumWhenPlayerKillingInPvPMode",
      "bAdditionalDropItemWhenPlayerKillingInPvPMode"
    ]
  },
  {
    id: "resources_items",
    label: "资源、物品与建造",
    description: "采集、掉落、孵化、工作、建筑耐久与物品规则",
    keys: [
      "BuildObjectHpRate", "BuildObjectDamageRate", "BuildObjectDeteriorationDamageRate",
      "CollectionDropRate", "CollectionObjectHpRate", "CollectionObjectRespawnSpeedRate",
      "EnemyDropItemRate", "ItemWeightRate", "PalEggDefaultHatchingTime", "WorkSpeedRate",
      "EquipmentDurabilityDamageRate", "ItemCorruptionMultiplier",
      "MonsterFarmActionSpeedRate", "DropItemAliveMaxHours", "bActiveUNKO"
    ]
  },
  {
    id: "base_guild",
    label: "据点与公会",
    description: "据点容量、工作帕鲁、公会清理、归属转移与建筑显示",
    keys: [
      "BaseCampMaxNum", "BaseCampMaxNumInGuild", "BaseCampWorkerMaxNum",
      "GuildPlayerMaxNum", "bAutoResetGuildNoOnlinePlayers",
      "AutoResetGuildTimeNoOnlinePlayers", "GuildRejoinCooldownMinutes",
      "bCanPickupOtherGuildDeathPenaltyDrop", "bEnableDefenseOtherGuildPlayer",
      "bInvisibleOtherGuildBaseCampAreaFX", "bBuildAreaLimit",
      "AutoTransferMasterCheckIntervalSeconds", "AutoTransferMasterThresholdDays",
      "MaxGuildsPerFrame", "bEnableBuildingPlayerUIdDisplay",
      "BuildingNameDisplayCacheTTLSeconds"
    ]
  },
  {
    id: "travel_features",
    label: "旅行与世界功能",
    description: "快速旅行、出生点、离线惩罚、跨界终端与科技限制",
    keys: [
      "bEnableNonLoginPenalty", "bEnableFastTravel", "bEnableFastTravelOnlyBaseCamp",
      "bIsStartLocationSelectByMap", "bExistPlayerAfterLogout",
      "bAllowGlobalPalboxExport", "bAllowGlobalPalboxImport", "DenyTechnologyList"
    ]
  },
  {
    id: "performance_limits",
    label: "性能与数量限制",
    description: "掉落物、建筑、同步距离和后台处理频率",
    keys: [
      "DropItemMaxNum", "DropItemMaxNum_UNKO", "MaxBuildingLimitNum",
      "ServerReplicatePawnCullDistance", "ItemContainerForceMarkDirtyInterval",
      "PhysicsActiveDropItemMaxNum", "PlayerDataPalStorageUpdateCheckTickInterval"
    ]
  }
];

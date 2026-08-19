<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import {
  Activity,
  AlertTriangle,
  ArrowLeft,
  Bell,
  Check,
  ChevronRight,
  CircleGauge,
  Clock3,
  CloudCog,
  Cpu,
  DatabaseBackup,
  Download,
  Flame,
  FolderSearch,
  Gamepad2,
  HardDrive,
  History,
  KeyRound,
  LayoutDashboard,
  LoaderCircle,
  LogOut,
  MemoryStick,
  Menu,
  MoreHorizontal,
  PackageOpen,
  PackageSearch,
  Play,
  RefreshCw,
  RotateCw,
  Save,
  Search,
  Server,
  Settings2,
  ShieldCheck,
  SlidersHorizontal,
  Square,
  TerminalSquare,
  Trash2,
  UserPlus,
  Users,
  X
} from "@lucide/vue";
import {
  api,
  isUnauthorized,
  type ConfigAuditChange,
  type ConfigAuditEntry,
  type DSTConfigDocument,
  type DSTSettings,
  type DSTWorldSettings,
  type Game,
  type IPRule,
  type LoginAuditEntry,
  type LogRef,
  type Logs,
  type Management,
  type ManagedGame,
  type MemberCredential,
  type ModInventory,
  type OperationAuditEntry,
  type Overview,
  type PalworldSettings,
  type PanelUpdateStatus,
  type Permission,
  type Session,
  type Setting
} from "./api";
import Sparkline from "./components/Sparkline.vue";
import {
  encodeWorldOption,
  parseWorldOption,
  verifyWorldOptionData,
  verifyWorldOptionRoundTrip,
  worldOptionSettings,
  type ParsedWorldOption
} from "./worldOptionCodec";

type Page = "overview" | "game" | "mods" | "settings" | "logs" | "access" | "system";
type ActionName = "start" | "stop" | "restart" | "update" | "backup";
type SettingsSource = "world" | "ini";
const minimumPasswordLength = 10;
const panelUpdateWaitTimeoutMs = 15 * 60_000;
const panelHealthRequestTimeoutMs = 5_000;
const panelUpdateResumeKey = "hearth.resume-panel-update";

function passwordCharacterCount(password: string) {
  return Array.from(password).length;
}

const permissionDefinitions: Array<{
  id: Permission;
  label: string;
  description: string;
}> = [
  { id: "game.control", label: "控制运行状态", description: "启动、停止和重启游戏服务器" },
  { id: "game.update", label: "更新服务端", description: "执行 SteamCMD 更新流程" },
  { id: "game.backup", label: "创建存档备份", description: "手动创建游戏存档备份" },
  {
    id: "palworld.settings.gameplay",
    label: "修改帕鲁玩法参数",
    description: "仅可查看和保存开放的日常玩法参数；系统与安全设置仍由管理员管理"
  }
];

const permissionPresets: Array<{
  id: string;
  label: string;
  permissions: Permission[];
}> = [
  { id: "readonly", label: "只读", permissions: [] },
  { id: "daily", label: "日常管理", permissions: ["game.control", "game.backup"] },
  { id: "owner", label: "服主管理", permissions: permissionDefinitions.map((item) => item.id) }
];

const booting = ref(true);
const loggedIn = ref(false);
const loginPassword = ref("");
const loginError = ref("");
const loggingIn = ref(false);
const overview = ref<Overview | null>(null);
const page = ref<Page>("overview");
const selectedGameId = ref("palworld");
const settingsGameId = ref<"palworld" | "dont-starve-together">("palworld");
const mobileNavOpen = ref(false);
const loading = ref(false);
const loadError = ref("");
const confirmAction = ref<{ game: Game; action: ActionName } | null>(null);
const actionBusy = ref(false);
const versionCheckSubmitting = ref(false);
const toast = ref<{ type: "success" | "error"; message: string } | null>(null);
const worldSettings = ref<PalworldSettings | null>(null);
const iniSettings = ref<PalworldSettings | null>(null);
const settingsSource = ref<SettingsSource>("world");
const settings = computed(() => settingsSource.value === "world" ? worldSettings.value : iniSettings.value);
const worldDirtyKeys = ref<Set<string>>(new Set());
const iniDirtyKeys = ref<Set<string>>(new Set());
const settingsDirty = computed(() =>
  (settingsSource.value === "world" ? worldDirtyKeys.value : iniDirtyKeys.value).size > 0
);
const settingsLoading = ref(false);
const settingsSaving = ref(false);
const settingsSearch = ref("");
const settingsGroup = ref("server_access");
const parsedWorldOption = ref<ParsedWorldOption | null>(null);
const worldRoundTripError = ref("");
const worldSourceError = ref("");
const iniSourceError = ref("");
const dstConfig = ref<DSTConfigDocument | null>(null);
const dstSettings = ref<DSTSettings | null>(null);
const dstWorldSettings = ref<DSTWorldSettings | null>(null);
const dstSettingsMode = ref<"server" | "world" | "advanced">("server");
const dstSettingsGroup = ref("server");
const dstSettingsDirtyKeys = ref<Set<string>>(new Set());
const dstSettingsDirty = computed(() => dstSettingsDirtyKeys.value.size > 0);
const dstWorldShard = ref<"master" | "caves">("master");
const dstWorldGroup = ref("generation");
const dstWorldDirtyKeys = ref<Set<string>>(new Set());
const dstWorldDirty = computed(() => dstWorldDirtyKeys.value.size > 0);
const dstWorldSourceError = ref("");
const activeDSTWorldShard = computed(() => dstWorldSettings.value?.shards.find((shard) => shard.id === dstWorldShard.value) ?? null);
const dstConfigActiveFile = ref<DSTConfigDocument["files"][number]["id"]>("cluster");
const dstConfigDrafts = ref<Record<string, string>>({});
const dstConfigDirtyFiles = ref<Set<string>>(new Set());
const dstConfigDirty = computed(() => dstConfigDirtyFiles.value.size > 0);
const dstConfigLoading = ref(false);
const dstConfigSaving = ref(false);
const modInventory = ref<ModInventory | null>(null);
const modsLoading = ref(false);
const modsError = ref("");
const dstActiveDirty = computed(() => {
  if (dstSettingsMode.value === "advanced") return dstConfigDirty.value;
  if (dstSettingsMode.value === "world") return dstWorldDirty.value;
  return dstSettingsDirty.value;
});
const logs = ref<Logs | null>(null);
const logsLoading = ref(false);
const selectedLogId = ref("");
const nodeMenuOpen = ref(false);
const gameMenuOpen = ref(false);
const buildVersion = ref("未知");
const currentRole = ref<"admin" | "member" | "">("");
const credentialId = ref("");
const currentPermissions = ref<Permission[]>([]);
const members = ref<MemberCredential[]>([]);
const auditEntries = ref<LoginAuditEntry[]>([]);
const operationAuditEntries = ref<OperationAuditEntry[]>([]);
const configAuditEntries = ref<ConfigAuditEntry[]>([]);
const ipRules = ref<IPRule[]>([]);
const accessLoading = ref(false);
const accessTab = ref<"members" | "audit" | "operation-audit" | "config-audit" | "ip-rules">("members");
const auditMenuEntryId = ref("");
const newRuleIP = ref("");
const newRuleKind = ref<IPRule["kind"]>("deny");
const newRuleHours = ref(24);
const newRulePermanent = ref(false);
const newRuleNote = ref("");
const savingIPRule = ref(false);
const deletingIPRuleId = ref("");
const newMemberPassword = ref("");
const newMemberPermissions = ref<Permission[]>([]);
const editingMemberId = ref("");
const editingMemberPassword = ref("");
const editingMemberPermissions = ref<Permission[]>([]);
const deletingMemberId = ref("");
const management = ref<Management | null>(null);
const managementLoading = ref(false);
const managementSaving = ref(false);
const dstTokenInput = ref("");
const systemTab = ref<"games" | "settings" | "updates">("games");
const panelUpdate = ref<PanelUpdateStatus | null>(null);
const panelUpdateBusy = ref(false);
const installSteamCmdRoot = ref("");
const installConsent = ref(false);
const dstInstallClusterDir = ref("");
const dstInstallClusterName = ref("Hearth DST Server");
const dstInstallClusterToken = ref("");
const discoveryRootsText = ref("");
const trustedProxyCIDRsText = ref("");
const currentTime = ref(new Date());
let pollTimer: number | undefined;
let logPollTimer: number | undefined;
let toastTimer: number | undefined;
let clockTimer: number | undefined;
let refreshInFlight = false;
let logRefreshInFlight = false;
let dismissedLogIds = new Set<string>();
let panelUpdateTrackingEpoch = 0;
let sessionRecoveryReloading = false;

const selectedGame = computed(() =>
  overview.value?.games.find((game) => game.id === selectedGameId.value)
);

const selectedGameActivity = computed(() =>
  (overview.value?.activities ?? []).find(
    (item) =>
      item.status === "running" &&
      (!item.gameId || item.gameId === selectedGameId.value)
  )
);

const runningCount = computed(
  () => overview.value?.games.filter((game) => game.state === "running").length ?? 0
);

const palworldGame = computed(() =>
  overview.value?.games.find((game) => game.id === "palworld")
);
const dstGame = computed(() =>
  overview.value?.games.find((game) => game.id === "dont-starve-together")
);
const steamCmdPalworldPath = computed(() => {
  const root = installSteamCmdRoot.value.trim().replace(/[\\/]+$/, "");
  return root ? `${root}\\steamapps\\common\\PalServer` : "SteamCMD\\steamapps\\common\\PalServer";
});
const steamCmdDSTPath = computed(() => {
  const root = installSteamCmdRoot.value.trim().replace(/[\\/]+$/, "");
  return root
    ? `${root}\\steamapps\\common\\Don't Starve Together Dedicated Server`
    : "SteamCMD\\steamapps\\common\\Don't Starve Together Dedicated Server";
});
const activityByID = computed(
  () => new Map((overview.value?.activities ?? []).map((item) => [item.id, item]))
);

function managedGameActivity(game: ManagedGame) {
  return game.activeTaskId ? activityByID.value.get(game.activeTaskId) : undefined;
}

const hasManagedGames = computed(() => (overview.value?.games.length ?? 0) > 0);
const palworldManagement = computed(() =>
  management.value?.games.find((game) => game.id === "palworld")
);

const restAPIEnabled = computed(() => {
  for (const group of iniSettings.value?.groups ?? []) {
    const setting = group.settings.find((item) => item.key === "RESTAPIEnabled");
    if (setting) return setting.value === true || setting.value === "True";
  }
  return null;
});

const playerSummary = computed(() => {
  const games = overview.value?.games ?? [];
  if (games.length === 0) {
    return { online: null as number | null, max: null, source: "尚未接入游戏" };
  }
  const max = games.reduce((sum, game) => sum + game.playersMax, 0);
  const maxKnown = games.every((game) => game.playersMaxKnown);
  const unavailable = games.some(
    (game) => game.state === "running" && !game.playersAvailable
  );
  if (unavailable) {
    return { online: null as number | null, max: maxKnown ? max : null, source: "REST API 暂不可用" };
  }
  const online = games.reduce((sum, game) => sum + game.playersOnline, 0);
  const demo = games.some((game) => game.playersSource === "演示数据");
  return {
    online,
    max: maxKnown ? max : null,
    source: demo ? "演示数据" : "REST API 实时数据"
  };
});

const todayLabel = computed(() =>
  new Intl.DateTimeFormat("zh-CN", {
    month: "long",
    day: "numeric",
    weekday: "long"
  }).format(currentTime.value)
);

const greetingLabel = computed(() => {
  const hour = currentTime.value.getHours();
  if (hour < 6) return "晚上好";
  if (hour < 9) return "早上好";
  if (hour < 12) return "上午好";
  if (hour < 18) return "下午好";
  return "晚上好";
});

const nodeHealthScore = computed(() => {
  if (!overview.value) return 0;
  let score = 100;
  if (overview.value.host.memoryPercent >= 90) score -= 25;
  else if (overview.value.host.memoryPercent >= 80) score -= 10;
  if (overview.value.host.diskPercent >= 90) score -= 25;
  else if (overview.value.host.diskPercent >= 80) score -= 10;
  if (overview.value.host.cpuPercent >= 90) score -= 20;
  if (overview.value.games.some((game) => game.state === "error")) score -= 30;
  return Math.max(0, score);
});

const nodeHealthLabel = computed(() => {
  if (nodeHealthScore.value >= 90) return "运行状态良好";
  if (nodeHealthScore.value >= 70) return "需要留意";
  return "需要检查";
});

const filteredGroups = computed(() => {
  if (!settings.value) return [];
  const query = settingsSearch.value.trim().toLowerCase();
  if (!query) {
    return settings.value.groups.filter((group) => group.id === settingsGroup.value);
  }
  return settings.value.groups
    .map((group) => ({
      ...group,
      settings: group.settings.filter(
        (item) =>
          item.label.toLowerCase().includes(query) ||
          item.key.toLowerCase().includes(query) ||
          item.description.toLowerCase().includes(query)
      )
    }))
    .filter((group) => group.settings.length > 0);
});

const pageTitle = computed(() => {
  if (page.value === "system") return "系统设置与游戏管理";
  if (page.value === "mods") return "帕鲁模组管理";
  if (page.value === "settings") return settingsGameId.value === "dont-starve-together" ? "饥荒联机版配置" : "帕鲁服务器配置";
  if (page.value === "logs") return "任务日志";
  if (page.value === "access") return "访问权限";
  if (page.value === "game") return selectedGame.value?.name ?? "游戏详情";
  return "服务器总览";
});

const selectedLog = computed(() =>
  logs.value?.files.find((file) => file.id === selectedLogId.value)
);

function updateTaskRunning(gameId: string) {
  return (overview.value?.activities ?? []).some(
    (item) => item.status === "running" && item.action === "update" && (!item.gameId || item.gameId === gameId)
  );
}

const isAdmin = computed(() => currentRole.value === "admin");
const canManageSettings = computed(() => hasPermission("palworld.settings.gameplay"));
const canOpenPalworldSettings = computed(() => canManageSettings.value && !!palworldGame.value);
const canOpenDSTSettings = computed(() => isAdmin.value && !!dstGame.value);
const canOpenPalworldMods = computed(() => isAdmin.value && !!palworldGame.value);
const enabledModCount = computed(() => modInventory.value?.mods.filter((mod) => mod.enabled).length ?? 0);

onMounted(async () => {
  clockTimer = window.setInterval(() => {
    currentTime.value = new Date();
  }, 60_000);
  try {
    const session = await api.session();
    buildVersion.value = session.version || "未知";
    if (session.authenticated) {
      // This resumes a session that already passed password authentication.
      // The separate known-device cookie can never make this response authenticated.
      await enterAuthenticatedSession(session);
    }
  } catch (error) {
    if (!isUnauthorized(error)) {
      loginError.value = error instanceof Error ? error.message : "连接面板失败";
    }
  } finally {
    booting.value = false;
  }
});

onBeforeUnmount(() => {
  if (pollTimer) window.clearTimeout(pollTimer);
  if (logPollTimer) window.clearTimeout(logPollTimer);
  if (toastTimer) window.clearTimeout(toastTimer);
  if (clockTimer) window.clearInterval(clockTimer);
});

function stopOverviewPolling() {
  if (pollTimer) window.clearTimeout(pollTimer);
  pollTimer = undefined;
}

function stopLogPolling() {
  if (logPollTimer) window.clearTimeout(logPollTimer);
  logPollTimer = undefined;
}

function clearAuthenticatedState() {
  loggedIn.value = false;
  overview.value = null;
  currentRole.value = "";
  credentialId.value = "";
  currentPermissions.value = [];
  members.value = [];
  auditEntries.value = [];
  operationAuditEntries.value = [];
  configAuditEntries.value = [];
  ipRules.value = [];
  management.value = null;
  modInventory.value = null;
  modsError.value = "";
  panelUpdate.value = null;
  panelUpdateBusy.value = false;
  panelUpdateTrackingEpoch += 1;
  stopOverviewPolling();
  stopLogPolling();
}

function rememberPanelUpdateResume(target?: string) {
  try {
    const safeTarget = target && /^[0-9A-Za-z.-]{1,64}$/.test(target) ? target : "";
    window.sessionStorage.setItem(panelUpdateResumeKey, safeTarget);
  } catch {
    // A blocked sessionStorage must not prevent a safe reload.
  }
}

function consumePanelUpdateResume() {
  try {
    const stored = window.sessionStorage.getItem(panelUpdateResumeKey);
    window.sessionStorage.removeItem(panelUpdateResumeKey);
    return { requested: stored !== null, target: stored && /^[0-9A-Za-z.-]{1,64}$/.test(stored) ? stored : "" };
  } catch {
    return { requested: false, target: "" };
  }
}

function handleSessionExpired() {
  const resumePanelUpdate = panelUpdateBusy.value ||
    panelUpdate.value?.state === "checking" || panelUpdate.value?.state === "preparing";
  if (resumePanelUpdate) rememberPanelUpdateResume(panelUpdate.value?.latestVersion);
  clearAuthenticatedState();
  if (resumePanelUpdate && !sessionRecoveryReloading) {
    sessionRecoveryReloading = true;
    window.location.reload();
  }
}

async function enterAuthenticatedSession(session: Session) {
  if (!session.authenticated || !session.role) return;
  buildVersion.value = session.version || buildVersion.value;
  currentRole.value = session.role;
  credentialId.value = session.credentialId ?? "";
  currentPermissions.value = session.permissions ?? [];
  loggedIn.value = true;
  sessionRecoveryReloading = false;
  await refresh();
  if (!loggedIn.value) return;

  const resumePanelUpdate = consumePanelUpdateResume();
  if (session.role === "admin") {
    await loadManagement();
    if (page.value === "mods") await loadModInventory();
    if (resumePanelUpdate.requested) {
      page.value = "system";
      systemTab.value = "updates";
    }
    if (page.value === "system" && systemTab.value === "updates") await refreshPanelUpdateOnEntry();
    if (resumePanelUpdate.target &&
      !(panelUpdate.value?.state === "succeeded" && panelUpdate.value.currentVersion === resumePanelUpdate.target) &&
      panelUpdate.value?.state !== "failed" && panelUpdate.value?.state !== "rolled_back") {
      resumePanelUpdateWait(resumePanelUpdate.target, false);
    }
  } else if (page.value === "mods") {
    page.value = "overview";
  }
  startPolling();
}

async function login() {
  loginError.value = "";
  loggingIn.value = true;
  try {
    const session = await api.login(loginPassword.value);
    loginPassword.value = "";
    await enterAuthenticatedSession(session);
  } catch (error) {
    loginError.value = error instanceof Error ? error.message : "登录失败";
  } finally {
    loggingIn.value = false;
  }
}

async function logout() {
  await api.logout().catch(() => undefined);
  clearAuthenticatedState();
  page.value = "overview";
}

function startPolling() {
  schedulePolling();
}

function schedulePolling(delay?: number) {
  if (pollTimer) window.clearTimeout(pollTimer);
  pollTimer = undefined;
  if (!loggedIn.value) return;
  const activeTask =
    (overview.value?.activities ?? []).some((item) => item.status === "running") ||
    (overview.value?.games ?? []).some(
      (game) => game.state === "starting" || game.state === "stopping"
    );
  pollTimer = window.setTimeout(async () => {
    await refresh(true);
    schedulePolling();
  }, delay ?? (activeTask ? 1000 : 5000));
}

async function refresh(silent = false) {
  if (refreshInFlight) return;
  refreshInFlight = true;
  if (!silent) loading.value = true;
  try {
    const nextOverview = await api.overview();
    overview.value = {
      ...nextOverview,
      games: nextOverview.games ?? [],
      activities: nextOverview.activities ?? []
    };
    if (isAdmin.value && (page.value === "system" || nextOverview.games.length === 0)) {
      void loadManagement(true);
    }
    if (logs.value) {
      logs.value = { ...logs.value, activities: nextOverview.activities ?? [] };
      syncRunningTaskLogs(nextOverview.activities ?? []);
    }
    loadError.value = "";
  } catch (error) {
    if (isUnauthorized(error)) {
      handleSessionExpired();
    } else if (!silent) {
      loadError.value = error instanceof Error ? error.message : "无法加载服务器状态";
    }
  } finally {
    refreshInFlight = false;
    loading.value = false;
  }
}

function navigate(next: Page, gameId?: string) {
  if (next === "settings") {
    const target = gameId === "dont-starve-together" ? "dont-starve-together" : "palworld";
    if (target === "dont-starve-together" && !canOpenDSTSettings.value) {
      showToast("error", "饥荒联机版配置仅对已托管的管理员开放");
      return;
    }
    if (target === "palworld" && !canOpenPalworldSettings.value) {
      showToast("error", "帕鲁配置仅对已托管且有权限的服务器开放");
      return;
    }
    settingsGameId.value = target;
  }
  if (next === "mods" && !canOpenPalworldMods.value) {
    showToast("error", "帕鲁模组管理仅对已托管的管理员开放");
    return;
  }
  if (!isAdmin.value && (next === "mods" || next === "logs" || next === "access" || next === "system")) {
    showToast("error", "仅管理员可以访问该页面");
    return;
  }
  const enteringLogs = next === "logs" && page.value !== "logs";
  if (gameId) selectedGameId.value = gameId;
  page.value = next;
  if (next !== "logs") stopLogPolling();
  mobileNavOpen.value = false;
  nodeMenuOpen.value = false;
  gameMenuOpen.value = false;
  window.scrollTo({ top: 0, behavior: "smooth" });
  if (next === "settings") {
    if (settingsGameId.value === "dont-starve-together") void loadDSTConfig();
    else if (!settings.value) void loadSettings();
  }
  if (next === "mods") void loadModInventory();
  if (next === "logs") void loadLogs(enteringLogs);
  if (next === "access") void loadAccess();
  if (next === "system") {
    void loadManagement();
    void refreshPanelUpdateOnEntry();
  }
}

async function loadModInventory() {
  if (!canOpenPalworldMods.value || modsLoading.value) return;
  modsLoading.value = true;
  modsError.value = "";
  try {
    const inventory = await api.modInventory("palworld");
    modInventory.value = {
      ...inventory,
      mods: (inventory.mods ?? []).map((mod) => ({
        ...mod,
        dependencies: mod.dependencies ?? [],
        warnings: mod.warnings ?? []
      })),
      warnings: inventory.warnings ?? []
    };
  } catch (error) {
    if (isUnauthorized(error)) {
      handleSessionExpired();
      return;
    }
    modsError.value = error instanceof Error ? error.message : "模组清单读取失败";
  } finally {
    modsLoading.value = false;
  }
}

async function loadPanelUpdate(silent = false): Promise<boolean> {
  if (!isAdmin.value) return false;
  const controller = new AbortController();
  const requestTimeout = window.setTimeout(() => controller.abort(), panelHealthRequestTimeoutMs);
  try {
    const status = await api.panelUpdate(controller.signal);
    panelUpdate.value = status;
    if (status.state === "preparing" && status.latestVersion && !panelUpdateBusy.value) {
      // Re-entry already loads the current frontend bundle; a terminal result
      // should update this page in place instead of forcing another login flow.
      resumePanelUpdateWait(status.latestVersion, false);
    }
    return true;
  } catch (error) {
    if (isUnauthorized(error)) {
      handleSessionExpired();
      return false;
    }
    if (!silent) showToast("error", error instanceof Error ? error.message : "面板更新状态读取失败");
    return false;
  } finally {
    window.clearTimeout(requestTimeout);
  }
}

async function refreshPanelUpdateOnEntry() {
  if (!isAdmin.value) return;
  if (!panelUpdateBusy.value && (panelUpdate.value?.state === "checking" || panelUpdate.value?.state === "preparing")) {
    panelUpdate.value = null;
  }
  for (let attempt = 0; attempt < 3; attempt += 1) {
    if (await loadPanelUpdate(true)) return;
    if (!isAdmin.value) return;
    if (attempt < 2) await new Promise((resolve) => window.setTimeout(resolve, 1000));
  }
  showToast("error", "面板刚刚重启，更新状态暂时无法读取，请稍后重试");
}

function resumePanelUpdateWait(target: string, reloadOnSuccess = true) {
  if (panelUpdateBusy.value) return;
  panelUpdateBusy.value = true;
  const trackingEpoch = ++panelUpdateTrackingEpoch;
  void waitForPanelUpdate(target, trackingEpoch, reloadOnSuccess)
    .catch(async (error) => {
      await loadPanelUpdate(true);
      showToast("error", error instanceof Error ? error.message : "面板更新状态跟踪失败");
    })
    .finally(() => {
      if (trackingEpoch === panelUpdateTrackingEpoch) panelUpdateBusy.value = false;
    });
}

async function checkPanelUpdate() {
  panelUpdateBusy.value = true;
  try {
    panelUpdate.value = await api.checkPanelUpdate();
    showToast("success", panelUpdate.value.updateAvailable ? `发现 Hearth ${panelUpdate.value.latestVersion}` : "当前已是所选通道的最新版本");
  } catch (error) {
    await loadPanelUpdate(true);
    showToast("error", error instanceof Error ? error.message : "面板版本检查失败");
  } finally {
    panelUpdateBusy.value = false;
  }
}

async function applyPanelUpdate() {
  const target = panelUpdate.value?.latestVersion;
  if (!target || !panelUpdate.value?.updateAvailable) return;
  if (!window.confirm(`确认将 Hearth 更新到 ${target}？面板会短暂离线；游戏服务器和存档不会停止或修改。`)) return;
  panelUpdateBusy.value = true;
  const trackingEpoch = ++panelUpdateTrackingEpoch;
  try {
    panelUpdate.value = await api.applyPanelUpdate(target);
    showToast("success", "更新包正在校验；完成后面板会自动重启并执行健康检查");
    await waitForPanelUpdate(target, trackingEpoch);
  } catch (error) {
    await loadPanelUpdate(true);
    showToast("error", error instanceof Error ? error.message : "面板更新启动失败");
  } finally {
    if (trackingEpoch === panelUpdateTrackingEpoch) panelUpdateBusy.value = false;
  }
}

async function waitForPanelUpdate(target: string, trackingEpoch: number, reloadOnSuccess = true) {
  const deadline = Date.now() + panelUpdateWaitTimeoutMs;
  while (Date.now() < deadline) {
    if (trackingEpoch !== panelUpdateTrackingEpoch) return;
    await new Promise((resolve) => window.setTimeout(resolve, 1000));
    const statusLoaded = await loadPanelUpdate(true);
    if (trackingEpoch !== panelUpdateTrackingEpoch) return;
    const status = panelUpdate.value;
    if (statusLoaded && status?.state === "failed") {
      throw new Error(`更新失败：${status.message || status.stage}`);
    }
    if (statusLoaded && status?.state === "rolled_back") {
      throw new Error(status.message || "新版本未生效，面板已恢复上一版本");
    }
    if (statusLoaded && status?.state === "succeeded" && status.currentVersion === target) {
      showToast("success", `Hearth ${target} 更新完成`);
      if (reloadOnSuccess) {
        await new Promise((resolve) => window.setTimeout(resolve, 300));
        window.location.reload();
      }
      return;
    }

    const controller = new AbortController();
    const requestTimeout = window.setTimeout(() => controller.abort(), panelHealthRequestTimeoutMs);
    let health: { status?: string; version?: string } | undefined;
    try {
      const response = await fetch("/api/v1/health", { cache: "no-store", signal: controller.signal });
      if (!response.ok) continue;
      health = await response.json() as { status?: string; version?: string };
    } catch {
      continue;
    } finally {
      window.clearTimeout(requestTimeout);
    }
    if (health.status === "ok" && health.version === target) {
      if (statusLoaded && status) {
        panelUpdate.value = {
          ...status,
          latestVersion: target,
          state: "preparing",
          stage: "等待更新结果",
          progress: Math.max(status.progress, 99),
          message: "新版本已经启动，正在等待独立更新器确认健康检查结果"
        };
      }
      continue;
    }
  }
  await loadPanelUpdate(true);
  throw new Error(panelUpdate.value?.message || "等待更新完成超过 15 分钟，请查看面板更新状态和 panel-update.log");
}

function syncManagementInputs(document: Management) {
  const settings = document.settings;
  installSteamCmdRoot.value = installSteamCmdRoot.value || settings.steamCmdRoot;
  discoveryRootsText.value = (settings.discoveryRoots ?? []).join("\n");
  trustedProxyCIDRsText.value = (settings.trustedProxyCidrs ?? []).join("\n");
  const dst = document.games.find((game) => game.id === "dont-starve-together");
  if (!dstInstallClusterDir.value && dst?.suggestedClusterDir) {
    dstInstallClusterDir.value = dst.suggestedClusterDir;
  }
}

async function loadManagement(silent = false) {
  if (!isAdmin.value || managementLoading.value) return;
  managementLoading.value = true;
  try {
    const document = await api.management();
    management.value = document;
    syncManagementInputs(document);
  } catch (error) {
    if (!silent) showToast("error", error instanceof Error ? error.message : "游戏管理数据加载失败");
  } finally {
    managementLoading.value = false;
  }
}

async function refreshDiscovery() {
  managementLoading.value = true;
  try {
    const document = await api.refreshDiscovery();
    management.value = document;
    syncManagementInputs(document);
    showToast("success", "只读探测已完成，没有修改游戏文件");
  } catch (error) {
    showToast("error", error instanceof Error ? error.message : "探测失败");
  } finally {
    managementLoading.value = false;
  }
}

async function adoptManagedGame(game: ManagedGame, candidateId: string) {
  managementSaving.value = true;
  try {
    await api.adoptGame(game.id, candidateId);
    showToast("success", `现有${game.name}已接管；没有修改存档或游戏配置`);
    await Promise.all([refresh(true), loadManagement(true)]);
  } catch (error) {
    showToast("error", error instanceof Error ? error.message : "接管失败");
  } finally {
    managementSaving.value = false;
  }
}

async function installManagedGame(game: ManagedGame) {
  if (!installConsent.value) {
    showToast("error", "请先确认安装会联网下载并写入 SteamCMD 目录");
    return;
  }
  if (game.id === "dont-starve-together" && (!dstInstallClusterDir.value.trim() || !dstInstallClusterName.value.trim())) {
    showToast("error", "请填写全新的 DST 集群目录和服务器名称");
    return;
  }
  managementSaving.value = true;
  try {
    await api.installGame(game.id, {
      steamCmdRoot: installSteamCmdRoot.value,
      ...(game.id === "dont-starve-together"
        ? {
            dst: {
              clusterDir: dstInstallClusterDir.value,
              clusterName: dstInstallClusterName.value,
              clusterToken: dstInstallClusterToken.value || undefined
            }
          }
        : {})
    });
    showToast("success", "安装任务已开始；完成后不会自动启动游戏");
    installConsent.value = false;
    dstInstallClusterToken.value = "";
    await Promise.all([refresh(true), loadManagement(true)]);
    schedulePolling(1000);
  } catch (error) {
    showToast("error", error instanceof Error ? error.message : "安装任务提交失败");
  } finally {
    managementSaving.value = false;
  }
}

async function saveDSTToken(game: ManagedGame) {
  const token = dstTokenInput.value.trim();
  if (game.id !== "dont-starve-together" || !token) {
    showToast("error", "请输入 DST Cluster Token");
    return;
  }
  managementSaving.value = true;
  try {
    await api.updateDSTToken(token);
    showToast("success", "DST Cluster Token 已更新；可启动 Master/Caves");
    await Promise.all([refresh(true), loadManagement(true)]);
  } catch (error) {
    showToast("error", error instanceof Error ? error.message : "DST Token 更新失败");
  } finally {
    dstTokenInput.value = "";
    managementSaving.value = false;
  }
}

async function saveSystemSettings() {
  if (!management.value) return;
  managementSaving.value = true;
  try {
    const updated = await api.updateSystemSettings({
      ...management.value.settings,
      discoveryRoots: discoveryRootsText.value.split(/\r?\n/).map((item) => item.trim()).filter(Boolean),
      trustedProxyCidrs: trustedProxyCIDRsText.value.split(/\r?\n/).map((item) => item.trim()).filter(Boolean)
    });
    management.value = { ...management.value, settings: updated };
    syncManagementInputs(management.value);
    showToast("success", updated.restartRequired ? "设置已保存，相关运行参数将在重启 Hearth 后生效" : "设置已保存");
  } catch (error) {
    showToast("error", error instanceof Error ? error.message : "后台设置保存失败");
  } finally {
    managementSaving.value = false;
  }
}

async function loadLogs(resetTabs = false) {
  logsLoading.value = true;
  try {
    const metadata = await api.logs();
    if (resetTabs || !logs.value) {
      dismissedLogIds = new Set<string>();
      logs.value = metadata;
      selectedLogId.value = metadata.files.find((file) => file.id === "panel")?.id ?? "";
    } else {
      const existing = new Map(logs.value.files.map((file) => [file.id, file]));
      for (const file of metadata.files) {
        existing.set(file.id, { ...existing.get(file.id), ...file });
      }
      logs.value = { activities: metadata.activities, files: [...existing.values()] };
    }
    syncRunningTaskLogs(metadata.activities);
    if (!selectedLogId.value || !logs.value.files.some((file) => file.id === selectedLogId.value)) {
      selectedLogId.value = logs.value.files[0]?.id ?? "";
    }
    if (selectedLogId.value) await loadLogContent(selectedLogId.value, true);
  } catch (error) {
    showToast("error", error instanceof Error ? error.message : "日志加载失败");
  } finally {
    logsLoading.value = false;
    scheduleLogPolling();
  }
}

function scheduleLogPolling(delay = 750) {
  stopLogPolling();
  if (!loggedIn.value || !isAdmin.value || page.value !== "logs" || !selectedLogId.value) return;
  logPollTimer = window.setTimeout(async () => {
    if (!logRefreshInFlight) {
      logRefreshInFlight = true;
      try {
        await loadLogContent(selectedLogId.value, true);
      } finally {
        logRefreshInFlight = false;
      }
    }
    scheduleLogPolling();
  }, delay);
}

function addLogTabs(refs: LogRef[], updatedAt?: string) {
  if (!logs.value) return;
  const files = [...logs.value.files];
  const known = new Set(files.map((file) => file.id));
  for (const ref of refs) {
    if (known.has(ref.id) || dismissedLogIds.has(ref.id)) continue;
    files.push({ id: ref.id, label: ref.label, updatedAt: updatedAt ?? new Date().toISOString(), truncated: false });
    known.add(ref.id);
  }
  logs.value = { ...logs.value, files };
}

function syncRunningTaskLogs(activities: Logs["activities"]) {
  for (const activity of activities) {
    if (activity.status === "running" && activity.logs?.length) {
      addLogTabs(activity.logs, activity.updatedAt ?? activity.createdAt);
    }
  }
}

async function loadLogContent(id: string, silent = false) {
  if (!logs.value || !id) return;
  try {
    const file = await api.log(id);
    if (!logs.value.files.some((item) => item.id === id)) return;
    logs.value = {
      ...logs.value,
      files: logs.value.files.map((item) => item.id === id ? file : item)
    };
  } catch (error) {
    if (!silent) showToast("error", error instanceof Error ? error.message : "日志读取失败");
  }
}

function selectLog(id: string) {
  selectedLogId.value = id;
  void loadLogContent(id);
  scheduleLogPolling();
}

async function openActivityLogs(item: Logs["activities"][number]) {
  const refs = item.logs ?? [];
  if (!refs.length) return;
  if (page.value !== "logs") {
    page.value = "logs";
    mobileNavOpen.value = false;
    nodeMenuOpen.value = false;
    gameMenuOpen.value = false;
    window.scrollTo({ top: 0, behavior: "smooth" });
    await loadLogs(true);
  }
  for (const ref of refs) dismissedLogIds.delete(ref.id);
  addLogTabs(refs, item.updatedAt ?? item.createdAt);
  selectLog(refs[0].id);
}

function closeLog(id: string) {
  if (!logs.value || id === "panel") return;
  const index = logs.value.files.findIndex((file) => file.id === id);
  if (index < 0) return;
  dismissedLogIds.add(id);
  const files = logs.value.files.filter((file) => file.id !== id);
  logs.value = { ...logs.value, files };
  if (selectedLogId.value === id) {
    selectedLogId.value = files[Math.max(0, index - 1)]?.id ?? files[0]?.id ?? "";
    if (selectedLogId.value) void loadLogContent(selectedLogId.value, true);
  }
  scheduleLogPolling();
}

async function loadAccess() {
  if (!isAdmin.value) return;
  accessLoading.value = true;
  try {
    const [memberResult, auditResult, operationAuditResult, configAuditResult, ipRuleResult] = await Promise.all([
      api.members(),
      api.loginAudit(),
      api.operationAudit(),
      api.configAudit(),
      api.ipRules()
    ]);
    members.value = memberResult.members ?? [];
    auditEntries.value = auditResult.entries ?? [];
    operationAuditEntries.value = operationAuditResult.entries ?? [];
    configAuditEntries.value = configAuditResult.entries ?? [];
    ipRules.value = ipRuleResult.rules ?? [];
  } catch (error) {
    showToast("error", error instanceof Error ? error.message : "权限数据加载失败");
  } finally {
    accessLoading.value = false;
  }
}

async function refreshOperationAuditAfterMutation() {
  try {
    operationAuditEntries.value = (await api.operationAudit()).entries ?? [];
    return true;
  } catch {
    return false;
  }
}

async function addMember() {
  if (passwordCharacterCount(newMemberPassword.value) < minimumPasswordLength) {
    showToast("error", `成员密码至少需要 ${minimumPasswordLength} 个字符`);
    return;
  }
  try {
    const member = await api.createMember(
      newMemberPassword.value,
      newMemberPermissions.value
    );
    newMemberPassword.value = "";
    newMemberPermissions.value = [];
    members.value = [member, ...members.value];
    const auditRefreshed = await refreshOperationAuditAfterMutation();
    showToast("success", `成员密码 ${member.id} 已添加${auditRefreshed ? "" : "；刷新页面可查看审计记录"}`);
  } catch (error) {
    showToast("error", error instanceof Error ? error.message : "成员密码添加失败");
  }
}

function beginMemberEdit(member: MemberCredential) {
  editingMemberId.value = member.id;
  editingMemberPassword.value = "";
  editingMemberPermissions.value = [...(member.permissions ?? [])];
  deletingMemberId.value = "";
}

async function saveMember(member: MemberCredential) {
  if (
    editingMemberPassword.value.length > 0 &&
    passwordCharacterCount(editingMemberPassword.value) < minimumPasswordLength
  ) {
    showToast("error", `新密码留空表示不修改；填写时至少需要 ${minimumPasswordLength} 个字符`);
    return;
  }
  try {
    const updated = await api.updateMember(member.id, {
      ...(editingMemberPassword.value ? { password: editingMemberPassword.value } : {}),
      permissions: editingMemberPermissions.value
    });
    members.value = members.value.map((item) => item.id === member.id ? updated : item);
    editingMemberId.value = "";
    editingMemberPassword.value = "";
    const auditRefreshed = await refreshOperationAuditAfterMutation();
    showToast("success", `成员密码 ${member.id} 的密码或权限已更新，原会话已退出${auditRefreshed ? "" : "；刷新页面可查看审计记录"}`);
  } catch (error) {
    showToast("error", error instanceof Error ? error.message : "成员密码更新失败");
  }
}

async function removeMember(member: MemberCredential) {
  try {
    await api.deleteMember(member.id);
    members.value = members.value.filter((item) => item.id !== member.id);
    deletingMemberId.value = "";
    const auditRefreshed = await refreshOperationAuditAfterMutation();
    showToast("success", `成员密码 ${member.id} 已删除${auditRefreshed ? "" : "；刷新页面可查看审计记录"}`);
  } catch (error) {
    showToast("error", error instanceof Error ? error.message : "成员密码删除失败");
  }
}

async function loadSettings() {
  settingsLoading.value = true;
  try {
    const [worldResult, iniResult] = await Promise.allSettled([
      isAdmin.value
        ? api.worldOption()
        : Promise.reject(new Error("WorldOption.sav 仅管理员可访问")),
      api.palworldSettings()
    ]);
    worldSettings.value = null;
    iniSettings.value = null;
    parsedWorldOption.value = null;
    worldSourceError.value = "";
    iniSourceError.value = "";
    worldRoundTripError.value = "";

    if (worldResult.status === "fulfilled") {
      try {
        const parsed = parseWorldOption(worldResult.value);
        const worldDocument = worldOptionSettings(parsed);
        parsedWorldOption.value = parsed;
        worldSettings.value = worldDocument;
        try {
          verifyWorldOptionRoundTrip(parsed, worldDocument);
        } catch (error) {
          worldRoundTripError.value = error instanceof Error ? error.message : "WorldOption.sav 往返校验失败";
        }
      } catch (error) {
        worldSourceError.value = error instanceof Error ? error.message : "WorldOption.sav 解析失败";
      }
    } else {
      worldSourceError.value = worldResult.reason instanceof Error ? worldResult.reason.message : "WorldOption.sav 读取失败";
    }

    if (iniResult.status === "fulfilled") {
      iniSettings.value = iniResult.value;
    } else {
      iniSourceError.value = iniResult.reason instanceof Error ? iniResult.reason.message : "PalWorldSettings.ini 读取失败";
    }

    if (!worldSettings.value && !iniSettings.value) {
      throw new Error("两个配置来源都无法读取");
    }
    worldDirtyKeys.value = new Set();
    iniDirtyKeys.value = new Set();
    if (settingsSource.value === "world" && !worldSettings.value) settingsSource.value = "ini";
    if (settingsSource.value === "ini" && !iniSettings.value) settingsSource.value = "world";
    selectSettingsSource(settingsSource.value);
  } catch (error) {
    showToast("error", error instanceof Error ? error.message : "配置加载失败");
  } finally {
    settingsLoading.value = false;
  }
}

async function loadDSTConfig() {
  dstConfigLoading.value = true;
  try {
    const [settingsResult, worldResult, configResult] = await Promise.allSettled([
      api.dstSettings(),
      api.dstWorldSettings(),
      api.dstConfig()
    ]);
    if (settingsResult.status === "rejected") throw settingsResult.reason;
    if (configResult.status === "rejected") throw configResult.reason;
    const settingsDocument = settingsResult.value;
    const document = configResult.value;
    dstSettings.value = settingsDocument;
    dstConfig.value = document;
    dstConfigDrafts.value = Object.fromEntries(document.files.map((file) => [file.id, file.content]));
    dstSettingsDirtyKeys.value = new Set();
    dstWorldDirtyKeys.value = new Set();
    dstConfigDirtyFiles.value = new Set();
    if (!document.files.some((file) => file.id === dstConfigActiveFile.value)) {
      dstConfigActiveFile.value = document.files[0]?.id ?? "cluster";
    }
    if (!settingsDocument.groups.some((group) => group.id === dstSettingsGroup.value)) {
      dstSettingsGroup.value = settingsDocument.groups[0]?.id ?? "server";
    }
    if (worldResult.status === "fulfilled") {
      const worldDocument = worldResult.value;
      dstWorldSettings.value = worldDocument;
      dstWorldSourceError.value = "";
      if (!worldDocument.shards.some((shard) => shard.id === dstWorldShard.value)) {
        dstWorldShard.value = worldDocument.shards[0]?.id ?? "master";
      }
      const selectedWorldShard = worldDocument.shards.find((shard) => shard.id === dstWorldShard.value);
      if (!selectedWorldShard?.groups.some((group) => group.id === dstWorldGroup.value)) {
        dstWorldGroup.value = selectedWorldShard?.groups[0]?.id ?? "generation";
      }
    } else {
      dstWorldSettings.value = null;
      dstWorldSourceError.value = worldResult.reason instanceof Error ? worldResult.reason.message : "worldgenoverride.lua 无法安全解析";
      if (dstSettingsMode.value === "world") dstSettingsMode.value = "advanced";
    }
  } catch (error) {
    dstConfig.value = null;
    dstSettings.value = null;
    dstWorldSettings.value = null;
    dstWorldSourceError.value = "";
    showToast("error", error instanceof Error ? error.message : "DST 配置加载失败");
  } finally {
    dstConfigLoading.value = false;
  }
}

function updateDSTConfigDraft(value: string) {
  dstConfigDrafts.value = { ...dstConfigDrafts.value, [dstConfigActiveFile.value]: value };
  const next = new Set(dstConfigDirtyFiles.value);
  next.add(dstConfigActiveFile.value);
  dstConfigDirtyFiles.value = next;
}

async function saveDSTConfig() {
  if (!dstConfig.value || !dstConfigDirty.value) return;
  dstConfigSaving.value = true;
  try {
    const files = Object.fromEntries(
      [...dstConfigDirtyFiles.value].map((id) => [id, dstConfigDrafts.value[id] ?? ""])
    );
    await api.updateDSTConfig({
      revision: dstConfig.value.revision,
      files
    });
    await loadDSTConfig();
    showToast("success", "DST 配置已保存；请重新启动 Master/Caves 使其生效");
  } catch (error) {
    showToast("error", error instanceof Error ? error.message : "DST 配置保存失败");
  } finally {
    dstConfigSaving.value = false;
  }
}

function setDSTSettingValue(setting: Setting, value: string | number | boolean) {
  setting.value = value;
  const next = new Set(dstSettingsDirtyKeys.value);
  next.add(setting.key);
  dstSettingsDirtyKeys.value = next;
}

function resetDSTSetting(setting: Setting) {
  setDSTSettingValue(setting, setting.default);
}

function setDSTWorldSettingValue(setting: Setting, value: string | number | boolean) {
  setting.value = value;
  const next = new Set(dstWorldDirtyKeys.value);
  next.add(setting.key);
  dstWorldDirtyKeys.value = next;
}

function resetDSTWorldSetting(setting: Setting) {
  setDSTWorldSettingValue(setting, setting.default);
}

function selectDSTWorldShard(shard: "master" | "caves") {
  dstWorldShard.value = shard;
  dstWorldGroup.value = activeDSTWorldShard.value?.groups[0]?.id ?? "generation";
}

async function saveDSTSettings() {
  if (!dstSettings.value || !dstSettingsDirty.value) return;
  dstConfigSaving.value = true;
  try {
    const changes: Record<string, string | number | boolean> = {};
    for (const group of dstSettings.value.groups) {
      for (const setting of group.settings) {
        if (dstSettingsDirtyKeys.value.has(setting.key)) changes[setting.key] = setting.value;
      }
    }
    await api.updateDSTSettings({ revision: dstSettings.value.revision, changes });
    await loadDSTConfig();
    showToast("success", "DST 常用配置已保存；请重新启动 Master/Caves 使其生效");
  } catch (error) {
    showToast("error", error instanceof Error ? error.message : "DST 参数保存失败");
  } finally {
    dstConfigSaving.value = false;
  }
}

async function saveDSTWorldSettings() {
  if (!dstWorldSettings.value || !dstWorldDirty.value) return;
  dstConfigSaving.value = true;
  try {
    const changes: Record<string, string | number | boolean> = {};
    for (const shard of dstWorldSettings.value.shards) {
      for (const group of shard.groups) {
        for (const setting of group.settings) {
          if (dstWorldDirtyKeys.value.has(setting.key)) changes[setting.key] = setting.value;
        }
      }
    }
    await api.updateDSTWorldSettings({ revision: dstWorldSettings.value.revision, changes });
    await loadDSTConfig();
    showToast("success", "DST 世界规则已保存；现有存档不会自动重建，规则将在新世界生成时生效");
  } catch (error) {
    showToast("error", error instanceof Error ? error.message : "DST 世界规则保存失败");
  } finally {
    dstConfigSaving.value = false;
  }
}

function saveActiveDSTSettings() {
  if (dstSettingsMode.value === "advanced") void saveDSTConfig();
  else if (dstSettingsMode.value === "world") void saveDSTWorldSettings();
  else void saveDSTSettings();
}

function selectSettingsSource(source: SettingsSource) {
  settingsSource.value = source;
  settingsSearch.value = "";
  settingsGroup.value = settings.value?.groups[0]?.id ?? "";
}

function hasPermission(permission: Permission): boolean {
  return isAdmin.value || currentPermissions.value.includes(permission);
}

function permissionForAction(action: ActionName): Permission {
  if (action === "update") return "game.update";
  if (action === "backup") return "game.backup";
  return "game.control";
}

function hasActionPermission(action: ActionName): boolean {
  return hasPermission(permissionForAction(action));
}

function actionDisabledReason(game: Game, action: ActionName): string | undefined {
  if (!hasActionPermission(action)) return "当前成员密码没有此操作权限";
  if (action === "update" && !game.updateSupported) return "当前游戏暂未接入安全更新";
  if (action === "backup" && !game.backupSupported) return "当前游戏暂未接入面板备份";
  if (action === "backup" && game.backupRequiresStopped && game.state !== "stopped") {
    return "DST 没有在线保存通道，请先停止 Master/Caves 再备份";
  }
  if (
    game.state === "running" &&
    !game.restAvailable &&
    !(action === "update" && game.updateRequiresUnsafeStop) &&
    action !== "start" &&
    action !== "stop" &&
    action !== "restart"
  ) {
    return "REST API 不可用，无法安全执行此操作";
  }
  return undefined;
}

function canRunSafeAction(game: Game, action: ActionName): boolean {
  return !actionDisabledReason(game, action);
}

function askAction(game: Game, action: ActionName) {
  if (!hasActionPermission(action)) {
    showToast("error", "当前成员密码没有执行此操作的权限");
    return;
  }
  if (!canRunSafeAction(game, action)) {
    showToast("error", actionDisabledReason(game, action) ?? "当前无法安全执行此操作");
    return;
  }
  confirmAction.value = { game, action };
}

function usesUnsafeFallback(game: Game, action: ActionName): boolean {
  return (
    game.state !== "stopped" &&
    !game.restAvailable &&
    (actionAllowsUnsafeFallback(action) || (action === "update" && game.updateRequiresUnsafeStop))
  );
}

function actionAllowsUnsafeFallback(action: ActionName): boolean {
  return action === "stop" || action === "restart";
}

function activityProgress(item: { progress?: number }): number {
  return Math.max(0, Math.min(100, item.progress ?? 0));
}

async function executeAction() {
  if (!confirmAction.value) return;
  const { game, action } = confirmAction.value;
  const allowUnsafe = actionAllowsUnsafeFallback(action) ||
    (action === "update" && game.updateRequiresUnsafeStop && game.state !== "stopped");
  actionBusy.value = true;
  try {
    await api.action(game.id, action, allowUnsafe);
    confirmAction.value = null;
    showToast("success", `${actionLabel(action)}任务已进入执行队列`);
    await refresh(true);
    schedulePolling(1000);
  } catch (error) {
    showToast("error", error instanceof Error ? error.message : "操作失败");
  } finally {
    actionBusy.value = false;
  }
}

async function requestVersionCheck(game: Game) {
  if (!hasPermission("game.update")) {
    showToast("error", "当前成员密码没有检查服务端版本的权限");
    return;
  }
  versionCheckSubmitting.value = true;
  try {
    await api.action(game.id, "check-update");
    showToast("success", "版本检查任务已进入执行队列");
    await refresh(true);
    schedulePolling(1000);
  } catch (error) {
    showToast("error", error instanceof Error ? error.message : "版本检查失败");
  } finally {
    versionCheckSubmitting.value = false;
  }
}

async function saveSettings() {
  if (!settings.value) return;
  if (settingsSource.value === "world" && !parsedWorldOption.value) return;
  settingsSaving.value = true;
  try {
    if (settingsSource.value === "world") {
      if (worldRoundTripError.value) throw new Error(worldRoundTripError.value);
      const parsed = parsedWorldOption.value;
      if (!worldSettings.value || !parsed) return;
      const encoded = encodeWorldOption(
        parsed,
        worldSettings.value,
        worldDirtyKeys.value
      );
      const candidate = { ...parsed.document, data: encoded.data };
      verifyWorldOptionData(candidate, encoded.semantic);
      await api.updateWorldOption(candidate);
      showToast("success", "仅修改的 WorldOption.sav 参数已备份并保存");
    } else {
      if (!iniSettings.value) return;
      const changes: Record<string, string | number | boolean> = {};
      for (const group of iniSettings.value.groups) {
        for (const setting of group.settings) {
          if (iniDirtyKeys.value.has(setting.key)) changes[setting.key] = setting.value;
        }
      }
      await api.updatePalworldSettings({
        revision: iniSettings.value.revision,
        changes
      });
      showToast("success", "仅修改的 PalWorldSettings.ini 参数已备份并保存");
    }
    await loadSettings();
  } catch (error) {
    showToast("error", error instanceof Error ? error.message : "配置保存失败");
  } finally {
    settingsSaving.value = false;
  }
}

function setSettingValue(setting: Setting, value: string | number | boolean) {
  setting.value = value;
  const current = settingsSource.value === "world" ? worldDirtyKeys.value : iniDirtyKeys.value;
  const next = new Set(current);
  next.add(setting.key);
  if (settingsSource.value === "world") worldDirtyKeys.value = next;
  else iniDirtyKeys.value = next;
}

function resetSetting(setting: Setting) {
  setSettingValue(setting, setting.default);
}

function findSetting(document: PalworldSettings | null, key: string): Setting | undefined {
  for (const group of document?.groups ?? []) {
    const setting = group.settings.find((item) => item.key === key);
    if (setting) return setting;
  }
  return undefined;
}

function hasSourceConflict(setting: Setting): boolean {
  const other = findSetting(settingsSource.value === "world" ? iniSettings.value : worldSettings.value, setting.key);
  if (!other || !setting.configured || !other.configured) return false;
  const normalize = (item: Setting) => item.sensitive
    ? (String(item.value) === "" ? "empty" : "set")
    : JSON.stringify(item.value);
  return normalize(setting) !== normalize(other);
}

function settingSourceLabel(setting: Setting): string {
  const source = settingsSource.value === "world" ? "WorldOption.sav" : "PalWorldSettings.ini";
  return setting.configured ? source : source + " · 默认值";
}

function applyPermissionPreset(target: "new" | "edit", permissions: Permission[]) {
  const next = [...permissions];
  if (target === "new") newMemberPermissions.value = next;
  else editingMemberPermissions.value = next;
}

function toggleMemberPermission(
  target: "new" | "edit",
  permission: Permission,
  enabled: boolean
) {
  const current = target === "new" ? newMemberPermissions.value : editingMemberPermissions.value;
  const next = enabled
    ? Array.from(new Set([...current, permission]))
    : current.filter((item) => item !== permission);
  if (target === "new") newMemberPermissions.value = next;
  else editingMemberPermissions.value = next;
}

function presetSelected(current: Permission[], preset: Permission[]): boolean {
  return current.length === preset.length && preset.every((item) => current.includes(item));
}

function permissionLabel(permission: Permission): string {
  return permissionDefinitions.find((item) => item.id === permission)?.label ?? permission;
}

function showToast(type: "success" | "error", message: string) {
  toast.value = { type, message };
  if (toastTimer) window.clearTimeout(toastTimer);
  toastTimer = window.setTimeout(() => (toast.value = null), 3600);
}

function actionLabel(action: string) {
  return {
    start: "启动",
    stop: "停止",
    restart: "重启",
    update: "更新",
    backup: "备份"
  }[action] ?? action;
}

function stateLabel(state: Game["state"]) {
  return {
    running: "运行中",
    stopped: "已停止",
    starting: "启动中",
    stopping: "停止中",
    error: "异常"
  }[state];
}

function managementStateLabel(state: ManagedGame["state"]) {
  return {
    managed: "已管理",
    detected: "已发现",
    not_installed: "未安装",
    installing: "安装中",
    error: "需要处理"
  }[state];
}

function modCompatibilityLabel(compatibility: ModInventory["mods"][number]["compatibility"]) {
  return {
    supported: "支持服务端",
    unsupported: "不支持服务端",
    unknown: "兼容性未知"
  }[compatibility];
}

function modSourceLabel(source: ModInventory["mods"][number]["source"]) {
  return {
    official_package: "Palworld 官方包",
    steam_workshop: "Steam Workshop",
    existing: "现有文件"
  }[source];
}

function versionCheckLabel(game: Game) {
  if (game.versionCheck === "unchecked") return "等待自动检查服务端版本";
  if (game.versionCheck === "checking") return `正在检查${game.name}服务端版本`;
  if (game.versionCheck === "current") return "服务端已是最新版";
  if (game.versionCheck === "update_available") {
    return game.availableVersion
      ? `可更新至 Steam Build ${game.availableVersion}`
      : `${game.name}服务端有可用更新`;
  }
  if (game.versionCheck === "unavailable") return "服务端版本检查暂不可用";
  return "";
}

function formatDuration(seconds: number) {
  if (!seconds) return "—";
  const hours = Math.floor(seconds / 3600);
  const minutes = Math.floor((seconds % 3600) / 60);
  return `${hours} 小时 ${minutes} 分`;
}

function formatRelative(value?: string) {
  if (!value) return "尚无记录";
  const delta = Date.now() - new Date(value).getTime();
  const minutes = Math.max(1, Math.floor(delta / 60000));
  if (minutes < 60) return `${minutes} 分钟前`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours} 小时前`;
  return `${Math.floor(hours / 24)} 天前`;
}

function formatDateTime(value?: string) {
  if (!value) return "—";
  return new Intl.DateTimeFormat("zh-CN", {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    hour12: false
  }).format(new Date(value));
}

function auditCredentialLabel(entry: LoginAuditEntry) {
  if (entry.credentialId === "ADMIN") return "管理员凭据";
  if (entry.credentialId.startsWith("M-")) return `成员密码 ${entry.credentialId}`;
  if (entry.credentialId === "RATE-LIMITED") return "已触发登录限制";
  if (entry.credentialId === "BLOCKED") return "IP 黑名单拦截";
  if (entry.credentialId === "INVALID-REQUEST") return "无效登录请求";
  return "未匹配凭据";
}

function auditResultLabel(entry: LoginAuditEntry) {
  if (entry.success) return "登录成功";
  return entry.reason || "登录失败";
}

function upsertIPRule(rule: IPRule) {
  ipRules.value = [rule, ...ipRules.value.filter((item) => item.ip !== rule.ip)];
}

async function addIPRule() {
  const ip = newRuleIP.value.trim();
  if (!ip) {
    showToast("error", "请输入一个明确的 IPv4 或 IPv6 地址");
    return;
  }
  if (
    newRuleKind.value === "deny" &&
    !window.confirm("确认加入黑名单？如果这是当前访问 IP，保存后当前网络将无法继续登录。")
  ) return;
  savingIPRule.value = true;
  try {
    const rule = await api.createIPRule(ip, newRuleKind.value, {
      note: newRuleNote.value.trim() || undefined,
      expiresInHours: newRulePermanent.value ? undefined : newRuleHours.value,
      permanent: newRulePermanent.value,
      confirmCurrentIp: newRuleKind.value === "deny"
    });
    upsertIPRule(rule);
    newRuleIP.value = "";
    newRuleNote.value = "";
    const auditRefreshed = await refreshOperationAuditAfterMutation();
    showToast("success", `${ip} 已加入${newRuleKind.value === "deny" ? "黑" : "白"}名单${auditRefreshed ? "" : "；刷新页面可查看审计记录"}`);
  } catch (error) {
    showToast("error", error instanceof Error ? error.message : "IP 规则保存失败");
  } finally {
    savingIPRule.value = false;
  }
}

async function quickSetIPRule(entry: LoginAuditEntry, kind: IPRule["kind"]) {
  auditMenuEntryId.value = "";
  if (!entry.ip) return;
  const label = kind === "deny" ? "黑名单 24 小时" : "白名单 7 天";
  if (!window.confirm(`确认将 ${entry.ip} 加入${label}？`)) return;
  try {
    const rule = await api.createIPRule(entry.ip, kind, {
      note: `从登录审计 ${entry.id} 快速添加`,
      expiresInHours: kind === "deny" ? 24 : 7 * 24,
      confirmCurrentIp: kind === "deny"
    });
    upsertIPRule(rule);
    const auditRefreshed = await refreshOperationAuditAfterMutation();
    showToast("success", `${entry.ip} 已加入${kind === "deny" ? "黑" : "白"}名单${auditRefreshed ? "" : "；刷新页面可查看审计记录"}`);
  } catch (error) {
    showToast("error", error instanceof Error ? error.message : "IP 规则保存失败");
  }
}

async function removeIPRule(rule: IPRule) {
  if (!window.confirm(`确认删除 ${rule.ip} 的${rule.kind === "deny" ? "黑" : "白"}名单规则？`)) {
    return;
  }
  deletingIPRuleId.value = rule.id;
  try {
    await api.deleteIPRule(rule.id);
    ipRules.value = ipRules.value.filter((item) => item.id !== rule.id);
    const auditRefreshed = await refreshOperationAuditAfterMutation();
    showToast("success", `IP 规则已删除${auditRefreshed ? "" : "；刷新页面可查看审计记录"}`);
  } catch (error) {
    showToast("error", error instanceof Error ? error.message : "IP 规则删除失败");
  } finally {
    deletingIPRuleId.value = "";
  }
}

async function copyAuditIP(entry: LoginAuditEntry) {
  auditMenuEntryId.value = "";
  try {
    await navigator.clipboard.writeText(entry.ip);
    showToast("success", "IP 地址已复制");
  } catch {
    showToast("error", "浏览器未允许复制，请手动选择 IP");
  }
}

function ruleExpiryLabel(rule: IPRule) {
  if (!rule.expiresAt) return "永久";
  const expiresAt = new Date(rule.expiresAt).getTime();
  if (expiresAt <= Date.now()) return "已到期";
  return `有效至 ${formatDateTime(rule.expiresAt)}`;
}

function configAuditCredentialLabel(entry: ConfigAuditEntry) {
  return entry.role === "admin" ? "管理员" : `成员 ${entry.credentialId}`;
}

function configAuditChangeLabel(change: ConfigAuditChange) {
  if (change.sensitive) return "敏感值已修改";
  return `${change.before || "（空）"} → ${change.after || "（空）"}`;
}

function operationActorLabel(entry: OperationAuditEntry) {
  return entry.actorRole === "admin" ? "管理员" : `成员 ${entry.actorCredentialId}`;
}

function operationActionLabel(entry: OperationAuditEntry) {
  switch (entry.event) {
    case "member_created": return "创建成员凭据";
    case "member_updated": return "修改成员凭据";
    case "member_deleted": return "删除成员凭据";
    case "ip_rule_added": return `保存${entry.ruleKind === "deny" ? "黑" : "白"}名单规则`;
    case "ip_rule_removed": return `删除${entry.ruleKind === "deny" ? "黑" : "白"}名单规则`;
    case "game_adopted": return "接管游戏服务器";
    case "game_install_started": return "启动游戏安装";
    case "dst_token_updated": return "更新 DST Cluster Token";
    case "system_settings_updated": return "保存后台设置";
    case "panel_update_checked": return "检查面板更新";
    case "panel_update_started": return "启动面板更新";
    case "panel_update_succeeded": return "面板更新完成";
    case "panel_update_rolled_back": return "面板更新已回滚";
    case "panel_update_failed": return "面板更新失败";
  }
}

function operationTargetLabel(entry: OperationAuditEntry) {
  if (entry.targetType === "member") return entry.targetId || "未知成员";
  if (entry.targetType === "ip_rule") return entry.targetIp || "未知 IP";
  if (entry.targetType === "game") return entry.targetId || "未知游戏";
  return "Hearth";
}

function operationDetailLabel(entry: OperationAuditEntry) {
  if (entry.event === "member_created") {
    return entry.currentPermissions?.length
      ? `初始权限：${entry.currentPermissions.map(permissionLabel).join("、")}`
      : "初始权限：只读";
  }
  if (entry.event === "member_updated") {
    const changes: string[] = [];
    if (entry.passwordChanged) changes.push("密码已修改");
    if (entry.permissionsChanged) {
      changes.push(entry.currentPermissions?.length
        ? `权限：${entry.currentPermissions.map(permissionLabel).join("、")}`
        : "权限：只读");
    }
    return changes.join(" · ") || "成员信息已更新";
  }
  if (entry.event === "ip_rule_added") {
    return entry.expiresAt ? `有效至 ${formatDateTime(entry.expiresAt)}` : "永久生效";
  }
  if (entry.event === "member_deleted") return "成员凭据已删除，原会话已失效";
  if (entry.event === "game_adopted") return "已保存经确认的现有安装路径";
  if (entry.event === "game_install_started") return "安装任务已进入队列，完成后不会自动启动";
  if (entry.event === "dst_token_updated") return "Token 已写入 DST cluster_token.txt；审计不保存 Token 内容";
  if (entry.event === "system_settings_updated") return "配置已保存；页面会提示是否需要重启 Hearth";
  if (entry.event === "panel_update_checked") return "已查询固定官方仓库的 Release 元数据";
  if (entry.event === "panel_update_started") return "已确认下载、校验并由独立更新器执行健康检查与回滚";
  if (entry.event === "panel_update_succeeded") return `已从 ${entry.previousVersion || "旧版本"} 更新至 ${entry.updateVersion || "新版本"}`;
  if (entry.event === "panel_update_rolled_back") return entry.detail || `新版本未通过健康检查，已恢复 ${entry.previousVersion || "旧版本"}`;
  if (entry.event === "panel_update_failed") return entry.detail || "更新未完成，请查看 panel-update.log";
  return "IP 规则已删除";
}

function gameAccent(id: string) {
  return id === "palworld" ? "pal" : "dst";
}
</script>

<template>
  <div v-if="booting" class="boot-screen">
    <div class="brand-mark"><Flame :size="25" /></div>
    <LoaderCircle class="spin" :size="21" />
  </div>

  <main v-else-if="!loggedIn" class="login-page">
    <div class="login-ambient login-ambient-a"></div>
    <div class="login-ambient login-ambient-b"></div>
    <section class="login-panel">
      <div class="login-brand">
        <div class="brand-mark"><Flame :size="24" /></div>
        <div>
          <strong>Hearth</strong>
          <span>Game server home</span>
        </div>
      </div>
      <div class="login-copy">
        <div class="eyebrow"><ShieldCheck :size="14" /> PRIVATE CONSOLE</div>
        <h1>欢迎回来</h1>
        <p>管理游戏、更新版本，以及确认朋友们随时都能连接。</p>
      </div>
      <form class="login-form" @submit.prevent="login">
        <label for="password">访问密码</label>
        <div class="input-with-icon">
          <KeyRound :size="18" />
          <input
            id="password"
            v-model="loginPassword"
            type="password"
            autocomplete="current-password"
            placeholder="输入管理员或成员密码"
            autofocus
          />
        </div>
        <p v-if="loginError" class="form-error">{{ loginError }}</p>
        <button class="button primary wide" :disabled="loggingIn || !loginPassword">
          <LoaderCircle v-if="loggingIn" class="spin" :size="17" />
          <span>{{ loggingIn ? "正在验证…" : "进入控制台" }}</span>
          <ChevronRight v-if="!loggingIn" :size="17" />
        </button>
      </form>
      <div class="login-footnote">
        <span class="status-dot online"></span>
        管理服务在线 · 面板 v{{ buildVersion }}
      </div>
    </section>
    <aside class="login-preview">
      <div class="preview-label">LIVE SYSTEM</div>
      <div class="preview-orbit">
        <div class="preview-core"><Server :size="34" /></div>
        <div class="orbit-ring orbit-ring-a"></div>
        <div class="orbit-ring orbit-ring-b"></div>
        <span class="orbit-node node-a"></span>
        <span class="orbit-node node-b"></span>
        <span class="orbit-node node-c"></span>
      </div>
      <div class="preview-stats">
        <div><span>LOCAL</span><small>PRIVATE PANEL</small></div>
        <div><span>WIN</span><small>SERVER</small></div>
        <div><span>PAL</span><small>PALWORLD</small></div>
      </div>
    </aside>
  </main>

  <div v-else class="app-shell">
    <div
      v-if="mobileNavOpen"
      class="mobile-scrim"
      @click="mobileNavOpen = false"
    ></div>
    <aside class="sidebar" :class="{ open: mobileNavOpen }">
      <div class="sidebar-brand">
        <div class="brand-mark"><Flame :size="22" /></div>
        <div>
          <strong>Hearth</strong>
          <span>Game server home</span>
        </div>
        <button class="icon-button sidebar-close" @click="mobileNavOpen = false">
          <X :size="19" />
        </button>
      </div>

      <nav class="main-nav">
        <span class="nav-label">工作区</span>
        <button :class="{ active: page === 'overview' }" @click="navigate('overview')">
          <LayoutDashboard :size="18" />
          <span>服务器总览</span>
        </button>
        <button
          v-for="game in overview?.games ?? []"
          :key="game.id"
          :class="{ active: page === 'game' && selectedGameId === game.id }"
          @click="navigate('game', game.id)"
        >
          <Gamepad2 :size="18" />
          <span>{{ game.name }}</span>
          <i :class="['nav-state', game.state]"></i>
        </button>

        <span v-if="canOpenPalworldSettings || canOpenDSTSettings || isAdmin" class="nav-label nav-section">管理</span>
        <button v-if="canOpenPalworldSettings" :class="{ active: page === 'settings' && settingsGameId === 'palworld' }" @click="navigate('settings', 'palworld')">
          <SlidersHorizontal :size="18" />
          <span>帕鲁配置</span>
        </button>
        <button v-if="canOpenPalworldMods" :class="{ active: page === 'mods' }" @click="navigate('mods', 'palworld')">
          <PackageSearch :size="18" />
          <span>帕鲁模组</span>
        </button>
        <button v-if="canOpenDSTSettings" :class="{ active: page === 'settings' && settingsGameId === 'dont-starve-together' }" @click="navigate('settings', 'dont-starve-together')">
          <SlidersHorizontal :size="18" />
          <span>饥荒配置</span>
        </button>
        <button v-if="isAdmin" :class="{ active: page === 'logs' }" @click="navigate('logs')">
          <TerminalSquare :size="18" />
          <span>任务日志</span>
        </button>
        <button v-if="isAdmin" :class="{ active: page === 'access' }" @click="navigate('access')">
          <ShieldCheck :size="18" />
          <span>访问权限</span>
        </button>
        <button v-if="isAdmin" :class="{ active: page === 'system' }" @click="navigate('system')">
          <Settings2 :size="18" />
          <span>系统设置</span>
        </button>
      </nav>

      <div class="sidebar-status-wrap">
        <button class="sidebar-status" @click="nodeMenuOpen = !nodeMenuOpen">
          <div class="server-avatar"><Server :size="17" /></div>
          <div>
            <strong>Windows ECS</strong>
            <span><i></i> 节点在线</span>
            <small>
              {{ isAdmin ? "管理员" : `成员 ${credentialId}` }} · v{{ buildVersion }}
            </small>
          </div>
          <MoreHorizontal :size="18" />
        </button>
        <div v-if="nodeMenuOpen" class="action-menu node-action-menu">
          <button @click="refresh(); nodeMenuOpen = false"><RefreshCw :size="15" />刷新节点状态</button>
          <button v-if="isAdmin" @click="navigate('logs')"><TerminalSquare :size="15" />查看任务日志</button>
          <button @click="logout"><LogOut :size="15" />退出登录</button>
        </div>
      </div>
    </aside>

    <section class="content-shell">
      <header class="topbar">
        <button class="icon-button mobile-menu" @click="mobileNavOpen = true">
          <Menu :size="20" />
        </button>
        <div class="topbar-title">
          <button
            v-if="page !== 'overview'"
            class="back-button"
            @click="navigate('overview')"
          >
            <ArrowLeft :size="17" />
          </button>
          <div>
            <span>{{ page === "overview" ? "控制台" : page === "system" ? "系统管理" : "游戏服务器" }}</span>
            <strong>{{ pageTitle }}</strong>
          </div>
        </div>
        <div class="topbar-actions">
          <div class="node-pill"><span></span> 节点正常</div>
          <button v-if="isAdmin" class="icon-button" title="任务日志" @click="navigate('logs')"><Bell :size="19" /></button>
          <button class="avatar-button" :title="`${isAdmin ? '管理员' : '成员'} · 点击退出`" @click="logout">
            {{ isAdmin ? "A" : "M" }}
          </button>
        </div>
      </header>

      <div v-if="loadError" class="load-error">
        <AlertTriangle :size="18" />
        <span>{{ loadError }}</span>
        <button @click="refresh()">重试</button>
      </div>

      <div v-if="loading && !overview" class="page-loader">
        <LoaderCircle class="spin" :size="24" /> 正在连接节点…
      </div>

      <main v-else-if="overview" class="page-content">
        <template v-if="page === 'overview'">
          <section class="welcome-row">
            <div>
              <span class="eyebrow">{{ todayLabel }}</span>
              <h1>{{ greetingLabel }}，{{ isAdmin ? "管理员" : "成员" }}</h1>
              <p>{{ runningCount }} 个服务器正在运行，节点资源处于正常范围。</p>
            </div>
            <button class="button ghost" @click="refresh()">
              <RefreshCw :size="16" :class="{ spin: loading }" />
              刷新状态
            </button>
          </section>

          <section v-if="!hasManagedGames" class="onboarding-card panel-card">
            <div class="onboarding-icon"><FolderSearch :size="30" /></div>
            <div>
              <span class="eyebrow">FIRST SERVER</span>
              <h2>{{ isAdmin ? "还没有接入游戏服务器" : "游戏服务器尚未配置" }}</h2>
              <p v-if="isAdmin">
                Hearth 只完成了只读探测，没有自动下载、安装或修改任何游戏文件。
                可以检查发现的现有 Palworld，或者确认目录后开始新安装。
              </p>
              <p v-else>请联系管理员完成现有服务器接管或新服务器安装。</p>
              <div v-if="isAdmin" class="onboarding-actions">
                <button class="button primary" @click="navigate('system')">
                  <FolderSearch :size="16" />打开启动向导
                </button>
                <span v-if="palworldManagement?.state === 'detected'">
                  已发现 {{ palworldManagement.candidates?.length ?? 0 }} 个 Palworld 候选
                </span>
              </div>
            </div>
          </section>

          <section class="metric-grid">
            <article class="metric-card">
              <div class="metric-head">
                <span class="metric-icon cpu"><Cpu :size="18" /></span>
                <small>CPU 使用率</small>
                <i class="trend">稳定</i>
              </div>
              <strong>{{ overview.host.cpuPercent.toFixed(1) }}<em>%</em></strong>
              <Sparkline :points="overview.host.cpuHistory" color="#7697ff" :height="54" />
              <footer>4 核 · 负载 {{ overview.host.loadOne.toFixed(2) }}</footer>
            </article>
            <article class="metric-card">
              <div class="metric-head">
                <span class="metric-icon memory"><MemoryStick :size="18" /></span>
                <small>内存占用</small>
                <i class="trend warm">较高</i>
              </div>
              <strong>{{ overview.host.memoryUsedGB.toFixed(1) }}<em> GB</em></strong>
              <Sparkline :points="overview.host.memoryHistory" color="#a178ff" :height="54" />
              <footer>
                {{ overview.host.memoryPercent.toFixed(0) }}% · 共
                {{ overview.host.memoryTotalGB }} GB
              </footer>
            </article>
            <article class="metric-card disk-card">
              <div class="metric-head">
                <span class="metric-icon disk"><HardDrive :size="18" /></span>
                <small>磁盘空间</small>
              </div>
              <strong>{{ overview.host.diskUsedGB.toFixed(1) }}<em> GB</em></strong>
              <div class="progress">
                <span :style="{ width: `${overview.host.diskPercent}%` }"></span>
              </div>
              <footer>
                {{ overview.host.diskPercent.toFixed(0) }}% · 共
                {{ overview.host.diskTotalGB }} GB
              </footer>
            </article>
            <article class="metric-card">
              <div class="metric-head">
                <span class="metric-icon player"><Users :size="18" /></span>
                <small>在线玩家</small>
              </div>
              <strong>
                {{ playerSummary.online === null ? "—" : playerSummary.online }}
                <em> / {{ playerSummary.max ?? "?" }}</em>
              </strong>
              <div :class="['player-data-status', { unavailable: playerSummary.online === null }]">
                <span></span>{{ playerSummary.source }}
              </div>
              <footer>{{ hasManagedGames ? "任务期间每 1 秒刷新" : "配置游戏后显示在线数据" }}</footer>
            </article>
          </section>

          <section v-if="hasManagedGames" class="section-block">
            <div class="section-heading">
              <div>
                <h2>游戏服务器</h2>
                <p>状态、版本与快捷操作</p>
              </div>
              <span>{{ overview.games.length }} 个实例</span>
            </div>
            <div class="game-grid">
              <article
                v-for="game in overview.games"
                :key="game.id"
                class="game-card"
                :class="gameAccent(game.id)"
              >
                <div class="game-art">
                  <div class="game-monogram">{{ game.shortName }}</div>
                  <div class="game-art-copy">
                    <span :class="['status-badge', game.state]">
                      <i></i>{{ stateLabel(game.state) }}
                    </span>
                    <h3>{{ game.name }}</h3>
                    <p>端口 {{ game.port }} · {{ game.tags.join(" / ") }}</p>
                  </div>
                </div>
                <div class="game-body">
                  <div class="game-facts">
                    <div>
                      <small>在线玩家</small>
                      <strong v-if="game.playersAvailable">
                        {{ game.playersOnline }}<em>/{{ game.playersMaxKnown ? game.playersMax : "?" }}</em>
                      </strong>
                      <strong v-else class="fact-text">暂不可用</strong>
                    </div>
                    <div>
                      <small>内存</small>
                      <strong>{{ game.memoryGB.toFixed(1) }}<em> GB</em></strong>
                    </div>
                    <div>
                      <small>运行时间</small>
                      <strong class="fact-text">{{ formatDuration(game.uptimeSeconds) }}</strong>
                    </div>
                  </div>
                  <div v-if="game.updateAvailable && !updateTaskRunning(game.id)" class="update-callout">
                    <div>
                      <RefreshCw :size="16" />
                      <span>
                        <strong>服务端有更新</strong>
                        {{ game.name }} · public 分支
                      </span>
                    </div>
                    <button
                      v-if="game.updateSupported"
                      :disabled="!canRunSafeAction(game, 'update')"
                      :title="actionDisabledReason(game, 'update')"
                      @click="askAction(game, 'update')"
                    >立即更新</button>
                    <small v-else>当前仅提供版本提示</small>
                  </div>
                  <div v-else class="version-row">
                    <Check :size="15" />
                    已安装版本 · {{ game.version }}<template v-if="game.versionSource"> · {{ game.versionSource }}</template>
                  </div>
                  <div class="game-actions">
                    <button
                      v-if="game.state === 'stopped'"
                      class="button primary small"
                      :disabled="!canRunSafeAction(game, 'start')"
                      :title="actionDisabledReason(game, 'start')"
                      @click="askAction(game, 'start')"
                    >
                      <Play :size="15" /> 启动
                    </button>
                    <button
                      v-else
                      class="button secondary small"
                      :disabled="game.state !== 'running' || !canRunSafeAction(game, 'restart')"
                      :title="actionDisabledReason(game, 'restart')"
                      @click="askAction(game, 'restart')"
                    >
                      <RotateCw :size="15" /> 重启
                    </button>
                    <button class="button text small" @click="navigate('game', game.id)">
                      查看详情 <ChevronRight :size="15" />
                    </button>
                  </div>
                </div>
              </article>
            </div>
          </section>

          <section :class="['overview-bottom', { 'health-only': !isAdmin }]">
            <article v-if="isAdmin" class="activity-card panel-card">
              <div class="card-heading">
                <div>
                  <h2>最近活动</h2>
                  <p>所有管理操作都会记录在这里</p>
                </div>
                <button class="button text small" @click="navigate('logs')">查看全部</button>
              </div>
              <div class="activity-list">
                <div v-for="item in (overview.activities ?? []).slice(0, 5)" :key="item.id" class="activity-item">
                  <span :class="['activity-icon', item.status]">
                    <LoaderCircle v-if="item.status === 'running'" class="spin" :size="16" />
                    <AlertTriangle v-else-if="item.status === 'warning'" :size="16" />
                    <Check v-else-if="item.status === 'success'" :size="16" />
                    <Activity v-else :size="16" />
                  </span>
                  <div class="activity-copy">
                    <strong>{{ item.title }}</strong>
                    <span>{{ item.detail }}</span>
                    <div v-if="item.status === 'running'" class="activity-progress-row">
                      <div class="activity-progress-track">
                        <i :style="{ width: `${activityProgress(item)}%` }"></i>
                      </div>
                      <small>{{ item.stage || "执行中" }} · {{ activityProgress(item) }}%</small>
                    </div>
                  </div>
                  <div class="activity-tail">
                    <button
                      v-if="item.logs?.length"
                      class="activity-log-button"
                      type="button"
                      @click="openActivityLogs(item)"
                    >
                      <TerminalSquare :size="13" />查看日志
                    </button>
                    <time>{{ formatRelative(item.updatedAt || item.createdAt) }}</time>
                  </div>
                </div>
              </div>
            </article>
            <article class="health-card panel-card">
              <div class="card-heading">
                <div>
                  <h2>节点健康</h2>
                  <p>关键服务检查</p>
                </div>
                <CircleGauge :size="19" />
              </div>
              <div class="health-score">
                <div class="score-ring"><span>{{ nodeHealthScore }}</span><small>/100</small></div>
                <div><strong>{{ nodeHealthLabel }}</strong><span>根据当前 CPU、内存、磁盘和进程状态计算</span></div>
              </div>
              <ul class="health-list">
                <li><span><i></i> 面板连接</span><strong>正常</strong></li>
                <li><span><i></i> 存档磁盘</span><strong>{{ overview.host.diskPercent.toFixed(1) }}%</strong></li>
                <li><span><i></i> 最近备份</span><strong>{{ formatRelative(palworldGame?.lastBackupAt) }}</strong></li>
              </ul>
            </article>
          </section>
        </template>

        <template v-else-if="page === 'game' && selectedGame">
          <section class="game-hero" :class="gameAccent(selectedGame.id)">
            <div class="game-hero-main">
              <div class="game-monogram large">{{ selectedGame.shortName }}</div>
              <div>
                <span :class="['status-badge', selectedGame.state]">
                  <i></i>{{ stateLabel(selectedGame.state) }}
                </span>
                <h1>{{ selectedGame.name }}</h1>
                <p>
                  版本 {{ selectedGame.version }} · 端口 {{ selectedGame.port }} ·
                  {{ selectedGame.tags.join(" / ") }}
                </p>
                <p class="save-id-line">
                  当前存档 ·
                  <strong>{{ selectedGame.saveId || "未识别" }}</strong>
                  <span v-if="selectedGame.saveDetection">通过 {{ selectedGame.saveDetection }} 识别</span>
                </p>
              </div>
            </div>
            <div class="hero-actions">
              <button
                v-if="selectedGame.state === 'stopped'"
                class="button primary"
                :disabled="!canRunSafeAction(selectedGame, 'start')"
                :title="actionDisabledReason(selectedGame, 'start')"
                @click="askAction(selectedGame, 'start')"
              >
                <Play :size="16" /> 启动服务器
              </button>
              <template v-else>
                <button
                  class="button secondary"
                  :disabled="selectedGame.state !== 'running' || !canRunSafeAction(selectedGame, 'restart')"
                  :title="actionDisabledReason(selectedGame, 'restart')"
                  @click="askAction(selectedGame, 'restart')"
                >
                  <RotateCw :size="16" /> 重启
                </button>
                <button
                  class="button danger-ghost"
                  :disabled="selectedGame.state !== 'running' || !canRunSafeAction(selectedGame, 'stop')"
                  :title="actionDisabledReason(selectedGame, 'stop')"
                  @click="askAction(selectedGame, 'stop')"
                >
                  <Square :size="15" /> 停止
                </button>
              </template>
              <div class="menu-anchor">
                <button class="icon-button" @click="gameMenuOpen = !gameMenuOpen">
                  <MoreHorizontal :size="19" />
                </button>
                <div v-if="gameMenuOpen" class="action-menu game-action-menu">
                  <button
                    v-if="selectedGame.backupSupported"
                    :disabled="!canRunSafeAction(selectedGame, 'backup')"
                    :title="actionDisabledReason(selectedGame, 'backup')"
                    @click="askAction(selectedGame, 'backup'); gameMenuOpen = false"
                  >
                    <DatabaseBackup :size="15" />创建备份
                  </button>
                  <button v-if="selectedGame.id === 'palworld' && canOpenPalworldSettings" @click="navigate('settings', 'palworld')">
                    <Settings2 :size="15" />编辑配置
                  </button>
                  <button v-if="selectedGame.id === 'palworld' && canOpenPalworldMods" @click="navigate('mods', 'palworld')">
                    <PackageSearch :size="15" />查看模组
                  </button>
                  <button v-if="selectedGame.id === 'dont-starve-together' && canOpenDSTSettings" @click="navigate('settings', 'dont-starve-together')">
                    <Settings2 :size="15" />编辑配置
                  </button>
                  <button v-if="isAdmin" @click="navigate('logs')">
                    <TerminalSquare :size="15" />查看日志
                  </button>
                </div>
              </div>
            </div>
          </section>

          <section v-if="selectedGameActivity" class="detail-task-banner panel-card">
            <span class="activity-icon running">
              <LoaderCircle class="spin" :size="17" />
            </span>
            <div class="detail-task-copy">
              <div>
                <strong>{{ selectedGameActivity.title }}</strong>
                <span>{{ selectedGameActivity.detail }}</span>
              </div>
              <div class="detail-task-progress">
                <div class="activity-progress-track">
                  <i :style="{ width: `${activityProgress(selectedGameActivity)}%` }"></i>
                </div>
                <small>
                  {{ selectedGameActivity.stage || "执行中" }} ·
                  {{ activityProgress(selectedGameActivity) }}%
                </small>
              </div>
            </div>
          </section>

          <section v-if="selectedGame.updateAvailable && !updateTaskRunning(selectedGame.id)" class="detail-update-banner">
            <div class="banner-icon"><RefreshCw :size="20" /></div>
            <div>
              <strong>发现新的服务端版本</strong>
              <span>
                当前 {{ selectedGame.version }} ·
                {{ selectedGame.availableVersion ? `可更新至 Steam Build ${selectedGame.availableVersion}` : `${selectedGame.name} public 分支有可用更新` }}
              </span>
            </div>
            <button
              v-if="selectedGame.updateSupported"
              class="button primary small"
              :disabled="!canRunSafeAction(selectedGame, 'update')"
              :title="actionDisabledReason(selectedGame, 'update')"
              @click="askAction(selectedGame, 'update')"
            >
              安全更新
            </button>
            <small v-else>当前版本仅检查和提示，不会修改游戏文件</small>
          </section>

          <section class="detail-grid">
            <article class="chart-card panel-card">
              <div class="card-heading">
                <div><h2>CPU 使用率</h2><p>最近 2 小时</p></div>
                <strong>{{ selectedGame.cpuPercent.toFixed(1) }}%</strong>
              </div>
              <Sparkline :points="selectedGame.cpuHistory" color="#7697ff" :height="145" />
            </article>
            <article class="chart-card panel-card">
              <div class="card-heading">
                <div><h2>内存占用</h2><p>最近 2 小时</p></div>
                <strong>{{ selectedGame.memoryGB.toFixed(2) }} GB</strong>
              </div>
              <Sparkline :points="selectedGame.memoryHistory" color="#a178ff" :height="145" />
            </article>
          </section>

          <section class="detail-lower">
            <article class="panel-card server-info-card">
              <div class="card-heading">
                <div><h2>服务器信息</h2><p>当前实例状态</p></div>
                <Server :size="19" />
              </div>
              <dl class="info-grid">
                <div><dt>运行时间</dt><dd>{{ formatDuration(selectedGame.uptimeSeconds) }}</dd></div>
                <div>
                  <dt>在线玩家</dt>
                  <dd v-if="selectedGame.playersAvailable">
                    {{ selectedGame.playersOnline }} /
                    {{ selectedGame.playersMaxKnown ? selectedGame.playersMax : "上限未知" }}
                  </dd>
                  <dd v-else>暂不可用</dd>
                </div>
                <div><dt>人数来源</dt><dd>{{ selectedGame.playersSource || "未知" }}</dd></div>
                <div><dt>游戏端口</dt><dd>{{ selectedGame.port }} / UDP</dd></div>
                <div><dt>最近备份</dt><dd>{{ formatRelative(selectedGame.lastBackupAt) }}</dd></div>
                <div>
                  <dt>当前版本</dt>
                  <dd class="version-detail">
                    <span>{{ selectedGame.version }}</span>
                    <small v-if="selectedGame.versionSource">来源：{{ selectedGame.versionSource }}</small>
                    <span class="version-check-row">
                      <small
                        v-if="versionCheckLabel(selectedGame)"
                        :class="{ current: selectedGame.versionCheck === 'current', available: selectedGame.updateAvailable }"
                      >
                        {{ versionCheckLabel(selectedGame) }}
                      </small>
                      <button
                        v-if="selectedGame.versionCheck === 'unchecked' || selectedGame.versionCheck === 'unavailable'"
                        class="version-check-button"
                        :disabled="versionCheckSubmitting || Boolean(selectedGameActivity) || !hasPermission('game.update')"
                        :title="!hasPermission('game.update') ? '需要更新服务端权限' : undefined"
                        @click="requestVersionCheck(selectedGame)"
                      >
                        {{ versionCheckSubmitting ? "提交中…" : "检查新版本" }}
                      </button>
                    </span>
                  </dd>
                </div>
                <div><dt>进程状态</dt><dd class="good">{{ stateLabel(selectedGame.state) }}</dd></div>
                <div><dt>REST 管理</dt><dd :class="selectedGame.restAvailable ? 'good' : ''">{{ selectedGame.restAvailable ? "已连接" : selectedGame.restEnabled ? "已启用但不可用" : "未启用" }}</dd></div>
              </dl>
              <section
                v-if="selectedGame.state === 'running' && selectedGame.playersAvailable"
                class="online-player-panel"
              >
                <div class="online-player-heading">
                  <div>
                    <strong>在线玩家</strong>
                    <span>仅展示游戏昵称，不显示平台账号与玩家 ID</span>
                  </div>
                  <em>{{ selectedGame.playersOnline }} 人</em>
                </div>
                <div v-if="selectedGame.players?.length" class="online-player-list">
                  <span v-for="(player, index) in selectedGame.players" :key="`${player.name}-${index}`">
                    <Users :size="13" />{{ player.name }}
                  </span>
                </div>
                <p v-else-if="selectedGame.playersOnline === 0" class="online-player-empty">
                  当前没有玩家在线
                </p>
                <p v-else class="online-player-empty">
                  在线人数可用，但玩家名单接口暂不可用
                </p>
              </section>
            </article>
            <article class="panel-card quick-card">
              <div class="card-heading">
                <div><h2>快捷操作</h2><p>安全任务会自动排队</p></div>
                <CloudCog :size="19" />
              </div>
              <button
                v-if="selectedGame.backupSupported"
                :disabled="!canRunSafeAction(selectedGame, 'backup')"
                :title="actionDisabledReason(selectedGame, 'backup')"
                @click="askAction(selectedGame, 'backup')"
              >
                <DatabaseBackup :size="18" />
                <span><strong>创建备份</strong><small>存档与关键配置</small></span>
                <ChevronRight :size="17" />
              </button>
              <button
                v-if="selectedGame.updateSupported"
                :disabled="!canRunSafeAction(selectedGame, 'update')"
                :title="actionDisabledReason(selectedGame, 'update')"
                @click="askAction(selectedGame, 'update')"
              >
                <RefreshCw :size="18" />
                <span><strong>安全更新服务端</strong><small>保存、停服、备份、SteamCMD 更新</small></span>
                <ChevronRight :size="17" />
              </button>
              <button
                v-if="canOpenPalworldSettings && selectedGame.id === 'palworld'"
                @click="navigate('settings', 'palworld')"
              >
                <Settings2 :size="18" />
                <span><strong>编辑服务器配置</strong><small>Palworld 1.0 参数</small></span>
                <ChevronRight :size="17" />
              </button>
              <button
                v-if="canOpenPalworldMods && selectedGame.id === 'palworld'"
                @click="navigate('mods', 'palworld')"
              >
                <PackageSearch :size="18" />
                <span><strong>查看官方模组</strong><small>已安装清单与兼容性检查</small></span>
                <ChevronRight :size="17" />
              </button>
              <button
                v-if="canOpenDSTSettings && selectedGame.id === 'dont-starve-together'"
                @click="navigate('settings', 'dont-starve-together')"
              >
                <Settings2 :size="18" />
                <span><strong>编辑服务器配置</strong><small>DST cluster / Master / Caves</small></span>
                <ChevronRight :size="17" />
              </button>
              <button v-if="isAdmin" @click="navigate('logs')">
                <TerminalSquare :size="18" />
                <span><strong>查看任务日志</strong><small>面板、帕鲁启动与 SteamCMD</small></span>
                <ChevronRight :size="17" />
              </button>
            </article>
          </section>
        </template>

        <template v-else-if="page === 'mods' && isAdmin">
          <section class="settings-header mods-header">
            <div>
              <span class="eyebrow">PALWORLD · OFFICIAL MODS</span>
              <h1>帕鲁官方模组</h1>
              <p>读取 PalServer 默认 Workshop 目录与 PalModSettings.ini；当前阶段只检查，不改动任何模组文件。</p>
            </div>
            <button class="button secondary" :disabled="modsLoading" @click="loadModInventory">
              <RefreshCw :size="16" :class="{ spin: modsLoading }" />重新扫描
            </button>
          </section>

          <article class="settings-source-warning panel-card mods-readonly-notice">
            <ShieldCheck :size="18" />
            <div>
              <strong>只读验收阶段</strong>
              <span>这里暂不提供上传、安装、启用、停用或删除；Hearth 不会自动停止服务器，也不会执行模组中的内容。</span>
            </div>
          </article>

          <div v-if="modsLoading && !modInventory" class="page-loader">
            <LoaderCircle class="spin" :size="22" />正在扫描官方模组目录…
          </div>

          <article v-else-if="modsError" class="settings-source-warning panel-card danger mods-load-error">
            <AlertTriangle :size="18" />
            <div>
              <strong>模组清单读取失败</strong>
              <span>{{ modsError }}</span>
            </div>
            <button class="button secondary" :disabled="modsLoading" @click="loadModInventory">重试</button>
          </article>

          <template v-else-if="modInventory">
            <section class="mod-summary-grid" aria-label="模组清单摘要">
              <article class="panel-card">
                <span>已识别模组</span>
                <strong>{{ modInventory.mods.length }}</strong>
                <small>默认 Workshop 目录</small>
              </article>
              <article class="panel-card">
                <span>当前启用</span>
                <strong>{{ enabledModCount }}</strong>
                <small>由 PalModSettings.ini 判断</small>
              </article>
              <article class="panel-card">
                <span>上次扫描</span>
                <strong class="summary-time">{{ formatDateTime(modInventory.scannedAt) }}</strong>
                <small>修订 {{ modInventory.revision.slice(0, 12) }}</small>
              </article>
            </section>

            <article v-if="modInventory.warnings.length" class="panel-card mod-inventory-warnings">
              <div class="card-heading">
                <div><h2>目录检查提示</h2><p>其余合法模组仍会继续展示</p></div>
                <AlertTriangle :size="19" />
              </div>
              <ul>
                <li v-for="warning in modInventory.warnings" :key="warning">{{ warning }}</li>
              </ul>
            </article>

            <section v-if="modInventory.mods.length === 0" class="panel-card mods-empty-state">
              <span class="mods-empty-icon"><PackageOpen :size="31" /></span>
              <h2>未发现已安装模组</h2>
              <p>当前 PalServer 的 <code>Mods\Workshop</code> 默认目录中没有可识别的官方模组。</p>
              <small>这属于正常状态，不影响服务器运行。后续安装能力上线前仍需按 Palworld 官方流程手动部署。</small>
              <button class="button secondary" :disabled="modsLoading" @click="loadModInventory">
                <RefreshCw :size="15" :class="{ spin: modsLoading }" />再次扫描
              </button>
            </section>

            <section v-else class="mod-card-grid">
              <article v-for="mod in modInventory.mods" :key="mod.id" class="panel-card mod-card">
                <header>
                  <span class="mod-package-icon"><PackageOpen :size="20" /></span>
                  <div>
                    <h2>{{ mod.name }}</h2>
                    <code>{{ mod.id }}</code>
                  </div>
                  <span :class="['mod-enabled-chip', { enabled: mod.enabled }]">
                    {{ mod.enabled ? "已启用" : "未启用" }}
                  </span>
                </header>
                <div class="mod-chip-row">
                  <span :class="['mod-compatibility-chip', mod.compatibility]">
                    {{ modCompatibilityLabel(mod.compatibility) }}
                  </span>
                  <span>{{ modSourceLabel(mod.source) }}</span>
                  <span>{{ mod.ownership === "external" ? "外部文件" : "Hearth 管理" }}</span>
                </div>
                <dl>
                  <div><dt>版本</dt><dd>{{ mod.version || "未声明" }}</dd></div>
                  <div><dt>来源目录</dt><dd><code>{{ mod.sourceReference }}</code></dd></div>
                  <div>
                    <dt>依赖</dt>
                    <dd>{{ mod.dependencies.length ? mod.dependencies.join("、") : "未声明" }}</dd>
                  </div>
                </dl>
                <ul v-if="mod.warnings.length" class="mod-card-warnings">
                  <li v-for="warning in mod.warnings" :key="warning"><AlertTriangle :size="13" />{{ warning }}</li>
                </ul>
              </article>
            </section>
          </template>
        </template>

        <template v-else-if="page === 'system' && isAdmin">
          <section class="settings-header system-header">
            <div>
              <span class="eyebrow">HEARTH · ADMIN ONLY</span>
              <h1>系统设置与游戏管理</h1>
              <p>探测始终只读；接管和安装只会在管理员明确确认后执行。</p>
            </div>
            <button v-if="systemTab === 'games'" class="button secondary" :disabled="managementLoading" @click="refreshDiscovery">
              <FolderSearch :size="16" :class="{ spin: managementLoading }" />重新探测
            </button>
          </section>

          <section class="access-tabs system-tabs" aria-label="系统设置分页">
            <button :class="{ active: systemTab === 'games' }" @click="systemTab = 'games'">
              <Gamepad2 :size="16" />游戏管理
            </button>
            <button :class="{ active: systemTab === 'settings' }" @click="systemTab = 'settings'">
              <Settings2 :size="16" />后台设置
            </button>
            <button :class="{ active: systemTab === 'updates' }" @click="systemTab = 'updates'; refreshPanelUpdateOnEntry()">
              <Download :size="16" />面板更新
            </button>
          </section>

          <div v-if="managementLoading && !management" class="page-loader">
            <LoaderCircle class="spin" :size="22" />正在读取游戏管理状态…
          </div>

          <section v-else-if="management && systemTab === 'games'" class="management-grid">
            <article
              v-for="game in management.games"
              :key="game.id"
              class="panel-card management-game-card"
            >
              <header>
                <div class="management-monogram">{{ game.shortName }}</div>
                <div>
                  <span :class="['management-state', game.state]">{{ managementStateLabel(game.state) }}</span>
                  <h2>{{ game.name }}</h2>
                  <p>{{ game.detail }}</p>
                </div>
                <span v-if="game.support === 'planned'" class="planned-chip">1.3.0</span>
              </header>

              <div v-if="managedGameActivity(game)" class="management-task-progress">
                <div>
                  <span>{{ managedGameActivity(game)?.stage || "处理中" }}</span>
                  <strong>{{ managedGameActivity(game)?.progress ?? 0 }}%</strong>
                </div>
                <div class="activity-progress-track">
                  <i :style="{ width: `${managedGameActivity(game)?.progress ?? 0}%` }"></i>
                </div>
                <small>{{ managedGameActivity(game)?.detail }}</small>
              </div>

              <div v-if="game.state === 'managed'" class="managed-paths">
                <label><span>游戏目录</span><code>{{ game.installDir }}</code></label>
                <label v-if="game.clusterDir"><span>Cluster 目录</span><code>{{ game.clusterDir }}</code></label>
                <label><span>SteamCMD</span><code>{{ game.steamCmd }}</code></label>
              </div>

              <form
                v-if="game.id === 'dont-starve-together' && game.state === 'managed'"
                class="install-form token-form"
                @submit.prevent="saveDSTToken(game)"
              >
                <div class="install-form-head">
                  <KeyRound :size="18" />
                  <div><strong>DST 集群 Token</strong><span>{{ game.clusterTokenConfigured ? "已配置（内容不会回显）" : "尚未配置，启动前必须写入" }}</span></div>
                </div>
                <label>
                  <span>Cluster Token</span>
                  <input v-model="dstTokenInput" type="password" autocomplete="new-password" placeholder="输入 Klei cluster_token" />
                  <small>仅管理员可操作；Hearth 只写入 <code>cluster_token.txt</code>，不会保存到面板配置、接口响应或审计记录。更新前请先停止 DST。</small>
                </label>
                <button class="button secondary" :disabled="managementSaving || !dstTokenInput.trim()">
                  <LoaderCircle v-if="managementSaving" class="spin" :size="16" />
                  <Save v-else :size="16" />
                  保存 Token
                </button>
              </form>

              <div v-if="game.candidates?.length" class="candidate-list">
                <strong>只读探测结果</strong>
                <div v-for="candidate in game.candidates" :key="candidate.id" class="candidate-row">
                  <div>
                    <code>{{ candidate.installDir }}</code>
                    <code v-if="candidate.clusterDir">Cluster：{{ candidate.clusterDir }}</code>
                    <span>{{ candidate.detail }}</span>
                  </div>
                  <button
                    v-if="game.support === 'available' && game.state !== 'managed'"
                    class="button secondary small"
                    :disabled="managementSaving || !candidate.canAdopt"
                    :title="game.id === 'dont-starve-together' ? (!candidate.clusterDir ? '缺少有效 cluster 目录（cluster.ini、Master/Caves server.ini）' : !candidate.canAdopt ? '需要完整 DST 服务端和 cluster 配置' : '确认后只保存管理路径') : (!candidate.settingsPresent ? '缺少 PalWorldSettings.ini，不会自动创建' : !candidate.steamCmd ? '未找到 steamcmd.exe' : !candidate.canAdopt ? '仅支持 SteamCMD 标准目录' : '确认后只保存管理路径')"
                    @click="adoptManagedGame(game, candidate.id)"
                  >确认接管</button>
                </div>
              </div>

              <form
                v-if="game.canInstall && game.state !== 'managed'"
                class="install-form"
                @submit.prevent="installManagedGame(game)"
              >
                <div class="install-form-head">
                  <Download :size="18" />
                  <div><strong>安装新服务器</strong><span>安装完成后保持停止，不会自动开放防火墙。</span></div>
                </div>
                <label>
                  <span>SteamCMD 目录</span>
                  <input v-model="installSteamCmdRoot" autocomplete="off" placeholder="C:\SteamCMD" />
                  <small>
                    目录中已有 steamcmd.exe 时直接使用；空目录会从 Valve 官方地址下载。
                    {{ game.id === "dont-starve-together" ? "DST" : "Palworld" }} 固定安装到
                    <code>{{ game.id === "dont-starve-together" ? steamCmdDSTPath : steamCmdPalworldPath }}</code>。
                  </small>
                </label>
                <template v-if="game.id === 'dont-starve-together'">
                  <label>
                    <span>全新集群目录</span>
                    <input v-model="dstInstallClusterDir" autocomplete="off" placeholder="C:\Users\Administrator\Documents\Klei\DoNotStarveTogether\HearthCluster" />
                    <small>必须是父目录已存在、但自身尚不存在的绝对路径。现有集群请使用上方“确认接管”，安装不会覆盖或合并存档。</small>
                  </label>
                  <label>
                    <span>服务器名称</span>
                    <input v-model="dstInstallClusterName" maxlength="128" autocomplete="off" />
                  </label>
                  <label>
                    <span>Cluster Token（可选）</span>
                    <input v-model="dstInstallClusterToken" type="password" maxlength="4096" autocomplete="new-password" placeholder="可稍后在托管卡片中配置" />
                    <small>只会写入新集群的 <code>cluster_token.txt</code>，不会保存到 Hearth 配置、接口响应或审计记录。</small>
                  </label>
                </template>
                <label class="install-consent">
                  <input v-model="installConsent" type="checkbox" />
                  <span>
                    我确认联网下载，并向 SteamCMD 目录及其 steamapps 子目录写入文件；
                    <template v-if="game.id === 'dont-starve-together'">同时在上述全新路径创建 Master/Caves 集群配置。</template>
                  </span>
                </label>
                <button
                  class="button primary"
                  :disabled="managementSaving || game.state === 'installing' || !installSteamCmdRoot || !installConsent || (game.id === 'dont-starve-together' && (!dstInstallClusterDir.trim() || !dstInstallClusterName.trim()))"
                >
                  <LoaderCircle v-if="game.state === 'installing'" class="spin" :size="16" />
                  <Download v-else :size="16" />
                  {{ game.state === "installing" ? "安装中…" : "确认并开始安装" }}
                </button>
              </form>

              <div v-if="game.support === 'planned'" class="planned-note">
                <Clock3 :size="17" />当前只展示探测结果，不提供安装或接管，避免产生无法管理的半成品服务器。
              </div>
            </article>
          </section>

          <form
            v-else-if="management && systemTab === 'settings'"
            class="panel-card system-settings-form"
            @submit.prevent="saveSystemSettings"
          >
            <div class="system-settings-section">
              <div><h2>安装与探测</h2><p>启动时只读取这些有边界的目录，不扫描整块磁盘。</p></div>
              <label><span>默认游戏探测根目录</span><input v-model="management.settings.installRoot" autocomplete="off" /><small>用于发现已有游戏；新安装仍使用 SteamCMD 的标准 steamapps 目录。</small></label>
              <label><span>默认 SteamCMD 目录</span><input v-model="management.settings.steamCmdRoot" autocomplete="off" /></label>
              <label class="wide"><span>额外探测根目录</span><textarea v-model="discoveryRootsText" rows="4"></textarea><small>每行一个绝对路径，最多向下探测 5 层。</small></label>
            </div>
            <div class="system-settings-section">
              <div><h2>游戏服务器运维</h2><p>备份保留与 SteamCMD 超时同时用于 Palworld 和 DST；保存后需重启 Hearth，正在运行的游戏不会被自动重启。</p></div>
              <label><span>备份保留天数</span><input v-model.number="management.settings.backupRetentionDays" type="number" min="1" max="36500" /></label>
              <label><span>备份容量上限 GiB</span><input v-model.number="management.settings.backupMaxTotalGB" type="number" min="1" max="1000000" /></label>
              <label><span>关闭等待秒数</span><input v-model.number="management.settings.shutdownWaitSeconds" type="number" min="5" max="600" /><small>Palworld 用于安全关闭；DST 用于等待 Master/Caves 退出。</small></label>
              <label><span>SteamCMD 无进展超时</span><input v-model.number="management.settings.steamCmdNoProgressMinutes" type="number" min="1" max="10080" /></label>
              <label><span>Palworld 游戏端口</span><input v-model.number="management.settings.palworldPort" type="number" min="1" max="65535" /></label>
            </div>
            <div class="system-settings-section">
              <div><h2>面板网络边界</h2><p>修改后需要重启 Hearth；错误代理范围可能影响登录来源判断。</p></div>
              <label class="toggle-setting"><input v-model="management.settings.secureCookies" type="checkbox" /><span>强制 Secure Cookie</span></label>
              <label><span>面板更新通道</span><select v-model="management.settings.updateChannel"><option value="stable">Stable（推荐）</option><option value="prerelease">Prerelease</option></select><small>切换后重启 Hearth 生效；任何通道都不会静默安装。</small></label>
              <label class="wide"><span>可信代理 CIDR</span><textarea v-model="trustedProxyCIDRsText" rows="4"></textarea><small>每行一个最小范围；留空表示不信任任何代理，不要使用 0.0.0.0/0。</small></label>
            </div>
            <footer class="system-settings-actions">
              <div><AlertTriangle :size="16" />保存会保留一份 <code>config.json.previous</code>，不会自动重启面板或游戏。</div>
              <button class="button primary" :disabled="managementSaving">
                <LoaderCircle v-if="managementSaving" class="spin" :size="16" />
                <Save v-else :size="16" />保存后台设置
              </button>
            </footer>
          </form>

          <section
            v-else-if="systemTab === 'updates'"
            class="panel-card panel-update-card"
          >
            <header class="panel-update-heading">
              <div class="panel-update-icon"><Download :size="22" /></div>
              <div>
                <span class="eyebrow">HEARTH PANEL · WINDOWS</span>
                <h2>面板安全更新</h2>
                <p>只替换 Hearth 程序；不会停止游戏服务器，也不会修改存档或游戏配置。</p>
              </div>
              <span v-if="panelUpdate" :class="['panel-update-state', panelUpdate.state]">{{ panelUpdate.stage }}</span>
            </header>

            <div v-if="!panelUpdate" class="page-loader">
              <LoaderCircle class="spin" :size="22" />正在读取面板更新状态…
            </div>
            <template v-else>
              <div class="panel-update-facts">
                <label><span>当前版本</span><strong>v{{ panelUpdate.currentVersion }}</strong></label>
                <label><span>最新版本</span><strong>{{ panelUpdate.latestVersion ? `v${panelUpdate.latestVersion}` : "尚未检查" }}</strong></label>
                <label><span>更新通道</span><strong>{{ panelUpdate.channel === "prerelease" ? "Prerelease" : "Stable" }}</strong></label>
              </div>

              <div v-if="panelUpdate.state === 'checking' || panelUpdate.state === 'preparing'" class="panel-update-progress">
                <div><span>{{ panelUpdate.stage }}</span><strong>{{ panelUpdate.progress }}%</strong></div>
                <div class="activity-progress-track"><i :style="{ width: `${panelUpdate.progress}%` }"></i></div>
              </div>

              <div :class="['panel-update-message', panelUpdate.state]">
                <ShieldCheck v-if="panelUpdate.state === 'ready' || panelUpdate.state === 'succeeded'" :size="18" />
                <AlertTriangle v-else-if="panelUpdate.state === 'failed' || panelUpdate.state === 'rolled_back'" :size="18" />
                <CloudCog v-else :size="18" />
                <span>
                  <strong>{{ panelUpdate.message || "更新由管理员手动检查和确认，不会静默安装。" }}</strong>
                  <small>下载源固定为 <code>nikumatane/Hearth</code>，安装前校验 Release 摘要、SHA256 与包内版本。</small>
                  <small v-if="!panelUpdate.canApply">当前系统只支持检查版本；面板内安装与自动回滚目前仅支持 Windows。</small>
                </span>
              </div>

              <footer class="panel-update-actions">
                <small v-if="panelUpdate.checkedAt">上次检查：{{ formatDateTime(panelUpdate.checkedAt) }}</small>
                <span v-else></span>
                <button class="button secondary" :disabled="panelUpdateBusy || panelUpdate.state === 'checking' || panelUpdate.state === 'preparing'" @click="checkPanelUpdate">
                  <LoaderCircle v-if="panelUpdateBusy && panelUpdate.state !== 'preparing'" class="spin" :size="16" />
                  <RefreshCw v-else :size="16" />检查更新
                </button>
                <button
                  class="button primary"
                  :disabled="panelUpdateBusy || panelUpdate.state === 'checking' || panelUpdate.state === 'preparing' || !panelUpdate.updateAvailable || !panelUpdate.canApply"
                  @click="applyPanelUpdate"
                >
                  <LoaderCircle v-if="panelUpdate.state === 'preparing'" class="spin" :size="16" />
                  <Download v-else :size="16" />{{ panelUpdate.state === "preparing" ? "准备更新…" : "确认并安装" }}
                </button>
              </footer>
            </template>
          </section>
        </template>

        <template v-else-if="page === 'access' && isAdmin">
          <section class="access-header settings-header">
            <div>
              <span class="eyebrow">ACCESS CONTROL · PASSWORD ONLY</span>
              <h1>访问权限</h1>
              <p>
                安装时设置的密码是管理员凭据；成员通过独立密码进入，不需要用户名。
                密码只保存加盐摘要，审计记录不会保存密码明文。
              </p>
            </div>
            <button class="button secondary" :disabled="accessLoading" @click="loadAccess">
              <RefreshCw :size="16" :class="{ spin: accessLoading }" />刷新
            </button>
          </section>

          <section class="access-tabs" aria-label="权限管理分页">
            <button :class="{ active: accessTab === 'members' }" @click="accessTab = 'members'">
              <Users :size="17" />
              成员密码
              <span>{{ members.length }}</span>
            </button>
            <button :class="{ active: accessTab === 'audit' }" @click="accessTab = 'audit'">
              <History :size="17" />
              登录与攻击
              <span>{{ auditEntries.length }}</span>
            </button>
            <button
              :class="{ active: accessTab === 'operation-audit' }"
              @click="accessTab = 'operation-audit'"
            >
              <ShieldCheck :size="17" />
              安全操作审计
              <span>{{ operationAuditEntries.length }}</span>
            </button>
            <button
              :class="{ active: accessTab === 'config-audit' }"
              @click="accessTab = 'config-audit'"
            >
              <SlidersHorizontal :size="17" />
              参数审计
              <span>{{ configAuditEntries.length }}</span>
            </button>
            <button :class="{ active: accessTab === 'ip-rules' }" @click="accessTab = 'ip-rules'">
              <ShieldCheck :size="17" />
              IP 黑白名单
              <span>{{ ipRules.length }}</span>
            </button>
          </section>

          <div
            v-if="accessLoading && members.length === 0 && auditEntries.length === 0 && operationAuditEntries.length === 0 && configAuditEntries.length === 0 && ipRules.length === 0"
            class="page-loader"
          >
            <LoaderCircle class="spin" :size="22" />正在读取权限数据…
          </div>

          <section v-else-if="accessTab === 'members'" class="access-layout">
            <article class="panel-card access-create-card">
              <div class="card-heading">
                <div>
                  <h2>添加成员密码</h2>
                  <p>无需用户名，登录后按自动编号区分成员。</p>
                </div>
                <UserPlus :size="19" />
              </div>
              <form class="member-create-form" @submit.prevent="addMember">
                <div class="input-with-icon">
                  <KeyRound :size="17" />
                  <input
                    v-model="newMemberPassword"
                    type="password"
                    autocomplete="new-password"
                    :minlength="minimumPasswordLength"
                    maxlength="256"
                    :placeholder="`至少 ${minimumPasswordLength} 个字符`"
                  />
                </div>
                <div class="permission-presets" aria-label="权限模板">
                  <button
                    v-for="preset in permissionPresets"
                    :key="preset.id"
                    type="button"
                    :class="{ active: presetSelected(newMemberPermissions, preset.permissions) }"
                    @click="applyPermissionPreset('new', preset.permissions)"
                  >
                    {{ preset.label }}
                  </button>
                </div>
                <div class="permission-grid">
                  <label v-for="permission in permissionDefinitions" :key="permission.id">
                    <input
                      type="checkbox"
                      :checked="newMemberPermissions.includes(permission.id)"
                      @change="toggleMemberPermission(
                        'new',
                        permission.id,
                        ($event.target as HTMLInputElement).checked
                      )"
                    />
                    <span>
                      <strong>{{ permission.label }}</strong>
                      <small>{{ permission.description }}</small>
                    </span>
                  </label>
                </div>
                <button
                  class="button primary"
                  :disabled="passwordCharacterCount(newMemberPassword) < minimumPasswordLength"
                >
                  <UserPlus :size="16" />添加成员
                </button>
              </form>
              <div class="access-note">
                <ShieldCheck :size="17" />
                <span>模板只是快捷选择，最终以后端勾选项为准。任务日志、三类审计和成员管理始终只对管理员开放。</span>
              </div>
            </article>

            <article class="panel-card member-list-card">
              <div class="card-heading">
                <div>
                  <h2>成员凭据</h2>
                  <p>修改密码或权限后，该成员现有登录会话会立即退出。</p>
                </div>
                <span>最多 20 个</span>
              </div>
              <div v-if="members.length" class="member-list">
                <div v-for="member in members" :key="member.id" class="member-row">
                  <div class="member-identity">
                    <span><KeyRound :size="17" /></span>
                    <div>
                      <strong>{{ member.id }}</strong>
                      <small>
                        创建 {{ formatDateTime(member.createdAt) }} ·
                        最近登录 {{ formatRelative(member.lastUsedAt) }}
                      </small>
                      <div class="member-permission-chips">
                        <span v-if="!member.permissions?.length">只读</span>
                        <span v-for="permission in member.permissions" :key="permission">
                          {{ permissionLabel(permission) }}
                        </span>
                      </div>
                    </div>
                  </div>
                  <form
                    v-if="editingMemberId === member.id"
                    class="member-inline-form member-permission-editor"
                    @submit.prevent="saveMember(member)"
                  >
                    <div class="member-editor-top">
                      <input
                        v-model="editingMemberPassword"
                        type="password"
                        autocomplete="new-password"
                        maxlength="256"
                        placeholder="新密码（留空表示不修改）"
                        autofocus
                      />
                      <button
                        class="button primary small"
                        :disabled="
                          editingMemberPassword.length > 0 &&
                          passwordCharacterCount(editingMemberPassword) < minimumPasswordLength
                        "
                      >
                        <Save :size="14" />保存修改
                      </button>
                      <button type="button" class="button secondary small" @click="editingMemberId = ''">
                        取消
                      </button>
                    </div>
                    <div class="permission-presets compact" aria-label="权限模板">
                      <button
                        v-for="preset in permissionPresets"
                        :key="preset.id"
                        type="button"
                        :class="{ active: presetSelected(editingMemberPermissions, preset.permissions) }"
                        @click="applyPermissionPreset('edit', preset.permissions)"
                      >
                        {{ preset.label }}
                      </button>
                    </div>
                    <div class="permission-grid compact">
                      <label v-for="permission in permissionDefinitions" :key="permission.id">
                        <input
                          type="checkbox"
                          :checked="editingMemberPermissions.includes(permission.id)"
                          @change="toggleMemberPermission(
                            'edit',
                            permission.id,
                            ($event.target as HTMLInputElement).checked
                          )"
                        />
                        <span>
                          <strong>{{ permission.label }}</strong>
                          <small>{{ permission.description }}</small>
                        </span>
                      </label>
                    </div>
                  </form>
                  <div v-else-if="deletingMemberId === member.id" class="member-delete-confirm">
                    <span>确认删除？</span>
                    <button class="button danger-ghost small" @click="removeMember(member)">删除</button>
                    <button class="button secondary small" @click="deletingMemberId = ''">取消</button>
                  </div>
                  <div v-else class="member-actions">
                    <button class="button secondary small" @click="beginMemberEdit(member)">
                      <KeyRound :size="14" />编辑密码与权限
                    </button>
                    <button class="icon-button member-delete-button" title="删除成员密码" @click="deletingMemberId = member.id">
                      <Trash2 :size="16" />
                    </button>
                  </div>
                </div>
              </div>
              <div v-else class="access-empty">
                <Users :size="24" />
                <strong>还没有成员密码</strong>
                <span>添加后，把密码单独发给对应朋友即可。</span>
              </div>
            </article>
          </section>

          <section v-else-if="accessTab === 'audit'" class="panel-card audit-card">
            <div class="card-heading">
              <div>
                <h2>登录与攻击审计</h2>
                <p>只记录登录尝试、限流和黑名单拦截；安全配置操作不会混入这里。</p>
              </div>
              <History :size="19" />
            </div>
            <div v-if="auditEntries.length" class="audit-table-wrap">
              <table class="audit-table">
                <thead>
                  <tr>
                    <th>登录时间</th>
                    <th>登录来源 IP</th>
                    <th>使用凭据</th>
                    <th>结果</th>
                    <th aria-label="操作"></th>
                  </tr>
                </thead>
                <tbody>
                  <tr
                    v-for="entry in auditEntries"
                    :key="entry.id"
                    :class="{ 'attack-row': !!entry.severity }"
                  >
                    <td>{{ formatDateTime(entry.createdAt) }}</td>
                    <td><code>{{ entry.ip || "未知" }}</code></td>
                    <td>{{ auditCredentialLabel(entry) }}</td>
                    <td>
                      <span
                        :class="[
                          'audit-result',
                          entry.success ? 'success' : 'failure',
                          entry.severity || ''
                        ]"
                      >
                        <Check v-if="entry.success" :size="13" />
                        <AlertTriangle v-else :size="13" />
                        {{ auditResultLabel(entry) }}
                        <small v-if="entry.attemptCount">第 {{ entry.attemptCount }} 次</small>
                      </span>
                    </td>
                    <td class="audit-actions-cell">
                      <button
                        class="icon-button"
                        title="IP 快捷操作"
                        @click="auditMenuEntryId = auditMenuEntryId === entry.id ? '' : entry.id"
                      >
                        <MoreHorizontal :size="17" />
                      </button>
                      <div v-if="auditMenuEntryId === entry.id" class="audit-action-menu">
                        <button @click="copyAuditIP(entry)">复制 IP</button>
                        <button class="danger" @click="quickSetIPRule(entry, 'deny')">
                          将该登录来源加入黑名单 24 小时
                        </button>
                        <button @click="quickSetIPRule(entry, 'allow')">
                          将该登录来源加入白名单 7 天
                        </button>
                      </div>
                    </td>
                  </tr>
                </tbody>
              </table>
            </div>
            <div v-else class="access-empty">
              <History :size="24" />
              <strong>还没有登录审计记录</strong>
              <span>下一次登录尝试会显示在这里。</span>
            </div>
          </section>

          <section v-else-if="accessTab === 'operation-audit'" class="panel-card audit-card">
            <div class="card-heading">
              <div>
                <h2>安全操作审计</h2>
                <p>独立记录成员凭据与 IP 规则变更，明确区分操作者来源和操作目标。</p>
              </div>
              <ShieldCheck :size="19" />
            </div>
            <div v-if="operationAuditEntries.length" class="audit-table-wrap">
              <table class="audit-table operation-audit-table">
                <thead>
                  <tr>
                    <th>操作时间</th>
                    <th>操作者</th>
                    <th>操作来源 IP</th>
                    <th>操作内容</th>
                    <th>操作目标</th>
                    <th>结果</th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-for="entry in operationAuditEntries" :key="entry.id">
                    <td>{{ formatDateTime(entry.createdAt) }}</td>
                    <td>{{ operationActorLabel(entry) }}</td>
                    <td><code>{{ entry.actorIp || "未知" }}</code></td>
                    <td>
                      <strong>{{ operationActionLabel(entry) }}</strong>
                      <small>{{ operationDetailLabel(entry) }}</small>
                    </td>
                    <td><code>{{ operationTargetLabel(entry) }}</code></td>
                    <td>
                      <span :class="['audit-result', entry.success ? 'success' : 'failure']">
                        <Check v-if="entry.success" :size="13" />
                        <AlertTriangle v-else :size="13" />
                        {{ entry.success ? "成功" : "失败" }}
                      </span>
                    </td>
                  </tr>
                </tbody>
              </table>
            </div>
            <div v-else class="access-empty">
              <ShieldCheck :size="24" />
              <strong>还没有安全操作记录</strong>
              <span>下一次成功修改成员凭据、IP 规则、游戏接管/安装或后台设置后会显示在这里。</span>
            </div>
          </section>

          <section v-else-if="accessTab === 'config-audit'" class="panel-card audit-card">
            <div class="card-heading">
              <div>
                <h2>帕鲁参数审计</h2>
                <p>保留最近 1000 次成功的 INI 保存记录；文件达到 5 MiB 后自动轮转。</p>
              </div>
              <SlidersHorizontal :size="19" />
            </div>
            <div v-if="configAuditEntries.length" class="audit-table-wrap">
              <table class="audit-table config-audit-table">
                <thead>
                  <tr>
                    <th>保存时间</th>
                    <th>操作来源</th>
                    <th>参数变更</th>
                    <th>配置来源</th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-for="entry in configAuditEntries" :key="entry.id">
                    <td>{{ formatDateTime(entry.createdAt) }}</td>
                    <td>
                      <strong>{{ configAuditCredentialLabel(entry) }}</strong>
                      <code>{{ entry.ip || "未知 IP" }}</code>
                    </td>
                    <td>
                      <div class="config-audit-changes">
                        <div v-for="change in entry.changes" :key="change.key">
                          <strong>{{ change.label }}</strong>
                          <code>{{ change.key }}</code>
                          <span :class="{ sensitive: change.sensitive }">
                            {{ configAuditChangeLabel(change) }}
                          </span>
                        </div>
                      </div>
                    </td>
                    <td>
                      <span>{{ entry.source }}</span>
                      <small>
                        {{ entry.revisionBefore.slice(0, 8) }} →
                        {{ entry.revisionAfter.slice(0, 8) }}
                      </small>
                    </td>
                  </tr>
                </tbody>
              </table>
            </div>
            <div v-else class="access-empty">
              <SlidersHorizontal :size="24" />
              <strong>还没有参数审计记录</strong>
              <span>下一次成功保存 PalWorldSettings.ini 后会显示在这里。</span>
            </div>
          </section>

          <section v-else class="ip-rules-layout">
            <article class="panel-card ip-rule-create-card">
              <div class="card-heading">
                <div>
                  <h2>添加 IP 规则</h2>
                  <p>当前仅支持单个精确 IP，避免误封整段网络。</p>
                </div>
                <ShieldCheck :size="19" />
              </div>
              <form class="ip-rule-form" @submit.prevent="addIPRule">
                <label>
                  <span>IP 地址</span>
                  <input v-model="newRuleIP" placeholder="例如 203.0.113.8" />
                </label>
                <div class="ip-rule-kind">
                  <button
                    type="button"
                    :class="{ active: newRuleKind === 'deny', danger: newRuleKind === 'deny' }"
                    @click="newRuleKind = 'deny'; newRuleHours = 24"
                  >
                    黑名单 · 拒绝登录
                  </button>
                  <button
                    type="button"
                    :class="{ active: newRuleKind === 'allow' }"
                    @click="newRuleKind = 'allow'; newRuleHours = 168"
                  >
                    白名单 · 独立限流
                  </button>
                </div>
                <label>
                  <span>有效期（小时）</span>
                  <input
                    v-model.number="newRuleHours"
                    type="number"
                    min="1"
                    max="8760"
                    :disabled="newRulePermanent"
                  />
                </label>
                <label class="ip-rule-permanent">
                  <input v-model="newRulePermanent" type="checkbox" />
                  <span>永久生效</span>
                </label>
                <label>
                  <span>备注（可选）</span>
                  <input v-model="newRuleNote" maxlength="200" placeholder="添加原因或负责人" />
                </label>
                <button class="button primary" :disabled="savingIPRule || !newRuleIP.trim()">
                  <LoaderCircle v-if="savingIPRule" class="spin" :size="16" />
                  <ShieldCheck v-else :size="16" />
                  保存规则
                </button>
              </form>
              <div class="access-note">
                <ShieldCheck :size="17" />
                <span>白名单只使用保留的登录校验通道，不会跳过密码；可信设备 Cookie 也不会创建登录会话。</span>
              </div>
            </article>

            <article class="panel-card ip-rule-list-card">
              <div class="card-heading">
                <div>
                  <h2>现有规则</h2>
                  <p>过期规则自动失效；本机和可信代理地址不能被加入名单。</p>
                </div>
                <span>{{ ipRules.length }} 条</span>
              </div>
              <div v-if="ipRules.length" class="ip-rule-list">
                <div v-for="rule in ipRules" :key="rule.id" class="ip-rule-row">
                  <span :class="['ip-rule-badge', rule.kind]">
                    {{ rule.kind === "deny" ? "黑名单" : "白名单" }}
                  </span>
                  <div>
                    <code>{{ rule.ip }}</code>
                    <small>
                      {{ ruleExpiryLabel(rule) }} · 命中 {{ rule.hitCount }} 次
                      <template v-if="rule.note"> · {{ rule.note }}</template>
                    </small>
                  </div>
                  <button
                    class="icon-button member-delete-button"
                    title="删除 IP 规则"
                    :disabled="deletingIPRuleId === rule.id"
                    @click="removeIPRule(rule)"
                  >
                    <LoaderCircle v-if="deletingIPRuleId === rule.id" class="spin" :size="16" />
                    <Trash2 v-else :size="16" />
                  </button>
                </div>
              </div>
              <div v-else class="access-empty">
                <ShieldCheck :size="24" />
                <strong>还没有 IP 规则</strong>
                <span>可以从登录审计的三点菜单快速添加。</span>
              </div>
            </article>
          </section>
        </template>

        <template v-else-if="page === 'logs' && isAdmin">
          <section class="logs-header settings-header">
            <div>
              <span class="eyebrow">OPERATIONS · WINDOWS ECS</span>
              <h1>任务日志</h1>
              <p>持久化操作记录、面板运行日志，以及帕鲁和饥荒联机版的任务输出。</p>
            </div>
            <button class="button secondary" :disabled="logsLoading" @click="loadLogs(false)">
              <RefreshCw :size="16" :class="{ spin: logsLoading }" />刷新日志
            </button>
          </section>

          <div v-if="logsLoading && !logs" class="page-loader">
            <LoaderCircle class="spin" :size="22" />正在读取日志…
          </div>
          <section v-else-if="logs" class="logs-layout">
            <article class="panel-card logs-activity-card">
              <div class="card-heading">
                <div><h2>操作记录</h2><p>跨面板重启保留的最近管理任务</p></div>
                <span>{{ logs.activities.length }} 条</span>
              </div>
              <div class="activity-list">
                <div v-for="item in logs.activities" :key="item.id" class="activity-item">
                  <span :class="['activity-icon', item.status]">
                    <LoaderCircle v-if="item.status === 'running'" class="spin" :size="16" />
                    <AlertTriangle v-else-if="item.status === 'warning' || item.status === 'error'" :size="16" />
                    <Check v-else-if="item.status === 'success'" :size="16" />
                    <Activity v-else :size="16" />
                  </span>
                  <div class="activity-copy">
                    <strong>{{ item.title }}</strong>
                    <span>{{ item.detail }}</span>
                    <div v-if="item.status === 'running'" class="activity-progress-row">
                      <div class="activity-progress-track">
                        <i :style="{ width: `${activityProgress(item)}%` }"></i>
                      </div>
                      <small>{{ item.stage || "执行中" }} · {{ activityProgress(item) }}%</small>
                    </div>
                  </div>
                  <div class="activity-tail">
                    <button
                      v-if="item.logs?.length"
                      class="activity-log-button"
                      type="button"
                      @click="openActivityLogs(item)"
                    >
                      <TerminalSquare :size="13" />查看日志
                    </button>
                    <time>{{ formatRelative(item.updatedAt || item.createdAt) }}</time>
                  </div>
                </div>
                <div v-if="logs.activities.length === 0" class="log-empty">暂时没有操作记录</div>
              </div>
            </article>

            <article class="panel-card log-viewer-card">
              <div class="log-tabs">
                <div
                  v-for="file in logs.files"
                  :key="file.id"
                  :class="['log-tab', { active: selectedLog?.id === file.id }]"
                >
                  <button type="button" class="log-tab-select" @click="selectLog(file.id)">
                    <TerminalSquare :size="14" />
                    <span>{{ file.label }}</span>
                  </button>
                  <button
                    v-if="file.id !== 'panel'"
                    type="button"
                    class="log-tab-close"
                    title="关闭日志"
                    @click.stop="closeLog(file.id)"
                  ><X :size="13" /></button>
                </div>
              </div>
              <div v-if="selectedLog" class="log-viewer">
                <div class="log-viewer-head">
                  <div>
                    <strong>{{ selectedLog.label }}</strong>
                    <span>更新于 {{ formatRelative(selectedLog.updatedAt) }}</span>
                  </div>
                  <span v-if="selectedLog.truncated">仅显示末尾 128 KB</span>
                </div>
                <pre>{{ selectedLog.content || "日志文件目前为空" }}</pre>
              </div>
              <div v-else class="log-empty">暂时没有可读取的日志文件</div>
            </article>
          </section>
        </template>

        <template v-else-if="page === 'settings'">
          <template v-if="settingsGameId === 'dont-starve-together'">
            <section class="settings-header">
              <div>
                <span class="eyebrow">DST · {{ dstSettingsMode === "server" ? "SERVER SETTINGS" : dstSettingsMode === "world" ? "WORLD RULES" : "ADVANCED FILES" }}</span>
                <h1>饥荒联机版配置</h1>
                <p v-if="dstSettingsMode === 'server'">服务器参数按功能分类；只保存明确修改的键，并保留 INI 中的未知参数与注释。</p>
                <p v-else-if="dstSettingsMode === 'world'">分别配置地表与洞穴的常用生成规则；只修改白名单键，不执行 Lua。</p>
                <p v-else>高级模式直接编辑三个 INI 与两个 worldgenoverride.lua 文件，适用于模组或尚未功能化的参数。</p>
              </div>
              <div class="settings-header-actions">
                <span v-if="dstActiveDirty" class="dirty-indicator"><i></i> 有未保存修改</span>
                <button class="button secondary" :disabled="dstConfigLoading" @click="loadDSTConfig">
                  <RefreshCw :size="16" :class="{ spin: dstConfigLoading }" /> 重新载入
                </button>
                <button
                  class="button primary"
                  :disabled="dstConfigSaving || !dstActiveDirty || dstGame?.state !== 'stopped'"
                  @click="saveActiveDSTSettings"
                >
                  <LoaderCircle v-if="dstConfigSaving" class="spin" :size="16" />
                  <Save v-else :size="16" />
                  {{ dstConfigSaving ? "保存中…" : "保存配置" }}
                </button>
              </div>
            </section>

            <div class="settings-source-tabs dst-source-tabs" role="tablist" aria-label="DST 配置模式">
              <button :class="{ active: dstSettingsMode === 'server' }" role="tab" @click="dstSettingsMode = 'server'">
                <strong>服务器配置</strong>
                <span>服务器、玩法、维护与分片端口</span>
              </button>
              <button :class="{ active: dstSettingsMode === 'world' }" :disabled="!dstWorldSettings" :title="dstWorldSourceError" role="tab" @click="dstSettingsMode = 'world'">
                <strong>世界规则</strong>
                <span>{{ dstWorldSourceError ? "文件含高级语法，请使用高级模式" : "地表与洞穴的常用生成选项" }}</span>
              </button>
              <button :class="{ active: dstSettingsMode === 'advanced' }" role="tab" @click="dstSettingsMode = 'advanced'">
                <strong>高级文件</strong>
                <span>INI · Master/Caves worldgenoverride.lua</span>
              </button>
            </div>

            <div v-if="dstConfigLoading && (!dstConfig || !dstSettings || !dstWorldSettings)" class="page-loader">
              <LoaderCircle class="spin" :size="22" /> 正在读取 DST 配置…
            </div>
            <section v-else-if="dstSettingsMode === 'server' && dstSettings" class="settings-layout">
              <aside class="settings-nav panel-card">
                <nav>
                  <button
                    v-for="group in dstSettings.groups"
                    :key="group.id"
                    :class="{ active: dstSettingsGroup === group.id }"
                    @click="dstSettingsGroup = group.id"
                  >
                    <span>{{ group.label }}</span>
                    <small>{{ group.settings.length }}</small>
                  </button>
                </nav>
                <div class="settings-version">
                  <ShieldCheck :size="17" />
                  <div><strong>结构版本 {{ dstSettings.version }}</strong><span>最后修改 {{ formatRelative(dstSettings.lastModified) }}</span></div>
                </div>
              </aside>
              <div class="settings-main">
                <article v-if="dstGame?.state !== 'stopped'" class="settings-source-warning panel-card">
                  <AlertTriangle :size="18" />
                  <div><strong>服务器运行中，暂时不能保存配置</strong><span>可以先调整参数；保存前请停止 Master/Caves。</span></div>
                </article>
                <article
                  v-for="group in dstSettings.groups.filter((item) => item.id === dstSettingsGroup)"
                  :key="group.id"
                  class="settings-group panel-card"
                >
                  <div class="settings-group-head">
                    <div><h2>{{ group.label }}</h2><p>{{ group.description }}</p></div>
                    <span>{{ group.settings.length }} 项</span>
                  </div>
                  <div class="setting-list">
                    <div v-for="setting in group.settings" :key="setting.key" class="setting-row">
                      <div class="setting-copy">
                        <div>
                          <strong>{{ setting.label }}</strong>
                          <span class="source-chip">{{ setting.configured ? "当前文件" : "默认值" }}</span>
                          <span v-if="setting.risk" :class="['risk-chip', setting.risk]">
                            <AlertTriangle :size="12" />
                            {{ setting.risk === "disk" ? "增加磁盘写入" : "关键配置" }}
                          </span>
                        </div>
                        <p>{{ setting.description }}</p>
                        <code>{{ setting.key }}</code>
                      </div>
                      <div class="setting-control">
                        <label v-if="setting.type === 'boolean'" class="switch">
                          <input type="checkbox" :checked="Boolean(setting.value)" @change="setDSTSettingValue(setting, ($event.target as HTMLInputElement).checked)" />
                          <span></span>
                        </label>
                        <div v-else-if="setting.type === 'number'" class="number-control">
                          <input type="number" :value="Number(setting.value)" :min="setting.min" :max="setting.max" :step="setting.step" @input="setDSTSettingValue(setting, Number(($event.target as HTMLInputElement).value))" />
                          <small v-if="setting.min !== undefined && setting.max !== undefined">{{ setting.min }}–{{ setting.max }}</small>
                        </div>
                        <select v-else-if="setting.type === 'select'" :value="String(setting.value)" @change="setDSTSettingValue(setting, ($event.target as HTMLSelectElement).value)">
                          <option v-for="option in setting.options ?? []" :key="option.value" :value="option.value">{{ option.label }}</option>
                        </select>
                        <input v-else :type="setting.type === 'password' ? 'password' : 'text'" :value="String(setting.value)" autocomplete="off" @input="setDSTSettingValue(setting, ($event.target as HTMLInputElement).value)" />
                        <button class="reset-button" title="恢复默认值" @click="resetDSTSetting(setting)"><RotateCw :size="14" /></button>
                      </div>
                    </div>
                  </div>
                </article>
              </div>
            </section>
            <section v-else-if="dstSettingsMode === 'world' && dstWorldSettings && activeDSTWorldShard" class="settings-layout">
              <aside class="settings-nav panel-card">
                <div class="dst-world-shards" role="tablist" aria-label="DST 世界分片">
                  <button
                    v-for="shard in dstWorldSettings.shards"
                    :key="shard.id"
                    :class="{ active: dstWorldShard === shard.id }"
                    role="tab"
                    @click="selectDSTWorldShard(shard.id)"
                  >
                    <span>{{ shard.name }}</span>
                    <small>{{ shard.configured ? "已接管" : "新文件" }}</small>
                  </button>
                </div>
                <nav>
                  <button
                    v-for="group in activeDSTWorldShard.groups"
                    :key="group.id"
                    :class="{ active: dstWorldGroup === group.id }"
                    @click="dstWorldGroup = group.id"
                  >
                    <span>{{ group.label }}</span>
                    <small>{{ group.settings.length }}</small>
                  </button>
                </nav>
                <div class="settings-version">
                  <ShieldCheck :size="17" />
                  <div>
                    <strong>{{ activeDSTWorldShard.preset || "未指定预设" }}</strong>
                    <span>{{ activeDSTWorldShard.configured ? "读取现有 worldgenoverride.lua" : "保存时创建安全模板" }}</span>
                  </div>
                </div>
              </aside>
              <div class="settings-main">
                <article class="settings-source-warning panel-card danger">
                  <AlertTriangle :size="18" />
                  <div>
                    <strong>世界生成规则不会改造当前存档</strong>
                    <span>Hearth 只保存 worldgenoverride.lua，不会删除或重新生成世界；这些选项通常需要创建新世界后才生效。</span>
                  </div>
                </article>
                <article v-if="dstGame?.state !== 'stopped'" class="settings-source-warning panel-card">
                  <AlertTriangle :size="18" />
                  <div><strong>服务器运行中，暂时不能保存世界规则</strong><span>可以先调整选项；保存前请停止 Master/Caves。</span></div>
                </article>
                <article
                  v-for="group in activeDSTWorldShard.groups.filter((item) => item.id === dstWorldGroup)"
                  :key="group.id"
                  class="settings-group panel-card"
                >
                  <div class="settings-group-head">
                    <div><h2>{{ group.label }}</h2><p>{{ group.description }}</p></div>
                    <span>{{ group.settings.length }} 项 · 新世界生效</span>
                  </div>
                  <div class="setting-list">
                    <div v-for="setting in group.settings" :key="setting.key" class="setting-row">
                      <div class="setting-copy">
                        <div>
                          <strong>{{ setting.label }}</strong>
                          <span class="source-chip">{{ setting.configured ? "当前文件" : "默认值" }}</span>
                          <span class="risk-chip worldgen"><AlertTriangle :size="12" />重新生成世界生效</span>
                        </div>
                        <p>{{ setting.description }}</p>
                        <code>{{ setting.key }}</code>
                      </div>
                      <div class="setting-control">
                        <select :value="String(setting.value)" @change="setDSTWorldSettingValue(setting, ($event.target as HTMLSelectElement).value)">
                          <option v-for="option in setting.options ?? []" :key="option.value" :value="option.value">{{ option.label }}</option>
                        </select>
                        <button class="reset-button" title="恢复默认值" @click="resetDSTWorldSetting(setting)"><RotateCw :size="14" /></button>
                      </div>
                    </div>
                  </div>
                </article>
              </div>
            </section>
            <section v-else-if="dstSettingsMode === 'advanced' && dstConfig" class="settings-layout dst-config-layout">
              <aside class="settings-nav panel-card">
                <nav>
                  <button
                    v-for="file in dstConfig.files"
                    :key="file.id"
                    :class="{ active: dstConfigActiveFile === file.id }"
                    @click="dstConfigActiveFile = file.id"
                  >
                    <span>{{ file.name }}</span>
                    <small>{{ Math.ceil((dstConfigDrafts[file.id]?.length ?? 0) / 1024) }} KiB</small>
                  </button>
                </nav>
                <div class="settings-version">
                  <ShieldCheck :size="17" />
                  <div><strong>修订 {{ dstConfig.revision.slice(0, 12) }}</strong><span>最后修改 {{ formatRelative(dstConfig.lastModified) }}</span></div>
                </div>
              </aside>
              <div class="settings-main">
                <article v-if="dstGame?.state !== 'stopped'" class="settings-source-warning panel-card">
                  <AlertTriangle :size="18" />
                  <div><strong>服务器运行中，暂时不能写入 DST 配置</strong><span>可以先编辑文本；保存前请停止 Master/Caves。配置保存后需要重新启动分片。</span></div>
                </article>
                <article class="settings-group panel-card">
                  <div class="settings-group-head">
                    <div>
                      <h2>{{ dstConfig.files.find((file) => file.id === dstConfigActiveFile)?.name }}</h2>
                      <p v-if="dstConfig.files.find((file) => file.id === dstConfigActiveFile)?.format === 'worldgen'">只接受声明式静态 Lua table；不会执行文件内容，并保留支持语法内的未知键和注释。</p>
                      <p v-else>保留未知键和注释；服务端只接受固定文件名和有效 INI 文本。</p>
                    </div>
                    <span>管理员专属</span>
                  </div>
                  <textarea class="dst-config-editor" :value="dstConfigDrafts[dstConfigActiveFile] ?? ''" spellcheck="false" @input="updateDSTConfigDraft(($event.target as HTMLTextAreaElement).value)"></textarea>
                </article>
              </div>
            </section>
          </template>
          <template v-else>
          <section class="settings-header">
            <div>
              <span class="eyebrow">PALWORLD · {{ settingsSource === "world" ? "WORLDOPTION.SAV" : "PALWORLDSETTINGS.INI" }}</span>
              <h1>帕鲁服务器配置</h1>
              <p v-if="settingsSource === 'world'">
                当前存档 {{ parsedWorldOption?.document.worldId || palworldGame?.saveId || "未识别" }}；
                只读取和写入 WorldOption.sav，不会自动同步 INI。
              </p>
              <p v-else>
                Windows 专服配置 PalWorldSettings.ini；只保存明确修改的参数，不会改动当前世界存档。
              </p>
            </div>
            <div class="settings-header-actions">
              <span v-if="settingsDirty" class="dirty-indicator"><i></i> 有未保存修改</span>
              <button class="button secondary" :disabled="settingsLoading" @click="loadSettings">
                <RefreshCw :size="16" :class="{ spin: settingsLoading }" /> 重新载入
              </button>
              <button
                class="button primary"
                :disabled="settingsSaving || !settingsDirty || palworldGame?.state !== 'stopped' || (settingsSource === 'world' && !!worldRoundTripError)"
                :title="palworldGame?.state !== 'stopped' ? '请先安全停止服务器' : undefined"
                @click="saveSettings"
              >
                <LoaderCircle v-if="settingsSaving" class="spin" :size="16" />
                <Save v-else :size="16" />
                {{ settingsSaving ? "保存中…" : "保存配置" }}
              </button>
            </div>
          </section>

          <div
            :class="['settings-source-tabs', { single: !isAdmin }]"
            role="tablist"
            aria-label="配置来源"
          >
            <button
              v-if="isAdmin"
              :class="{ active: settingsSource === 'world' }"
              :disabled="!worldSettings"
              role="tab"
              @click="selectSettingsSource('world')"
            >
              <strong>WorldOption.sav</strong>
              <span>{{ worldSourceError || "当前世界规则 · 反向解析格式" }}</span>
            </button>
            <button
              :class="{ active: settingsSource === 'ini' }"
              :disabled="!iniSettings"
              role="tab"
              @click="selectSettingsSource('ini')"
            >
              <strong>PalWorldSettings.ini</strong>
              <span>{{ iniSourceError || "Windows 专服配置 · 官方配置入口" }}</span>
            </button>
          </div>

          <div v-if="settingsLoading && !settings" class="page-loader">
            <LoaderCircle class="spin" :size="22" /> 正在读取配置…
          </div>
          <section v-else-if="settings" class="settings-layout">
            <aside class="settings-nav panel-card">
              <div class="settings-search">
                <Search :size="16" />
                <input v-model="settingsSearch" placeholder="搜索参数…" />
              </div>
              <nav>
                <button
                  v-for="group in settings.groups"
                  :key="group.id"
                  :class="{ active: !settingsSearch && settingsGroup === group.id }"
                  @click="settingsSearch = ''; settingsGroup = group.id"
                >
                  <span>{{ group.label }}</span>
                  <small>{{ group.settings.length }}</small>
                </button>
              </nav>
              <div class="settings-version">
                <ShieldCheck :size="17" />
                <div>
                  <strong>{{ settings.version }}</strong>
                  <span>最后修改 {{ formatRelative(settings.lastModified) }}</span>
                </div>
              </div>
            </aside>

            <div class="settings-main">
              <article v-if="!isAdmin" class="settings-source-warning panel-card member-scope">
                <ShieldCheck :size="18" />
                <div>
                  <strong>当前为成员玩法参数权限</strong>
                  <span>这里只展示管理员开放的日常玩法参数；系统、安全和 WorldOption.sav 仍为管理员专属，成功保存会进入参数审计。</span>
                </div>
              </article>
              <article v-if="settingsSource === 'ini' && restAPIEnabled === false" class="settings-source-warning panel-card">
                <AlertTriangle :size="18" />
                <div>
                  <strong>REST API 未启用，但不影响启动 PalServer.exe</strong>
                  <span>玩家数据以及运行中的安全停止、重启、更新和备份会不可用；需要这些能力时再开启 RESTAPIEnabled。</span>
                </div>
              </article>
              <article v-if="settingsSource === 'world' && worldRoundTripError" class="settings-source-warning panel-card danger">
                <AlertTriangle :size="18" />
                <div>
                  <strong>WorldOption.sav 写入已锁定</strong>
                  <span>{{ worldRoundTripError }}。当前文件仍可查看，但不会写入。</span>
                </div>
              </article>
              <article v-if="palworldGame?.state !== 'stopped'" class="settings-source-warning panel-card">
                <AlertTriangle :size="18" />
                <div>
                  <strong>服务器运行中，暂时不能写入{{ settingsSource === "world" ? " WorldOption.sav" : " PalWorldSettings.ini" }}</strong>
                  <span>可以先调整参数；保存前请返回状态页安全停止服务器。</span>
                </div>
              </article>
              <article
                v-for="group in filteredGroups"
                :key="group.id"
                class="settings-group panel-card"
              >
                  <div class="settings-group-head">
                    <div>
                      <h2>{{ group.label }}</h2>
                      <p>{{ group.description }}</p>
                    </div>
                    <span>{{ group.settings.length }} 项</span>
                  </div>
                  <div class="setting-list">
                    <div v-for="setting in group.settings" :key="setting.key" class="setting-row">
                      <div class="setting-copy">
                        <div>
                          <strong>{{ setting.label }}</strong>
                          <span class="source-chip">{{ settingSourceLabel(setting) }}</span>
                          <span v-if="hasSourceConflict(setting)" class="conflict-chip">
                            两个来源不一致
                          </span>
                          <span v-if="setting.risk" :class="['risk-chip', setting.risk]">
                            <AlertTriangle :size="12" />
                            {{
                              setting.risk === "performance"
                                ? "影响性能"
                                : setting.risk === "disk"
                                  ? "增加磁盘写入"
                                  : "注意安全"
                            }}
                          </span>
                        </div>
                        <p>{{ setting.description }}</p>
                        <code>{{ setting.key }}</code>
                      </div>
                      <div class="setting-control">
                        <label v-if="setting.type === 'boolean'" class="switch">
                          <input
                            type="checkbox"
                            :checked="Boolean(setting.value)"
                            @change="setSettingValue(setting, ($event.target as HTMLInputElement).checked)"
                          />
                          <span></span>
                        </label>
                        <div v-else-if="setting.type === 'number'" class="number-control">
                          <input
                            type="number"
                            :value="Number(setting.value)"
                            :min="setting.min"
                            :max="setting.max"
                            :step="setting.step"
                            @input="setSettingValue(setting, Number(($event.target as HTMLInputElement).value))"
                          />
                          <small v-if="setting.min !== undefined && setting.max !== undefined">
                            {{ setting.min }}–{{ setting.max }}
                          </small>
                        </div>
                        <select
                          v-else-if="setting.type === 'select'"
                          :value="String(setting.value)"
                          @change="setSettingValue(setting, ($event.target as HTMLSelectElement).value)"
                        >
                          <option
                            v-for="option in setting.options ?? []"
                            :key="option.value"
                            :value="option.value"
                          >
                            {{ option.label }}
                          </option>
                        </select>
                        <input
                          v-else
                          :type="setting.type === 'password' ? 'password' : 'text'"
                          :value="String(setting.value)"
                          autocomplete="off"
                          @input="setSettingValue(setting, ($event.target as HTMLInputElement).value)"
                        />
                        <button class="reset-button" title="恢复默认值" @click="resetSetting(setting)">
                          <RotateCw :size="14" />
                        </button>
                      </div>
                    </div>
                  </div>
              </article>
              <div v-if="filteredGroups.length === 0" class="empty-search panel-card">
                <Search :size="24" />
                <strong>没有找到匹配参数</strong>
                <span>尝试搜索官方参数名或中文说明</span>
              </div>
            </div>
          </section>
          </template>
        </template>
      </main>
    </section>

    <Transition name="toast">
      <div v-if="toast" :class="['toast', toast.type]">
        <Check v-if="toast.type === 'success'" :size="18" />
        <AlertTriangle v-else :size="18" />
        {{ toast.message }}
      </div>
    </Transition>

    <Transition name="modal">
      <div v-if="confirmAction" class="modal-backdrop" @click.self="confirmAction = null">
        <section class="confirm-modal">
          <button class="icon-button modal-close" @click="confirmAction = null"><X :size="18" /></button>
          <div :class="['confirm-icon', confirmAction.action]">
            <RefreshCw v-if="confirmAction.action === 'update'" :size="24" />
            <RotateCw v-else-if="confirmAction.action === 'restart'" :size="24" />
            <DatabaseBackup v-else-if="confirmAction.action === 'backup'" :size="24" />
            <Square v-else-if="confirmAction.action === 'stop'" :size="22" />
            <Play v-else :size="24" />
          </div>
          <span class="eyebrow">CONFIRM ACTION</span>
          <h2>确认{{ actionLabel(confirmAction.action) }}{{ confirmAction.game.name }}？</h2>
          <p v-if="confirmAction.action === 'update' && usesUnsafeFallback(confirmAction.game, confirmAction.action)">
            DST 没有 REST 保存与安全关闭通道。面板会终止 Master/Caves，对静止存档创建 ZIP 备份，
            再更新并恢复两个分片；最近尚未由游戏写入磁盘的进度可能丢失。
          </p>
          <p v-else-if="confirmAction.action === 'update'">
            面板会先通知在线玩家、保存世界并安全停止服务器，然后执行 SteamCMD 更新和健康检查。
          </p>
          <p v-else-if="usesUnsafeFallback(confirmAction.game, confirmAction.action)">
            面板仍会先尝试通过 REST API 保存并安全关闭；如果失败，将只终止当前识别到的
            Palworld 进程。最近未自动保存的游戏进度可能丢失。
          </p>
          <p v-else-if="actionAllowsUnsafeFallback(confirmAction.action)">
            面板会先通过 REST API 保存世界并安全关闭；如果执行期间 REST API 失效，
            本次确认允许面板只终止当前识别到的 Palworld 进程，最近未自动保存的进度可能丢失。
          </p>
          <p v-else>
            操作会进入该游戏的串行任务队列，并在活动记录中保留结果。
          </p>
          <div
            v-if="
              confirmAction.game.playersAvailable &&
              confirmAction.game.playersOnline > 0 &&
              ['stop','restart','update'].includes(confirmAction.action)
            "
            class="modal-warning"
          >
            <Users :size="17" />
            当前有 {{ confirmAction.game.playersOnline }} 名玩家在线
          </div>
          <div
            v-if="usesUnsafeFallback(confirmAction.game, confirmAction.action)"
            class="modal-warning danger"
          >
            <AlertTriangle :size="17" />
            REST API 当前不可用；已明确确认后才会启用强制停止回退
          </div>
          <div class="modal-actions">
            <button class="button secondary" :disabled="actionBusy" @click="confirmAction = null">取消</button>
            <button class="button primary" :disabled="actionBusy" @click="executeAction">
              <LoaderCircle v-if="actionBusy" class="spin" :size="16" />
              {{
                actionBusy
                  ? "正在提交…"
                  : usesUnsafeFallback(confirmAction.game, confirmAction.action)
                    ? `确认强制${actionLabel(confirmAction.action)}`
                    : `确认${actionLabel(confirmAction.action)}`
              }}
            </button>
          </div>
        </section>
      </div>
    </Transition>
  </div>
</template>

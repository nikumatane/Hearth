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
  Flame,
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
  type Game,
  type LoginAuditEntry,
  type Logs,
  type MemberCredential,
  type Overview,
  type PalworldSettings,
  type Setting
} from "./api";
import Sparkline from "./components/Sparkline.vue";
import {
  encodeWorldOption,
  parseWorldOption,
  worldOptionSettings,
  type ParsedWorldOption
} from "./worldOptionCodec";

type Page = "overview" | "game" | "settings" | "logs" | "access";
type ActionName = "start" | "stop" | "restart" | "update" | "backup";

const booting = ref(true);
const loggedIn = ref(false);
const loginPassword = ref("");
const loginError = ref("");
const loggingIn = ref(false);
const overview = ref<Overview | null>(null);
const page = ref<Page>("overview");
const selectedGameId = ref("palworld");
const mobileNavOpen = ref(false);
const loading = ref(false);
const loadError = ref("");
const confirmAction = ref<{ game: Game; action: ActionName } | null>(null);
const actionBusy = ref(false);
const toast = ref<{ type: "success" | "error"; message: string } | null>(null);
const settings = ref<PalworldSettings | null>(null);
const settingsLoading = ref(false);
const settingsSaving = ref(false);
const settingsSearch = ref("");
const settingsGroup = ref("server");
const settingsDirty = ref(false);
const parsedWorldOption = ref<ParsedWorldOption | null>(null);
const logs = ref<Logs | null>(null);
const logsLoading = ref(false);
const selectedLogId = ref("");
const nodeMenuOpen = ref(false);
const gameMenuOpen = ref(false);
const buildVersion = ref("未知");
const currentRole = ref<"admin" | "member" | "">("");
const credentialId = ref("");
const members = ref<MemberCredential[]>([]);
const auditEntries = ref<LoginAuditEntry[]>([]);
const accessLoading = ref(false);
const accessTab = ref<"members" | "audit">("members");
const newMemberPassword = ref("");
const editingMemberId = ref("");
const editingMemberPassword = ref("");
const deletingMemberId = ref("");
let pollTimer: number | undefined;
let toastTimer: number | undefined;
let refreshInFlight = false;

const selectedGame = computed(() =>
  overview.value?.games.find((game) => game.id === selectedGameId.value)
);

const runningCount = computed(
  () => overview.value?.games.filter((game) => game.state === "running").length ?? 0
);

const palworldGame = computed(() =>
  overview.value?.games.find((game) => game.id === "palworld")
);

const playerSummary = computed(() => {
  const games = overview.value?.games ?? [];
  const max = games.reduce((sum, game) => sum + game.playersMax, 0);
  const unavailable = games.some(
    (game) => game.state === "running" && !game.playersAvailable
  );
  if (unavailable) {
    return { online: null as number | null, max, source: "REST API 暂不可用" };
  }
  const online = games.reduce((sum, game) => sum + game.playersOnline, 0);
  const demo = games.some((game) => game.playersSource === "演示数据");
  return {
    online,
    max,
    source: demo ? "演示数据" : "REST API 实时数据"
  };
});

const todayLabel = computed(() =>
  new Intl.DateTimeFormat("zh-CN", {
    month: "long",
    day: "numeric",
    weekday: "long"
  }).format(new Date())
);

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
  if (page.value === "settings") return "帕鲁服务器配置";
  if (page.value === "logs") return "任务日志";
  if (page.value === "access") return "访问权限";
  if (page.value === "game") return selectedGame.value?.name ?? "游戏详情";
  return "服务器总览";
});

const selectedLog = computed(() =>
  logs.value?.files.find((file) => file.id === selectedLogId.value) ?? logs.value?.files[0]
);

const isAdmin = computed(() => currentRole.value === "admin");

onMounted(async () => {
  try {
    const session = await api.session();
    buildVersion.value = session.version || "未知";
    if (session.authenticated) {
      await api.logout().catch(() => undefined);
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
  if (pollTimer) window.clearInterval(pollTimer);
  if (toastTimer) window.clearTimeout(toastTimer);
});

async function login() {
  loginError.value = "";
  loggingIn.value = true;
  try {
    const session = await api.login(loginPassword.value);
    buildVersion.value = session.version || buildVersion.value;
    currentRole.value = session.role ?? "";
    credentialId.value = session.credentialId ?? "";
    loggedIn.value = true;
    loginPassword.value = "";
    await refresh();
    startPolling();
  } catch (error) {
    loginError.value = error instanceof Error ? error.message : "登录失败";
  } finally {
    loggingIn.value = false;
  }
}

async function logout() {
  await api.logout().catch(() => undefined);
  loggedIn.value = false;
  overview.value = null;
  page.value = "overview";
  currentRole.value = "";
  credentialId.value = "";
  members.value = [];
  auditEntries.value = [];
  if (pollTimer) window.clearInterval(pollTimer);
}

function startPolling() {
  if (pollTimer) window.clearInterval(pollTimer);
  pollTimer = window.setInterval(() => void refresh(true), 5000);
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
    loadError.value = "";
  } catch (error) {
    if (isUnauthorized(error)) {
      loggedIn.value = false;
      if (pollTimer) window.clearInterval(pollTimer);
    } else if (!silent) {
      loadError.value = error instanceof Error ? error.message : "无法加载服务器状态";
    }
  } finally {
    refreshInFlight = false;
    loading.value = false;
  }
}

function navigate(next: Page, gameId?: string) {
  if (!isAdmin.value && (next === "settings" || next === "logs" || next === "access")) {
    showToast("error", "仅管理员可以访问该页面");
    return;
  }
  if (gameId) selectedGameId.value = gameId;
  page.value = next;
  mobileNavOpen.value = false;
  nodeMenuOpen.value = false;
  gameMenuOpen.value = false;
  window.scrollTo({ top: 0, behavior: "smooth" });
  if (next === "settings" && !settings.value) void loadSettings();
  if (next === "logs") void loadLogs();
  if (next === "access") void loadAccess();
}

async function loadLogs() {
  logsLoading.value = true;
  try {
    logs.value = await api.logs();
    if (!logs.value.files.some((file) => file.id === selectedLogId.value)) {
      selectedLogId.value = logs.value.files[0]?.id ?? "";
    }
  } catch (error) {
    showToast("error", error instanceof Error ? error.message : "日志加载失败");
  } finally {
    logsLoading.value = false;
  }
}

async function loadAccess() {
  if (!isAdmin.value) return;
  accessLoading.value = true;
  try {
    const [memberResult, auditResult] = await Promise.all([
      api.members(),
      api.loginAudit()
    ]);
    members.value = memberResult.members ?? [];
    auditEntries.value = auditResult.entries ?? [];
  } catch (error) {
    showToast("error", error instanceof Error ? error.message : "权限数据加载失败");
  } finally {
    accessLoading.value = false;
  }
}

async function addMember() {
  if (newMemberPassword.value.length < 10) {
    showToast("error", "成员密码至少需要 10 个字符");
    return;
  }
  try {
    const member = await api.createMember(newMemberPassword.value);
    newMemberPassword.value = "";
    members.value = [member, ...members.value];
    showToast("success", `成员密码 ${member.id} 已添加`);
  } catch (error) {
    showToast("error", error instanceof Error ? error.message : "成员密码添加失败");
  }
}

function beginMemberEdit(member: MemberCredential) {
  editingMemberId.value = member.id;
  editingMemberPassword.value = "";
  deletingMemberId.value = "";
}

async function saveMemberPassword(member: MemberCredential) {
  if (editingMemberPassword.value.length < 10) {
    showToast("error", "成员密码至少需要 10 个字符");
    return;
  }
  try {
    const updated = await api.updateMember(member.id, editingMemberPassword.value);
    members.value = members.value.map((item) => item.id === member.id ? updated : item);
    editingMemberId.value = "";
    editingMemberPassword.value = "";
    showToast("success", `成员密码 ${member.id} 已更新`);
  } catch (error) {
    showToast("error", error instanceof Error ? error.message : "成员密码更新失败");
  }
}

async function removeMember(member: MemberCredential) {
  try {
    await api.deleteMember(member.id);
    members.value = members.value.filter((item) => item.id !== member.id);
    deletingMemberId.value = "";
    showToast("success", `成员密码 ${member.id} 已删除`);
  } catch (error) {
    showToast("error", error instanceof Error ? error.message : "成员密码删除失败");
  }
}

async function loadSettings() {
  settingsLoading.value = true;
  try {
    const document = await api.worldOption();
    parsedWorldOption.value = parseWorldOption(document);
    settings.value = worldOptionSettings(parsedWorldOption.value);
    if (!settings.value.groups.some((group) => group.id === settingsGroup.value)) {
      settingsGroup.value = settings.value.groups[0]?.id ?? "";
    }
    settingsDirty.value = false;
  } catch (error) {
    showToast("error", error instanceof Error ? error.message : "配置加载失败");
  } finally {
    settingsLoading.value = false;
  }
}

function askAction(game: Game, action: ActionName) {
  confirmAction.value = { game, action };
}

async function executeAction() {
  if (!confirmAction.value) return;
  const { game, action } = confirmAction.value;
  actionBusy.value = true;
  try {
    await api.action(game.id, action);
    confirmAction.value = null;
    showToast("success", `${actionLabel(action)}任务已进入执行队列`);
    await refresh(true);
  } catch (error) {
    showToast("error", error instanceof Error ? error.message : "操作失败");
  } finally {
    actionBusy.value = false;
  }
}

async function saveSettings() {
  if (!settings.value || !parsedWorldOption.value) return;
  settingsSaving.value = true;
  try {
    const encoded = encodeWorldOption(parsedWorldOption.value, settings.value);
    const updatedDocument = await api.updateWorldOption({
      ...parsedWorldOption.value.document,
      data: encoded.data,
      management: encoded.management
    });
    parsedWorldOption.value = parseWorldOption(updatedDocument);
    settings.value = worldOptionSettings(parsedWorldOption.value);
    settingsDirty.value = false;
    showToast("success", "WorldOption.sav 已备份并保存；下次启动生效");
  } catch (error) {
    showToast("error", error instanceof Error ? error.message : "配置保存失败");
  } finally {
    settingsSaving.value = false;
  }
}

function setSettingValue(setting: Setting, value: string | number | boolean) {
  setting.value = value;
  settingsDirty.value = true;
}

function resetSetting(setting: Setting) {
  setting.value = setting.default;
  settingsDirty.value = true;
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
  if (entry.credentialId === "INVALID-REQUEST") return "无效登录请求";
  return "未匹配凭据";
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

        <span v-if="isAdmin" class="nav-label nav-section">管理</span>
        <button v-if="isAdmin" :class="{ active: page === 'settings' }" @click="navigate('settings', 'palworld')">
          <SlidersHorizontal :size="18" />
          <span>帕鲁配置</span>
        </button>
        <button v-if="isAdmin" :class="{ active: page === 'logs' }" @click="navigate('logs')">
          <TerminalSquare :size="18" />
          <span>任务日志</span>
        </button>
        <button v-if="isAdmin" :class="{ active: page === 'access' }" @click="navigate('access')">
          <ShieldCheck :size="18" />
          <span>访问权限</span>
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
            <span>{{ page === "overview" ? "控制台" : "游戏服务器" }}</span>
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
              <h1>下午好，{{ isAdmin ? "管理员" : "成员" }}</h1>
              <p>{{ runningCount }} 个服务器正在运行，节点资源处于正常范围。</p>
            </div>
            <button class="button ghost" @click="refresh()">
              <RefreshCw :size="16" :class="{ spin: loading }" />
              刷新状态
            </button>
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
                <em> / {{ playerSummary.max }}</em>
              </strong>
              <div :class="['player-data-status', { unavailable: playerSummary.online === null }]">
                <span></span>{{ playerSummary.source }}
              </div>
              <footer>每 5 秒刷新一次</footer>
            </article>
          </section>

          <section class="section-block">
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
                        {{ game.playersOnline }}<em>/{{ game.playersMax }}</em>
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
                  <div v-if="game.updateAvailable" class="update-callout">
                    <div>
                      <RefreshCw :size="16" />
                      <span><strong>有新版本</strong>{{ game.availableVersion }}</span>
                    </div>
                    <button @click="askAction(game, 'update')">立即更新</button>
                  </div>
                  <div v-else class="version-row">
                    <Check :size="15" />
                    已安装版本 · {{ game.version }}
                  </div>
                  <div class="game-actions">
                    <button
                      v-if="game.state === 'stopped'"
                      class="button primary small"
                      @click="askAction(game, 'start')"
                    >
                      <Play :size="15" /> 启动
                    </button>
                    <button
                      v-else
                      class="button secondary small"
                      :disabled="game.state !== 'running'"
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
                  <div>
                    <strong>{{ item.title }}</strong>
                    <span>{{ item.detail }}</span>
                  </div>
                  <time>{{ formatRelative(item.createdAt) }}</time>
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
                @click="askAction(selectedGame, 'start')"
              >
                <Play :size="16" /> 启动服务器
              </button>
              <template v-else>
                <button
                  class="button secondary"
                  :disabled="selectedGame.state !== 'running'"
                  @click="askAction(selectedGame, 'restart')"
                >
                  <RotateCw :size="16" /> 重启
                </button>
                <button
                  class="button danger-ghost"
                  :disabled="selectedGame.state !== 'running'"
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
                  <button @click="askAction(selectedGame, 'backup'); gameMenuOpen = false">
                    <DatabaseBackup :size="15" />创建备份
                  </button>
                  <button v-if="isAdmin" @click="navigate('settings', 'palworld')">
                    <Settings2 :size="15" />编辑配置
                  </button>
                  <button v-if="isAdmin" @click="navigate('logs')">
                    <TerminalSquare :size="15" />查看日志
                  </button>
                </div>
              </div>
            </div>
          </section>

          <section v-if="selectedGame.updateAvailable" class="detail-update-banner">
            <div class="banner-icon"><RefreshCw :size="20" /></div>
            <div>
              <strong>发现新的服务端版本</strong>
              <span>{{ selectedGame.version }} → {{ selectedGame.availableVersion }}</span>
            </div>
            <button class="button primary small" @click="askAction(selectedGame, 'update')">
              安全更新
            </button>
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
                    {{ selectedGame.playersOnline }} / {{ selectedGame.playersMax }}
                  </dd>
                  <dd v-else>暂不可用</dd>
                </div>
                <div><dt>玩家数据</dt><dd>{{ selectedGame.playersSource || "未知" }}</dd></div>
                <div><dt>游戏端口</dt><dd>{{ selectedGame.port }} / UDP</dd></div>
                <div><dt>最近备份</dt><dd>{{ formatRelative(selectedGame.lastBackupAt) }}</dd></div>
                <div><dt>当前版本</dt><dd>{{ selectedGame.version }}</dd></div>
                <div><dt>进程状态</dt><dd class="good">{{ stateLabel(selectedGame.state) }}</dd></div>
              </dl>
            </article>
            <article class="panel-card quick-card">
              <div class="card-heading">
                <div><h2>快捷操作</h2><p>安全任务会自动排队</p></div>
                <CloudCog :size="19" />
              </div>
              <button @click="askAction(selectedGame, 'backup')">
                <DatabaseBackup :size="18" />
                <span><strong>创建备份</strong><small>存档与关键配置</small></span>
                <ChevronRight :size="17" />
              </button>
              <button @click="askAction(selectedGame, 'update')">
                <RefreshCw :size="18" />
                <span><strong>安全更新服务端</strong><small>保存、停服、备份、SteamCMD 更新</small></span>
                <ChevronRight :size="17" />
              </button>
              <button
                v-if="isAdmin && selectedGame.id === 'palworld'"
                @click="navigate('settings', 'palworld')"
              >
                <Settings2 :size="18" />
                <span><strong>编辑服务器配置</strong><small>Palworld 1.0 参数</small></span>
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
              登录 IP 审计
              <span>{{ auditEntries.length }}</span>
            </button>
          </section>

          <div v-if="accessLoading && members.length === 0 && auditEntries.length === 0" class="page-loader">
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
                    minlength="10"
                    maxlength="256"
                    placeholder="至少 10 个字符"
                  />
                </div>
                <button class="button primary" :disabled="newMemberPassword.length < 10">
                  <UserPlus :size="16" />添加成员
                </button>
              </form>
              <div class="access-note">
                <ShieldCheck :size="17" />
                <span>成员可查看状态并执行启动、停止、重启、更新和备份；不能修改配置或查看管理日志。</span>
              </div>
            </article>

            <article class="panel-card member-list-card">
              <div class="card-heading">
                <div>
                  <h2>成员凭据</h2>
                  <p>修改密码会立即使旧密码失效；现有登录会话持续到退出或过期。</p>
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
                    </div>
                  </div>
                  <form
                    v-if="editingMemberId === member.id"
                    class="member-inline-form"
                    @submit.prevent="saveMemberPassword(member)"
                  >
                    <input
                      v-model="editingMemberPassword"
                      type="password"
                      autocomplete="new-password"
                      minlength="10"
                      maxlength="256"
                      placeholder="输入新的成员密码"
                      autofocus
                    />
                    <button class="button primary small" :disabled="editingMemberPassword.length < 10">
                      <Save :size="14" />保存
                    </button>
                    <button type="button" class="button secondary small" @click="editingMemberId = ''">
                      取消
                    </button>
                  </form>
                  <div v-else-if="deletingMemberId === member.id" class="member-delete-confirm">
                    <span>确认删除？</span>
                    <button class="button danger-ghost small" @click="removeMember(member)">删除</button>
                    <button class="button secondary small" @click="deletingMemberId = ''">取消</button>
                  </div>
                  <div v-else class="member-actions">
                    <button class="button secondary small" @click="beginMemberEdit(member)">
                      <KeyRound :size="14" />修改密码
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

          <section v-else class="panel-card audit-card">
            <div class="card-heading">
              <div>
                <h2>登录源审计</h2>
                <p>保留最近 500 次登录尝试，包含来源 IP、凭据身份、结果与时间。</p>
              </div>
              <History :size="19" />
            </div>
            <div v-if="auditEntries.length" class="audit-table-wrap">
              <table class="audit-table">
                <thead>
                  <tr>
                    <th>登录时间</th>
                    <th>来源 IP</th>
                    <th>使用凭据</th>
                    <th>结果</th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-for="entry in auditEntries" :key="entry.id">
                    <td>{{ formatDateTime(entry.createdAt) }}</td>
                    <td><code>{{ entry.ip || "未知" }}</code></td>
                    <td>{{ auditCredentialLabel(entry) }}</td>
                    <td>
                      <span :class="['audit-result', entry.success ? 'success' : 'failure']">
                        <Check v-if="entry.success" :size="13" />
                        <AlertTriangle v-else :size="13" />
                        {{ entry.success ? "登录成功" : (entry.reason || "登录失败") }}
                      </span>
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
        </template>

        <template v-else-if="page === 'logs' && isAdmin">
          <section class="logs-header settings-header">
            <div>
              <span class="eyebrow">OPERATIONS · WINDOWS ECS</span>
              <h1>任务日志</h1>
              <p>操作记录、面板运行日志、帕鲁启动日志和 SteamCMD 更新输出。</p>
            </div>
            <button class="button secondary" :disabled="logsLoading" @click="loadLogs">
              <RefreshCw :size="16" :class="{ spin: logsLoading }" />刷新日志
            </button>
          </section>

          <div v-if="logsLoading && !logs" class="page-loader">
            <LoaderCircle class="spin" :size="22" />正在读取日志…
          </div>
          <section v-else-if="logs" class="logs-layout">
            <article class="panel-card logs-activity-card">
              <div class="card-heading">
                <div><h2>操作记录</h2><p>本次面板进程中的管理任务</p></div>
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
                  <div><strong>{{ item.title }}</strong><span>{{ item.detail }}</span></div>
                  <time>{{ formatRelative(item.createdAt) }}</time>
                </div>
                <div v-if="logs.activities.length === 0" class="log-empty">暂时没有操作记录</div>
              </div>
            </article>

            <article class="panel-card log-viewer-card">
              <div class="log-tabs">
                <button
                  v-for="file in logs.files"
                  :key="file.id"
                  :class="{ active: selectedLog?.id === file.id }"
                  @click="selectedLogId = file.id"
                >
                  <TerminalSquare :size="14" />
                  <span>{{ file.label }}</span>
                </button>
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
          <section class="settings-header">
            <div>
              <span class="eyebrow">PALWORLD · WORLDOPTION.SAV</span>
              <h1>帕鲁服务器配置</h1>
              <p>
                当前存档 {{ parsedWorldOption?.document.worldId || palworldGame?.saveId || "未识别" }}；
                世界规则按 WorldOption.sav 分类，连接与管理项会同步至 INI。
              </p>
            </div>
            <div class="settings-header-actions">
              <span v-if="settingsDirty" class="dirty-indicator"><i></i> 有未保存修改</span>
              <button class="button secondary" :disabled="settingsLoading" @click="loadSettings">
                <RefreshCw :size="16" :class="{ spin: settingsLoading }" /> 重新载入
              </button>
              <button
                class="button primary"
                :disabled="settingsSaving || !settingsDirty || palworldGame?.state !== 'stopped'"
                :title="palworldGame?.state !== 'stopped' ? '请先安全停止服务器' : undefined"
                @click="saveSettings"
              >
                <LoaderCircle v-if="settingsSaving" class="spin" :size="16" />
                <Save v-else :size="16" />
                {{ settingsSaving ? "保存中…" : "保存配置" }}
              </button>
            </div>
          </section>

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
              <article v-if="palworldGame?.state !== 'stopped'" class="settings-source-warning panel-card">
                <AlertTriangle :size="18" />
                <div>
                  <strong>服务器运行中，暂时不能写入 WorldOption.sav</strong>
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
          <p v-if="confirmAction.action === 'update'">
            面板会先通知在线玩家、保存世界并安全停止服务器，然后执行 SteamCMD 更新和健康检查。
          </p>
          <p v-else-if="confirmAction.action === 'restart'">
            面板会先保存世界并安全停止进程，再重新启动服务器。
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
          <div class="modal-actions">
            <button class="button secondary" :disabled="actionBusy" @click="confirmAction = null">取消</button>
            <button class="button primary" :disabled="actionBusy" @click="executeAction">
              <LoaderCircle v-if="actionBusy" class="spin" :size="16" />
              {{ actionBusy ? "正在提交…" : `确认${actionLabel(confirmAction.action)}` }}
            </button>
          </div>
        </section>
      </div>
    </Transition>
  </div>
</template>

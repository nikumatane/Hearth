export type MetricPoint = {
  at: string;
  value: number;
};

export type Game = {
  id: string;
  name: string;
  shortName: string;
  state: "running" | "stopped" | "starting" | "stopping" | "error";
  version: string;
  availableVersion?: string;
  updateAvailable: boolean;
  versionCheck?: "unchecked" | "checking" | "current" | "update_available" | "unavailable";
  playersOnline: number;
  playersMax: number;
  playersMaxKnown: boolean;
  playersAvailable: boolean;
  playersSource?: string;
  players?: Array<{ name: string }>;
  uptimeSeconds: number;
  cpuPercent: number;
  memoryGB: number;
  port: number;
  saveId?: string;
  saveDetection?: string;
  lastBackupAt?: string;
  cpuHistory: MetricPoint[];
  memoryHistory: MetricPoint[];
  tags: string[];
  restEnabled: boolean;
  restAvailable: boolean;
};

export type Activity = {
  id: string;
  gameId?: string;
  action?: string;
  title: string;
  detail: string;
  status: "success" | "neutral" | "warning" | "running" | "error";
  stage?: string;
  progress?: number;
  createdAt: string;
  updatedAt?: string;
  logs?: LogRef[];
};

export type LogRef = {
  id: string;
  label: string;
};

export type Overview = {
  host: {
    cpuPercent: number;
    memoryPercent: number;
    memoryUsedGB: number;
    memoryTotalGB: number;
    diskPercent: number;
    diskUsedGB: number;
    diskTotalGB: number;
    loadOne: number;
    cpuHistory: MetricPoint[];
    memoryHistory: MetricPoint[];
  };
  games: Game[];
  activities: Activity[];
  updatedAt: string;
};

export type Setting = {
  key: string;
  label: string;
  description: string;
  type: "text" | "password" | "number" | "boolean" | "select";
  value: string | number | boolean;
  default: string | number | boolean;
  min?: number;
  max?: number;
  step?: number;
  options?: Array<{ label: string; value: string }>;
  sensitive?: boolean;
  risk?: "performance" | "disk" | "security" | "";
  memberEditable?: boolean;
  restartRequired: boolean;
  configured: boolean;
};

export type SettingGroup = {
  id: string;
  label: string;
  description: string;
  settings: Setting[];
};

export type PalworldSettings = {
  version: string;
  revision: string;
  groups: SettingGroup[];
  raw: string;
  lastModified: string;
};

export type PalworldSettingsPatch = {
  revision: string;
  changes: Record<string, string | number | boolean>;
};

export type Permission =
  | "game.control"
  | "game.update"
  | "game.backup"
  | "palworld.settings.gameplay";

export type Session = {
  authenticated: boolean;
  name?: string;
  role?: "admin" | "member";
  credentialId?: string;
  permissions?: Permission[];
  version: string;
};

export type MemberCredential = {
  id: string;
  permissions: Permission[];
  createdAt: string;
  updatedAt: string;
  lastUsedAt?: string;
};

export type LoginAuditEntry = {
  id: string;
  ip: string;
  credentialId: string;
  role?: "admin" | "member";
  success: boolean;
  reason?: string;
  event?: "login" | "attack_limited" | "attack_blocked";
  severity?: "warning" | "critical";
  attemptCount?: number;
  knownDevice?: boolean;
  ruleId?: string;
  ruleKind?: "allow" | "deny";
  createdAt: string;
};

export type OperationAuditEntry = {
  id: string;
  event: "member_created" | "member_updated" | "member_deleted" | "ip_rule_added" | "ip_rule_removed" |
    "game_adopted" | "game_install_started" | "system_settings_updated";
  actorCredentialId: string;
  actorRole: "admin" | "member";
  actorIp: string;
  targetType: "member" | "ip_rule" | "game" | "system";
  targetId?: string;
  targetIp?: string;
  ruleKind?: "allow" | "deny";
  expiresAt?: string;
  passwordChanged?: boolean;
  permissionsChanged?: boolean;
  currentPermissions?: Permission[];
  success: boolean;
  createdAt: string;
};

export type IPRule = {
  id: string;
  ip: string;
  kind: "allow" | "deny";
  note?: string;
  createdBy: string;
  createdAt: string;
  expiresAt?: string;
  hitCount: number;
  lastHitAt?: string;
};

export type ConfigAuditChange = {
  key: string;
  label: string;
  before?: string;
  after?: string;
  sensitive?: boolean;
};

export type ConfigAuditEntry = {
  id: string;
  gameId: string;
  source: string;
  credentialId: string;
  role: "admin" | "member";
  ip: string;
  revisionBefore: string;
  revisionAfter: string;
  changes: ConfigAuditChange[];
  createdAt: string;
};

export type WorldOptionDocument = {
  worldId: string;
  revision: string;
  lastModified: string;
  data: string;
};

export type LogFile = {
  id: string;
  label: string;
  updatedAt: string;
  content?: string;
  truncated: boolean;
};

export type Logs = {
  activities: Activity[];
  files: LogFile[];
};

export type GameCandidate = {
  id: string;
  installDir: string;
  steamCmd?: string;
  settingsPresent: boolean;
  canAdopt: boolean;
  detail: string;
};

export type ManagedGame = {
  id: string;
  name: string;
  shortName: string;
  support: "available" | "planned";
  state: "managed" | "detected" | "not_installed" | "installing" | "error";
  detail: string;
  installDir?: string;
  steamCmd?: string;
  canInstall: boolean;
  canAdopt: boolean;
  candidates?: GameCandidate[];
  activeTaskId?: string;
};

export type SystemSettings = {
  revision: string;
  installRoot: string;
  steamCmdRoot: string;
  discoveryRoots: string[];
  backupRetentionDays: number;
  backupMaxTotalGB: number;
  shutdownWaitSeconds: number;
  steamCmdNoProgressMinutes: number;
  palworldPort: number;
  secureCookies: boolean;
  trustedProxyCidrs: string[];
  restartRequired: boolean;
};

export type Management = {
  games: ManagedGame[];
  settings: SystemSettings;
};

class APIError extends Error {
  status: number;

  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, {
    credentials: "same-origin",
    ...init,
    headers: {
      ...(init?.body ? { "Content-Type": "application/json" } : {}),
      ...init?.headers
    }
  });
  if (!response.ok) {
    const body = await response.json().catch(() => ({ error: "请求失败" }));
    throw new APIError(response.status, body.error ?? "请求失败");
  }
  if (response.status === 204) {
    return undefined as T;
  }
  return response.json() as Promise<T>;
}

export const api = {
  session: () => request<Session>("/api/v1/session"),
  login: (password: string) =>
    request<Session>("/api/v1/session", {
      method: "POST",
      body: JSON.stringify({ password })
    }),
  logout: () => request<void>("/api/v1/session", { method: "DELETE" }),
  overview: () => request<Overview>("/api/v1/overview"),
  game: (id: string) => request<Game>(`/api/v1/games/${id}`),
  action: (id: string, action: string, allowUnsafe = false) =>
    request<Activity>(`/api/v1/games/${id}/actions`, {
      method: "POST",
      body: JSON.stringify({ action, allowUnsafe })
    }),
  palworldSettings: () =>
    request<PalworldSettings>("/api/v1/games/palworld/settings"),
  updatePalworldSettings: (patch: PalworldSettingsPatch) =>
    request<PalworldSettings>("/api/v1/games/palworld/settings", {
      method: "PATCH",
      body: JSON.stringify(patch)
    }),
  worldOption: () =>
    request<WorldOptionDocument>("/api/v1/games/palworld/world-option"),
  updateWorldOption: (document: WorldOptionDocument) =>
    request<WorldOptionDocument>("/api/v1/games/palworld/world-option", {
      method: "PUT",
      body: JSON.stringify(document)
    }),
  logs: () => request<Logs>("/api/v1/logs"),
  log: (id: string) => request<LogFile>(`/api/v1/logs/${encodeURIComponent(id)}`),
  members: () =>
    request<{ members: MemberCredential[] }>("/api/v1/access/members"),
  createMember: (password: string, permissions: Permission[]) =>
    request<MemberCredential>("/api/v1/access/members", {
      method: "POST",
      body: JSON.stringify({ password, permissions })
    }),
  updateMember: (
    id: string,
    update: { password?: string; permissions?: Permission[] }
  ) =>
    request<MemberCredential>(`/api/v1/access/members/${encodeURIComponent(id)}`, {
      method: "PATCH",
      body: JSON.stringify(update)
    }),
  deleteMember: (id: string) =>
    request<void>(`/api/v1/access/members/${encodeURIComponent(id)}`, {
      method: "DELETE"
    }),
  loginAudit: () =>
    request<{ entries: LoginAuditEntry[] }>("/api/v1/access/audit"),
  operationAudit: () =>
    request<{ entries: OperationAuditEntry[] }>("/api/v1/access/operation-audit"),
  configAudit: () =>
    request<{ entries: ConfigAuditEntry[] }>("/api/v1/access/config-audit"),
  ipRules: () =>
    request<{ rules: IPRule[] }>("/api/v1/access/ip-rules"),
  createIPRule: (
    ip: string,
    kind: IPRule["kind"],
    options: {
      note?: string;
      expiresInHours?: number;
      permanent?: boolean;
      confirmCurrentIp?: boolean;
    } = {}
  ) =>
    request<IPRule>("/api/v1/access/ip-rules", {
      method: "POST",
      body: JSON.stringify({ ip, kind, ...options })
    }),
  deleteIPRule: (id: string) =>
    request<void>(`/api/v1/access/ip-rules/${encodeURIComponent(id)}`, {
      method: "DELETE"
    }),
  management: () => request<Management>("/api/v1/system/management"),
  refreshDiscovery: () =>
    request<Management>("/api/v1/system/discovery", { method: "POST" }),
  adoptGame: (id: string, candidateId: string) =>
    request<ManagedGame>(`/api/v1/system/games/${encodeURIComponent(id)}/adopt`, {
      method: "POST",
      body: JSON.stringify({ candidateId, confirm: true })
    }),
  installGame: (id: string, steamCmdRoot: string) =>
    request<Activity>(`/api/v1/system/games/${encodeURIComponent(id)}/install`, {
      method: "POST",
      body: JSON.stringify({ steamCmdRoot, confirm: true })
    }),
  updateSystemSettings: (settings: SystemSettings) =>
    request<SystemSettings>("/api/v1/system/settings", {
      method: "PATCH",
      body: JSON.stringify({
        revision: settings.revision,
        installRoot: settings.installRoot,
        steamCmdRoot: settings.steamCmdRoot,
        discoveryRoots: settings.discoveryRoots,
        backupRetentionDays: settings.backupRetentionDays,
        backupMaxTotalGB: settings.backupMaxTotalGB,
        shutdownWaitSeconds: settings.shutdownWaitSeconds,
        steamCmdNoProgressMinutes: settings.steamCmdNoProgressMinutes,
        palworldPort: settings.palworldPort,
        secureCookies: settings.secureCookies,
        trustedProxyCidrs: settings.trustedProxyCidrs
      })
    })
};

export function isUnauthorized(error: unknown): boolean {
  return error instanceof APIError && error.status === 401;
}

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
  playersOnline: number;
  playersMax: number;
  playersAvailable: boolean;
  playersSource?: string;
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
  title: string;
  detail: string;
  status: "success" | "neutral" | "warning" | "running" | "error";
  createdAt: string;
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

export type Session = {
  authenticated: boolean;
  name?: string;
  role?: "admin" | "member";
  credentialId?: string;
  version: string;
};

export type MemberCredential = {
  id: string;
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
  content: string;
  truncated: boolean;
};

export type Logs = {
  activities: Activity[];
  files: LogFile[];
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
  action: (id: string, action: string) =>
    request<Activity>(`/api/v1/games/${id}/actions`, {
      method: "POST",
      body: JSON.stringify({ action })
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
  members: () =>
    request<{ members: MemberCredential[] }>("/api/v1/access/members"),
  createMember: (password: string) =>
    request<MemberCredential>("/api/v1/access/members", {
      method: "POST",
      body: JSON.stringify({ password })
    }),
  updateMember: (id: string, password: string) =>
    request<MemberCredential>(`/api/v1/access/members/${encodeURIComponent(id)}`, {
      method: "PUT",
      body: JSON.stringify({ password })
    }),
  deleteMember: (id: string) =>
    request<void>(`/api/v1/access/members/${encodeURIComponent(id)}`, {
      method: "DELETE"
    }),
  loginAudit: () =>
    request<{ entries: LoginAuditEntry[] }>("/api/v1/access/audit")
};

export function isUnauthorized(error: unknown): boolean {
  return error instanceof APIError && error.status === 401;
}

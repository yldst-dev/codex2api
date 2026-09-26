export type Account = {
  email: string
  account_id: string
  plan_type: string
  status: string
  ready: boolean
  expires_at: string
}

export type PendingLogin = {
  url: string
  callback: boolean
  expires_at: string
  error: string
}

export type ApiKey = {
  id: string
  name: string
  prefix: string
  status: "active" | "revoked"
  created_at: string
  last_used_at?: string
  revoked_at?: string
}

export type CreatedKey = {
  id: string
  name: string
  prefix: string
  key: string
}

export type GatewayState = {
  listen: string
  running: boolean
  locked: boolean
}

export type UpdateStatus = "idle" | "installing" | "restarting" | "requested" | "error"

export type UpdateState = {
  current: string
  supported: boolean
  mode: "systemd" | "self" | ""
  auto: boolean
  status: UpdateStatus
  error: string
  check_error: string
  available: boolean
  checked_at?: string
  latest: { tag: string; url: string; published_at: string } | null
}

export type CodexClientState = {
  version: string
  builtin: string
  checked_at?: string
  error?: string
}

export type Dashboard = {
  authenticated: true
  account: Account | null
  login: PendingLogin | null
  keys: ApiKey[]
  gateway: GatewayState
  admin_listen: string
  admin_allow: string
  data_dir: string
  update: UpdateState
  codex: CodexClientState
}

export type AdminState =
  | { authenticated: false; setup_required: boolean }
  | Dashboard

export class ApiError extends Error {
  readonly status: number

  constructor(message: string, status: number) {
    super(message)
    this.status = status
  }
}

const tokenKey = "codex-gateway-admin"

export function getToken(): string | null {
  try {
    return sessionStorage.getItem(tokenKey)
  } catch {
    return null
  }
}

export function setToken(token: string | null) {
  try {
    if (token) {
      sessionStorage.setItem(tokenKey, token)
    } else {
      sessionStorage.removeItem(tokenKey)
    }
  } catch {
    return
  }
}

async function request<T>(method: "GET" | "POST", path: string, body?: unknown): Promise<T> {
  const headers: Record<string, string> = {}
  const token = getToken()
  if (token) headers.Authorization = `Bearer ${token}`
  if (method === "POST") headers["Content-Type"] = "application/json"
  let res: Response
  try {
    res = await fetch(path, {
      method,
      headers,
      body: method === "POST" ? JSON.stringify(body ?? {}) : undefined,
      credentials: "omit",
      cache: "no-store",
    })
  } catch {
    throw new ApiError("게이트웨이에 연결하지 못했습니다. 서비스가 켜져 있는지 확인해 주세요.", 0)
  }
  const data = await res.json().catch(() => ({}))
  if (!res.ok) {
    if (res.status === 401) setToken(null)
    throw new ApiError(data.error ?? `요청이 실패했습니다 (${res.status})`, res.status)
  }
  return data as T
}

type TokenResponse = { token: string }

export const api = {
  state: () => request<AdminState>("GET", "/api/state"),
  setup: (token: string, password: string) =>
    request<TokenResponse>("POST", "/api/setup", { token, password }),
  login: (password: string) => request<TokenResponse>("POST", "/api/login", { password }),
  logout: () => request<{ ok: boolean }>("POST", "/api/logout"),
  changePassword: (current: string, next: string) =>
    request<TokenResponse>("POST", "/api/password", { current, next }),
  startLogin: () => request<{ login: PendingLogin }>("POST", "/api/oauth/start"),
  submitCallback: (url: string) =>
    request<{ account: Account }>("POST", "/api/oauth/callback", { url }),
  cancelLogin: () => request<{ ok: boolean }>("POST", "/api/oauth/cancel"),
  refreshToken: () => request<{ account: Account }>("POST", "/api/oauth/refresh"),
  disconnect: () => request<{ ok: boolean }>("POST", "/api/oauth/logout"),
  createKey: (name: string) => request<CreatedKey>("POST", "/api/keys", { name }),
  rotateKey: (id: string) =>
    request<CreatedKey>("POST", `/api/keys/${encodeURIComponent(id)}/rotate`),
  revokeKey: (id: string) =>
    request<{ ok: boolean }>("POST", `/api/keys/${encodeURIComponent(id)}/revoke`),
  checkUpdate: () => request<{ update: UpdateState }>("POST", "/api/update/check"),
  applyUpdate: () => request<{ update: UpdateState }>("POST", "/api/update/apply"),
  setAutoUpdate: (enabled: boolean) =>
    request<{ update: UpdateState }>("POST", "/api/update/auto", { enabled }),
  deleteKey: (id: string) =>
    request<{ ok: boolean }>("POST", `/api/keys/${encodeURIComponent(id)}/delete`),
  purgeKeys: () => request<{ deleted: number }>("POST", "/api/keys/purge"),
  setListen: (listen: string) =>
    request<{ listen: string; running: boolean }>("POST", "/api/listen", { listen }),
}

export function errorMessage(err: unknown): string {
  if (err instanceof Error) return err.message
  return "알 수 없는 오류가 났습니다."
}

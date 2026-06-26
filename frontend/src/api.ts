// Typed client for the heartd REST API.

export type NodeStatus = 'ok' | 'down' | 'unknown'

export interface Node {
  name: string
  local: boolean
  status: NodeStatus
}

export interface Metrics {
  cpu_percent: number
  mem_used: number
  mem_total: number
  mem_percent: number
  collected_at: string
}

export interface HistoryPoint {
  cpu_percent: number
  mem_used: number
  mem_total: number
  mem_percent: number
  at: string
}

export type CheckStatus = 'ok' | 'failing' | 'unknown'

export interface Check {
  name: string
  type: 'http' | 'tcp' | 'process' | 'shell'
  status: CheckStatus
  detail: string
  latency_ms: number
  last_checked: string
}

// Registered by the auth gate; invoked whenever a request is rejected with 401
// so the app can drop back to the login screen.
let unauthorizedHandler: (() => void) | null = null
export function setUnauthorizedHandler(fn: (() => void) | null): void {
  unauthorizedHandler = fn
}

async function getJSON<T>(path: string, signal?: AbortSignal): Promise<T> {
  const res = await fetch(path, { signal })
  if (res.status === 401) {
    unauthorizedHandler?.()
    throw new Error('unauthorized')
  }
  if (!res.ok) {
    throw new Error(`request failed: ${res.status}`)
  }
  return (await res.json()) as T
}

async function postJSON<T>(path: string, body: unknown): Promise<T> {
  const res = await fetch(path, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
  const data = await res.json().catch(() => ({}))
  if (!res.ok) {
    throw new Error((data as { error?: string }).error || `request failed: ${res.status}`)
  }
  return data as T
}

async function putJSON<T>(path: string, body: unknown): Promise<T> {
  const res = await fetch(path, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
  const data = await res.json().catch(() => ({}))
  if (!res.ok) {
    throw new Error((data as { error?: string }).error || `request failed: ${res.status}`)
  }
  return data as T
}

async function delJSON<T>(path: string): Promise<T> {
  const res = await fetch(path, { method: 'DELETE' })
  const data = await res.json().catch(() => ({}))
  if (!res.ok) {
    throw new Error((data as { error?: string }).error || `request failed: ${res.status}`)
  }
  return data as T
}

export interface AuthStatus {
  initialized: boolean
  authenticated: boolean
  username?: string
}

export function fetchAuthStatus(): Promise<AuthStatus> {
  return getJSON<AuthStatus>('/api/auth/status')
}

export function authInit(username: string, password: string): Promise<{ username: string }> {
  return postJSON('/api/auth/init', { username, password })
}

export function authLogin(username: string, password: string): Promise<{ username: string }> {
  return postJSON('/api/auth/login', { username, password })
}

export function authLogout(): Promise<{ status: string }> {
  return postJSON('/api/auth/logout', {})
}

// ----- User administration (every user is an admin) -----

export interface UserInfo {
  username: string
  self: boolean
}

export function fetchUsers(signal?: AbortSignal): Promise<UserInfo[]> {
  return getJSON<UserInfo[]>('/api/users', signal)
}

export function createUser(username: string, password: string): Promise<{ username: string }> {
  return postJSON('/api/users', { username, password })
}

export function deleteUser(username: string): Promise<{ status: string }> {
  return delJSON(`/api/users/${encodeURIComponent(username)}`)
}

export function changeUserPassword(username: string, password: string): Promise<{ status: string }> {
  return putJSON(`/api/users/${encodeURIComponent(username)}/password`, { password })
}

export function fetchNodes(signal?: AbortSignal): Promise<Node[]> {
  return getJSON<Node[]>('/api/nodes', signal)
}

// ----- Cluster topology (peer management) -----

export interface PeerInfo {
  name: string
  url: string
  status: NodeStatus
  last_seen: string
  last_error: string
  has_secret: boolean
}

export interface PeerInput {
  name: string
  url: string
  secret: string
}

export function fetchPeers(signal?: AbortSignal): Promise<PeerInfo[]> {
  return getJSON<PeerInfo[]>('/api/peers', signal)
}

export function createPeer(p: PeerInput): Promise<PeerInfo> {
  return postJSON<PeerInfo>('/api/peers', p)
}

// updatePeer sends url and (optionally) a new secret; a blank secret keeps the
// stored one. The name is the identity key and cannot be changed.
export function updatePeer(name: string, url: string, secret: string): Promise<PeerInfo> {
  return putJSON<PeerInfo>(`/api/peers/${encodeURIComponent(name)}`, { url, secret })
}

export function deletePeer(name: string): Promise<{ status: string }> {
  return delJSON<{ status: string }>(`/api/peers/${encodeURIComponent(name)}`)
}

export function fetchMetrics(nodeName: string, signal?: AbortSignal): Promise<Metrics> {
  return getJSON<Metrics>(`/api/nodes/${encodeURIComponent(nodeName)}/metrics`, signal)
}

export function fetchHistory(
  nodeName: string,
  minutes = 60,
  signal?: AbortSignal,
): Promise<HistoryPoint[]> {
  return getJSON<HistoryPoint[]>(
    `/api/nodes/${encodeURIComponent(nodeName)}/metrics/history?minutes=${minutes}`,
    signal,
  )
}

export function fetchChecks(nodeName: string, signal?: AbortSignal): Promise<Check[]> {
  return getJSON<Check[]>(`/api/nodes/${encodeURIComponent(nodeName)}/checks`, signal)
}

export interface DiskMount {
  mount: string
  used: number
  total: number
  percent: number
  at: string
}

export interface NetCurrent {
  recv_bytes: number
  sent_bytes: number
  recv_rate: number
  sent_rate: number
  at: string
}

export interface NetHistoryPoint {
  recv_rate: number
  sent_rate: number
  at: string
}

export function fetchDisk(nodeName: string, signal?: AbortSignal): Promise<DiskMount[]> {
  return getJSON<DiskMount[]>(`/api/nodes/${encodeURIComponent(nodeName)}/disk`, signal)
}

export function fetchNetwork(nodeName: string, signal?: AbortSignal): Promise<NetCurrent | null> {
  return getJSON<NetCurrent | null>(`/api/nodes/${encodeURIComponent(nodeName)}/network`, signal)
}

export function fetchNetworkHistory(
  nodeName: string,
  minutes = 60,
  signal?: AbortSignal,
): Promise<NetHistoryPoint[]> {
  return getJSON<NetHistoryPoint[]>(
    `/api/nodes/${encodeURIComponent(nodeName)}/network/history?minutes=${minutes}`,
    signal,
  )
}

// ----- Settings -----

export interface GeneralSettings {
  metrics_interval_sec: number
  peer_poll_interval_sec: number
  retention_sec: number
  cpu_threshold: number
  mem_threshold: number
  disk_threshold: number
}

export interface EmailNotify {
  enabled: boolean
  smtp_host: string
  smtp_port: number
  username: string
  password: string
  from: string
  to: string[]
  subject_prefix: string
}

export interface WebhookNotify {
  enabled: boolean
  url: string
}

export interface NotifySettings {
  email: EmailNotify
  webhook: WebhookNotify
}

export interface CheckConfig {
  id: number
  name: string
  type: 'http' | 'tcp' | 'process' | 'shell'
  interval_sec: number
  timeout_sec: number
  url: string
  method: string
  host: string
  port: number
  process: string
  command: string
  enabled: boolean
}

export interface AllSettings {
  general: GeneralSettings
  notify: NotifySettings
  checks: CheckConfig[]
}

// Settings are addressed per node. For the local node the backend edits its own
// config; for a peer it proxies the write over the shared-secret peer link. So
// every node's config is editable from any dashboard, while each node still owns
// its own copy (and keeps alerting even when its peers are down).

function settingsBase(nodeName: string): string {
  return `/api/nodes/${encodeURIComponent(nodeName)}/settings`
}

export function fetchSettings(nodeName: string, signal?: AbortSignal): Promise<AllSettings> {
  return getJSON<AllSettings>(settingsBase(nodeName), signal)
}

export function updateGeneral(nodeName: string, g: GeneralSettings): Promise<GeneralSettings> {
  return putJSON<GeneralSettings>(`${settingsBase(nodeName)}/general`, g)
}

export function updateNotify(nodeName: string, n: NotifySettings): Promise<NotifySettings> {
  return putJSON<NotifySettings>(`${settingsBase(nodeName)}/notify`, n)
}

export function createCheck(nodeName: string, c: Omit<CheckConfig, 'id'>): Promise<CheckConfig> {
  return postJSON<CheckConfig>(`${settingsBase(nodeName)}/checks`, c)
}

export function updateCheck(nodeName: string, c: CheckConfig): Promise<{ status: string }> {
  return putJSON<{ status: string }>(`${settingsBase(nodeName)}/checks/${c.id}`, c)
}

export function deleteCheck(nodeName: string, id: number): Promise<{ status: string }> {
  return delJSON<{ status: string }>(`${settingsBase(nodeName)}/checks/${id}`)
}

export function testNotify(nodeName: string, n: NotifySettings): Promise<Record<string, string>> {
  return postJSON<Record<string, string>>(`${settingsBase(nodeName)}/notify/test`, n)
}

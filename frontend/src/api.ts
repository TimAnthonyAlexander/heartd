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

async function getJSON<T>(path: string, signal?: AbortSignal): Promise<T> {
  const res = await fetch(path, { signal })
  if (!res.ok) {
    throw new Error(`request failed: ${res.status}`)
  }
  return (await res.json()) as T
}

export function fetchNodes(signal?: AbortSignal): Promise<Node[]> {
  return getJSON<Node[]>('/api/nodes', signal)
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

// Typed client for the heartd REST API.

export interface Node {
  name: string
  local: boolean
  status: 'ok' | 'failing' | 'unknown'
}

export interface Metrics {
  cpu_percent: number
  mem_used: number
  mem_total: number
  mem_percent: number
  collected_at: string
}

async function getJSON<T>(path: string): Promise<T> {
  const res = await fetch(path)
  if (!res.ok) {
    throw new Error(`request to ${path} failed: ${res.status}`)
  }
  return (await res.json()) as T
}

export function fetchNodes(): Promise<Node[]> {
  return getJSON<Node[]>('/api/nodes')
}

export function fetchMetrics(nodeName: string): Promise<Metrics> {
  return getJSON<Metrics>(`/api/nodes/${encodeURIComponent(nodeName)}/metrics`)
}

export interface HistoryPoint {
  cpu_percent: number
  mem_used: number
  mem_total: number
  mem_percent: number
  at: string
}

export function fetchHistory(nodeName: string, minutes = 60): Promise<HistoryPoint[]> {
  return getJSON<HistoryPoint[]>(
    `/api/nodes/${encodeURIComponent(nodeName)}/metrics/history?minutes=${minutes}`,
  )
}

export interface Check {
  name: string
  type: 'http' | 'tcp' | 'process' | 'shell'
  status: 'ok' | 'failing' | 'unknown'
  detail: string
  latency_ms: number
  last_checked: string
}

export function fetchChecks(nodeName: string): Promise<Check[]> {
  return getJSON<Check[]>(`/api/nodes/${encodeURIComponent(nodeName)}/checks`)
}

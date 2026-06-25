import { useEffect, useState } from 'react'
import {
  fetchChecks,
  fetchHistory,
  fetchMetrics,
  type Check,
  type Metrics,
} from '../api'

const POLL_MS = 3000
const MAX_POINTS = 400

export interface ChartPoint {
  t: number // epoch ms
  cpu: number
  memPct: number
  memUsed: number
  memTotal: number
}

export interface NodeData {
  metrics: Metrics | null
  series: ChartPoint[]
  checks: Check[]
  loading: boolean
  unreachable: boolean
  lastUpdated: number | null
}

const EMPTY: NodeData = {
  metrics: null,
  series: [],
  checks: [],
  loading: true,
  unreachable: false,
  lastUpdated: null,
}

// useNodeData drives the detail view for one node: seeds the time-series from
// persisted history for the chosen range, then polls metrics + checks on a
// self-scheduling timer (no overlap), aborting in-flight requests on change.
export function useNodeData(
  node: string | null,
  rangeMinutes: number,
  paused: boolean,
): NodeData {
  const [data, setData] = useState<NodeData>(EMPTY)

  useEffect(() => {
    if (!node) {
      setData(EMPTY)
      return
    }

    let active = true
    let timer: ReturnType<typeof setTimeout>
    const controller = new AbortController()

    setData({ ...EMPTY, loading: true })

    const seed = async () => {
      try {
        const hist = await fetchHistory(node, rangeMinutes, controller.signal)
        if (!active) return
        const series = hist.map<ChartPoint>((p) => ({
          t: new Date(p.at).getTime(),
          cpu: p.cpu_percent,
          memPct: p.mem_percent,
          memUsed: p.mem_used,
          memTotal: p.mem_total,
        }))
        setData((d) => ({ ...d, series }))
      } catch {
        /* history is best-effort; live polling fills the chart */
      }
    }

    const tick = async () => {
      try {
        const [m, cs] = await Promise.all([
          fetchMetrics(node, controller.signal),
          fetchChecks(node, controller.signal),
        ])
        if (!active) return
        const point: ChartPoint = {
          t: new Date(m.collected_at).getTime(),
          cpu: m.cpu_percent,
          memPct: m.mem_percent,
          memUsed: m.mem_used,
          memTotal: m.mem_total,
        }
        setData((d) => {
          const series = [...d.series, point].slice(-MAX_POINTS)
          return {
            metrics: m,
            series,
            checks: cs,
            loading: false,
            unreachable: false,
            lastUpdated: Date.now(),
          }
        })
      } catch (e) {
        if (!active || (e instanceof DOMException && e.name === 'AbortError')) return
        // Mark unreachable but keep the last data so the UI can dim it.
        setData((d) => ({ ...d, loading: false, unreachable: true }))
      } finally {
        if (active && !paused) timer = setTimeout(tick, POLL_MS)
      }
    }

    seed()
    tick()

    return () => {
      active = false
      clearTimeout(timer)
      controller.abort()
    }
  }, [node, rangeMinutes, paused])

  return data
}

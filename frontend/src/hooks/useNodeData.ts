import { useEffect, useState } from 'react'
import {
  fetchChecks,
  fetchDisk,
  fetchHistory,
  fetchMetrics,
  fetchNetwork,
  fetchNetworkHistory,
  type Check,
  type DiskMount,
  type Metrics,
  type NetCurrent,
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

export interface NetPoint {
  t: number
  recv: number
  sent: number
}

export interface NodeData {
  metrics: Metrics | null
  series: ChartPoint[]
  checks: Check[]
  disk: DiskMount[]
  net: NetCurrent | null
  netSeries: NetPoint[]
  loading: boolean
  unreachable: boolean
  lastUpdated: number | null
}

const EMPTY: NodeData = {
  metrics: null,
  series: [],
  checks: [],
  disk: [],
  net: null,
  netSeries: [],
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
        const [hist, netHist] = await Promise.all([
          fetchHistory(node, rangeMinutes, controller.signal),
          fetchNetworkHistory(node, rangeMinutes, controller.signal),
        ])
        if (!active) return
        const series = hist.map<ChartPoint>((p) => ({
          t: new Date(p.at).getTime(),
          cpu: p.cpu_percent,
          memPct: p.mem_percent,
          memUsed: p.mem_used,
          memTotal: p.mem_total,
        }))
        const netSeries = netHist.map<NetPoint>((p) => ({
          t: new Date(p.at).getTime(),
          recv: p.recv_rate,
          sent: p.sent_rate,
        }))
        setData((d) => ({ ...d, series, netSeries }))
      } catch {
        /* history is best-effort; live polling fills the charts */
      }
    }

    const tick = async () => {
      try {
        const [m, cs, disk, net] = await Promise.all([
          fetchMetrics(node, controller.signal),
          fetchChecks(node, controller.signal),
          fetchDisk(node, controller.signal),
          fetchNetwork(node, controller.signal),
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
          // Skip duplicate points: /metrics returns the latest persisted sample,
          // which only advances every metrics_interval (slower than our poll).
          const lastT = d.series[d.series.length - 1]?.t
          const series = lastT === point.t ? d.series : [...d.series, point].slice(-MAX_POINTS)

          const netT = net ? new Date(net.at).getTime() : null
          const lastNetT = d.netSeries[d.netSeries.length - 1]?.t
          const netSeries =
            net && netT !== lastNetT
              ? [...d.netSeries, { t: netT!, recv: net.recv_rate, sent: net.sent_rate }].slice(-MAX_POINTS)
              : d.netSeries
          return {
            metrics: m,
            series,
            checks: cs,
            disk,
            net,
            netSeries,
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

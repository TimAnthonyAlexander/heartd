import { useEffect, useState } from 'react'
import {
  fetchCPUState,
  fetchCPUStateHistory,
  fetchChecks,
  fetchDisk,
  fetchDiskIO,
  fetchDiskIOHistory,
  fetchHistory,
  fetchMetrics,
  fetchNetwork,
  fetchNetworkHistory,
  fetchProcesses,
  type Check,
  type CPUState,
  type DiskIODevice,
  type DiskMount,
  type Metrics,
  type NetCurrent,
  type ProcessInfo,
} from '../api'
import { resolveWindow, type TimeRange } from '../timerange'

const POLL_MS = 3000
// Cap on live-appended points per series. The server downsamples the seed to a
// bounded count; this bounds the live tail on top of it for long ranges.
const MAX_POINTS = 1000

// appendLive adds a fresh point to a live series, trimming anything that has
// scrolled out of the rolling window and capping the total length.
function appendLive<T extends { t: number }>(arr: T[], pt: T, cutoff: number): T[] {
  return [...arr, pt].filter((p) => p.t >= cutoff).slice(-MAX_POINTS)
}

export interface ChartPoint {
  t: number // epoch ms
  cpu: number
  memPct: number
  memUsed: number
  memTotal: number
  load1: number
  load5: number
  load15: number
}

export interface NetPoint {
  t: number
  recv: number
  sent: number
}

// CPUStatePoint is one CPU-state breakdown at an instant (percentages summing to
// ~100), used by the stacked CPU-breakdown chart.
export interface CPUStatePoint {
  t: number
  user: number
  system: number
  nice: number
  iowait: number
  irq: number
  steal: number
  idle: number
}

export interface DiskIOPoint {
  t: number
  read: number // bytes/sec, summed across devices
  write: number // bytes/sec, summed across devices
  readOps: number // ops/sec, summed across devices
  writeOps: number // ops/sec, summed across devices
}

// DiskIOTotals is the current disk throughput/IOPS summed across all devices,
// plus the device count so the panel can note how many disks are aggregated.
export interface DiskIOTotals {
  read: number
  write: number
  readOps: number
  writeOps: number
  devices: number
  at: string
}

export interface NodeData {
  metrics: Metrics | null
  series: ChartPoint[]
  checks: Check[]
  disk: DiskMount[]
  net: NetCurrent | null
  netSeries: NetPoint[]
  cpuState: CPUState | null
  cpuStateSeries: CPUStatePoint[]
  diskio: DiskIOTotals | null
  diskioSeries: DiskIOPoint[]
  processes: ProcessInfo[]
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
  cpuState: null,
  cpuStateSeries: [],
  diskio: null,
  diskioSeries: [],
  processes: [],
  loading: true,
  unreachable: false,
  lastUpdated: null,
}

// cpuStatePoint maps a CPU-state history/current reading to a chart point.
function cpuStatePoint(p: CPUState): CPUStatePoint {
  return {
    t: new Date(p.at).getTime(),
    user: p.user,
    system: p.system,
    nice: p.nice,
    iowait: p.iowait,
    irq: p.irq,
    steal: p.steal,
    idle: p.idle,
  }
}

// sumDiskIO aggregates a node's per-device snapshot into totals. Returns null
// when no devices are reported (e.g. no sample yet).
function sumDiskIO(rows: DiskIODevice[]): DiskIOTotals | null {
  if (rows.length === 0) return null
  let read = 0
  let write = 0
  let readOps = 0
  let writeOps = 0
  let at = ''
  for (const r of rows) {
    read += r.read_bytes_rate
    write += r.write_bytes_rate
    readOps += r.read_ops_rate
    writeOps += r.write_ops_rate
    if (r.at > at) at = r.at
  }
  return { read, write, readOps, writeOps, devices: rows.length, at }
}

// useNodeData drives the detail view for one node: seeds the time-series from
// persisted history for the chosen range, then polls metrics + checks on a
// self-scheduling timer (no overlap), aborting in-flight requests on change.
export function useNodeData(
  node: string | null,
  range: TimeRange,
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
        const { fromSec, toSec } = resolveWindow(range)
        const [hist, netHist, cpuHist, ioHist] = await Promise.all([
          fetchHistory(node, fromSec, toSec, controller.signal),
          fetchNetworkHistory(node, fromSec, toSec, controller.signal),
          fetchCPUStateHistory(node, fromSec, toSec, controller.signal),
          fetchDiskIOHistory(node, fromSec, toSec, controller.signal),
        ])
        if (!active) return
        const series = hist.map<ChartPoint>((p) => ({
          t: new Date(p.at).getTime(),
          cpu: p.cpu_percent,
          memPct: p.mem_percent,
          memUsed: p.mem_used,
          memTotal: p.mem_total,
          load1: p.load1,
          load5: p.load5,
          load15: p.load15,
        }))
        const netSeries = netHist.map<NetPoint>((p) => ({
          t: new Date(p.at).getTime(),
          recv: p.recv_rate,
          sent: p.sent_rate,
        }))
        const cpuStateSeries = cpuHist.map(cpuStatePoint)
        const diskioSeries = ioHist.map<DiskIOPoint>((p) => ({
          t: new Date(p.at).getTime(),
          read: p.read_bytes_rate,
          write: p.write_bytes_rate,
          readOps: p.read_ops_rate,
          writeOps: p.write_ops_rate,
        }))
        setData((d) => ({ ...d, series, netSeries, cpuStateSeries, diskioSeries }))
      } catch {
        /* history is best-effort; live polling fills the charts */
      }
    }

    const tick = async () => {
      try {
        const [m, cs, disk, net, cpuState, ioRows, procs] = await Promise.all([
          fetchMetrics(node, controller.signal),
          fetchChecks(node, controller.signal),
          fetchDisk(node, controller.signal),
          fetchNetwork(node, controller.signal),
          fetchCPUState(node, controller.signal),
          fetchDiskIO(node, controller.signal),
          fetchProcesses(node, controller.signal),
        ])
        if (!active) return
        const diskio = sumDiskIO(ioRows)
        const point: ChartPoint = {
          t: new Date(m.collected_at).getTime(),
          cpu: m.cpu_percent,
          memPct: m.mem_percent,
          memUsed: m.mem_used,
          memTotal: m.mem_total,
          load1: m.load1,
          load5: m.load5,
          load15: m.load15,
        }
        setData((d) => {
          // For a fixed (past) range the chart is static: keep the seeded series
          // and only refresh the headline/tables below. Live ranges append and
          // trim to the rolling window.
          if (!range.live) {
            return {
              metrics: m,
              series: d.series,
              checks: cs,
              disk,
              net,
              netSeries: d.netSeries,
              cpuState,
              cpuStateSeries: d.cpuStateSeries,
              diskio,
              diskioSeries: d.diskioSeries,
              processes: procs,
              loading: false,
              unreachable: false,
              lastUpdated: Date.now(),
            }
          }

          const cutoff = Date.now() - range.spanMs

          // Skip duplicate points: /metrics returns the latest persisted sample,
          // which only advances every metrics_interval (slower than our poll).
          const lastT = d.series[d.series.length - 1]?.t
          const series = lastT === point.t ? d.series : appendLive(d.series, point, cutoff)

          const netT = net ? new Date(net.at).getTime() : null
          const lastNetT = d.netSeries[d.netSeries.length - 1]?.t
          const netSeries =
            net && netT !== lastNetT
              ? appendLive(d.netSeries, { t: netT!, recv: net.recv_rate, sent: net.sent_rate }, cutoff)
              : d.netSeries

          const cpuT = cpuState ? new Date(cpuState.at).getTime() : null
          const lastCpuT = d.cpuStateSeries[d.cpuStateSeries.length - 1]?.t
          const cpuStateSeries =
            cpuState && cpuT !== lastCpuT
              ? appendLive(d.cpuStateSeries, cpuStatePoint(cpuState), cutoff)
              : d.cpuStateSeries

          const ioT = diskio ? new Date(diskio.at).getTime() : null
          const lastIoT = d.diskioSeries[d.diskioSeries.length - 1]?.t
          const diskioSeries =
            diskio && ioT !== lastIoT
              ? appendLive(
                  d.diskioSeries,
                  {
                    t: ioT!,
                    read: diskio.read,
                    write: diskio.write,
                    readOps: diskio.readOps,
                    writeOps: diskio.writeOps,
                  },
                  cutoff,
                )
              : d.diskioSeries
          return {
            metrics: m,
            series,
            checks: cs,
            disk,
            net,
            netSeries,
            cpuState,
            cpuStateSeries,
            diskio,
            diskioSeries,
            processes: procs,
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
        // Only keep polling for live ranges; a fixed past window is static.
        if (active && !paused && range.live) timer = setTimeout(tick, POLL_MS)
      }
    }

    seed()
    tick()

    return () => {
      active = false
      clearTimeout(timer)
      controller.abort()
    }
  }, [node, range, paused])

  return data
}

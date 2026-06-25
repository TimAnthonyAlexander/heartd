import { useEffect, useState } from 'react'
import { fetchMetrics, fetchNodes, type Node } from '../api'

const POLL_MS = 4000
const SPARK_LEN = 24

export interface ClusterState {
  nodes: Node[]
  // Rolling recent CPU% per node, for the sidebar mini sparklines.
  cpuByNode: Record<string, number[]>
  summary: { up: number; total: number }
  ready: boolean
}

// useCluster polls the node list and a short rolling CPU history per node so the
// sidebar can show live status and a glanceable sparkline for the whole cluster.
export function useCluster(): ClusterState {
  const [nodes, setNodes] = useState<Node[]>([])
  const [cpuByNode, setCpuByNode] = useState<Record<string, number[]>>({})
  const [ready, setReady] = useState(false)

  useEffect(() => {
    let active = true
    let timer: ReturnType<typeof setTimeout>

    const tick = async () => {
      try {
        const ns = await fetchNodes()
        if (!active) return
        setNodes(ns)
        setReady(true)

        // Pull current CPU for each node (best-effort; down nodes just skip).
        const readings = await Promise.all(
          ns.map(async (n) => {
            try {
              const m = await fetchMetrics(n.name)
              return [n.name, m.cpu_percent] as const
            } catch {
              return [n.name, null] as const
            }
          }),
        )
        if (!active) return
        setCpuByNode((prev) => {
          const next: Record<string, number[]> = {}
          for (const [name, cpu] of readings) {
            const prior = prev[name] ?? []
            next[name] = cpu == null ? prior : [...prior, cpu].slice(-SPARK_LEN)
          }
          return next
        })
      } catch {
        /* transient; retry next tick */
      } finally {
        if (active) timer = setTimeout(tick, POLL_MS)
      }
    }

    tick()
    return () => {
      active = false
      clearTimeout(timer)
    }
  }, [])

  const up = nodes.filter((n) => n.status === 'ok' || n.local).length
  return { nodes, cpuByNode, summary: { up, total: nodes.length }, ready }
}

import { useCallback, useEffect, useState } from 'react'
import { fetchMetrics, fetchNodes, type Node } from '../api'

const POLL_MS = 4000
const SPARK_LEN = 24

export interface ClusterState {
  nodes: Node[]
  // Rolling recent CPU% per node, for the sidebar mini sparklines.
  cpuByNode: Record<string, number[]>
  summary: { up: number; total: number }
  ready: boolean
  // Force an immediate re-fetch of the node list (e.g. after adding/removing).
  reload: () => void
}

// useCluster polls the node list and a short rolling CPU history per node so the
// sidebar can show live status and a glanceable sparkline for the whole cluster.
export function useCluster(): ClusterState {
  const [nodes, setNodes] = useState<Node[]>([])
  const [cpuByNode, setCpuByNode] = useState<Record<string, number[]>>({})
  const [ready, setReady] = useState(false)
  const [nonce, setNonce] = useState(0)
  const reload = useCallback(() => setNonce((n) => n + 1), [])

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
  }, [nonce])

  // Muted peers are ignored from this node's perspective, so they don't count
  // toward the cluster up/total summary.
  const counted = nodes.filter((n) => !n.muted)
  const up = counted.filter((n) => n.status === 'ok' || n.local).length
  return { nodes, cpuByNode, summary: { up, total: counted.length }, ready, reload }
}

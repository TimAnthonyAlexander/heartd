import { useEffect, useRef, useState } from 'react'
import { Box, Stack, Typography } from '@mui/material'
import { NodeSidebar } from './components/NodeSidebar'
import { MetricCard } from './components/MetricCard'
import { CheckList } from './components/CheckList'
import {
  fetchChecks,
  fetchHistory,
  fetchMetrics,
  fetchNodes,
  type Check,
  type Metrics,
  type Node,
} from './api'

const POLL_MS = 3000
const HISTORY_LEN = 60

function formatBytes(bytes: number): string {
  const gb = bytes / 1024 ** 3
  return `${gb.toFixed(1)} GB`
}

export default function App() {
  const [nodes, setNodes] = useState<Node[]>([])
  const [selected, setSelected] = useState<string | null>(null)
  const [metrics, setMetrics] = useState<Metrics | null>(null)
  const [checks, setChecks] = useState<Check[]>([])
  const [error, setError] = useState<string | null>(null)
  const cpuHistory = useRef<number[]>([])
  const memHistory = useRef<number[]>([])

  // Load the node list once on mount.
  useEffect(() => {
    fetchNodes()
      .then((ns) => {
        setNodes(ns)
        const local = ns.find((n) => n.local) ?? ns[0]
        if (local) setSelected(local.name)
      })
      .catch((e) => setError(String(e)))
  }, [])

  // Poll metrics for the selected node.
  useEffect(() => {
    if (!selected) return
    cpuHistory.current = []
    memHistory.current = []
    setChecks([])

    let active = true

    // Seed the sparklines from persisted history so they're populated on load
    // (and survive a page reload) instead of starting empty.
    fetchHistory(selected, 60)
      .then((points) => {
        if (!active) return
        cpuHistory.current = points.map((p) => p.cpu_percent).slice(-HISTORY_LEN)
        memHistory.current = points.map((p) => p.mem_percent).slice(-HISTORY_LEN)
      })
      .catch(() => {
        /* history is best-effort; live polling still fills the sparkline */
      })

    const tick = async () => {
      try {
        const [m, cs] = await Promise.all([fetchMetrics(selected), fetchChecks(selected)])
        if (!active) return
        cpuHistory.current = [...cpuHistory.current, m.cpu_percent].slice(-HISTORY_LEN)
        memHistory.current = [...memHistory.current, m.mem_percent].slice(-HISTORY_LEN)
        setMetrics(m)
        setChecks(cs)
        setError(null)
      } catch (e) {
        if (active) setError(String(e))
      }
    }

    tick()
    const id = setInterval(tick, POLL_MS)
    return () => {
      active = false
      clearInterval(id)
    }
  }, [selected])

  return (
    <Box sx={{ display: 'flex', minHeight: '100vh', bgcolor: '#121212' }}>
      <NodeSidebar nodes={nodes} selected={selected} onSelect={setSelected} />
      <Box sx={{ flex: 1, p: 4 }}>
        <Typography variant="h5" sx={{ color: '#fff', mb: 3 }}>
          {selected ?? 'No node selected'}
        </Typography>

        {error && <Typography sx={{ color: '#f44336', mb: 2 }}>{error}</Typography>}

        {metrics && (
          <Stack direction="row" spacing={2} useFlexGap sx={{ flexWrap: 'wrap' }}>
            <MetricCard
              title="CPU"
              value={`${metrics.cpu_percent.toFixed(1)}%`}
              percent={metrics.cpu_percent}
              history={cpuHistory.current}
            />
            <MetricCard
              title="Memory"
              value={`${formatBytes(metrics.mem_used)} / ${formatBytes(metrics.mem_total)}`}
              percent={metrics.mem_percent}
              history={memHistory.current}
            />
          </Stack>
        )}

        {selected && <CheckList checks={checks} />}
      </Box>
    </Box>
  )
}

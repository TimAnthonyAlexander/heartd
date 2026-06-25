import { useEffect, useState } from 'react'
import { Box, Drawer, Skeleton, useMediaQuery } from '@mui/material'
import { Sidebar } from './components/Sidebar'
import { TopBar } from './components/TopBar'
import { MetricPanel } from './components/MetricPanel'
import { DiskPanel } from './components/DiskPanel'
import { NetworkPanel } from './components/NetworkPanel'
import { ChecksTable } from './components/ChecksTable'
import { useCluster } from './hooks/useCluster'
import { useHashNode } from './hooks/useHashNode'
import { useNodeData } from './hooks/useNodeData'
import { colors, theme } from './theme'

function formatGB(bytes: number): string {
  return (bytes / 1024 ** 3).toFixed(1)
}

export default function App() {
  const { nodes, cpuByNode, summary, ready } = useCluster()
  const { node: selected, select, replace } = useHashNode()
  const [rangeMinutes, setRangeMinutes] = useState(60)
  const [paused, setPaused] = useState(false)
  const [drawerOpen, setDrawerOpen] = useState(false)

  const isMobile = useMediaQuery(theme.breakpoints.down('md'))
  const data = useNodeData(selected, rangeMinutes, paused)

  // Default to the local node, or fall back if the hash names an unknown node.
  useEffect(() => {
    if (!ready || nodes.length === 0) return
    const exists = selected && nodes.some((n) => n.name === selected)
    if (!exists) {
      const local = nodes.find((n) => n.local) ?? nodes[0]
      if (local) replace(local.name)
    }
  }, [ready, nodes, selected, replace])

  const selectedNode = nodes.find((n) => n.name === selected) ?? null
  const status = selectedNode ? selectedNode.status : null
  const m = data.metrics

  const sidebar = (
    <Sidebar
      nodes={nodes}
      cpuByNode={cpuByNode}
      summary={summary}
      selected={selected}
      onSelect={(n) => {
        select(n)
        setDrawerOpen(false)
      }}
    />
  )

  return (
    <Box sx={{ display: 'flex', minHeight: '100vh', bgcolor: colors.bg }}>
      {isMobile ? (
        <Drawer open={drawerOpen} onClose={() => setDrawerOpen(false)}>
          {sidebar}
        </Drawer>
      ) : (
        sidebar
      )}

      <Box sx={{ flex: 1, minWidth: 0 }}>
        <TopBar
          nodeName={selected}
          status={status}
          lastUpdated={data.lastUpdated}
          paused={paused}
          onTogglePause={() => setPaused((p) => !p)}
          rangeMinutes={rangeMinutes}
          onRangeChange={setRangeMinutes}
          onMenu={isMobile ? () => setDrawerOpen(true) : undefined}
        />

        <Box sx={{ p: { xs: 2, md: 4 }, maxWidth: 1280, mx: 'auto' }}>
          {data.unreachable && (
            <Box
              sx={{
                mb: 3,
                px: 2.5,
                py: 1.5,
                borderRadius: 2,
                border: `1px solid ${colors.error}55`,
                bgcolor: `${colors.error}14`,
                color: colors.error,
                fontSize: 14,
              }}
            >
              {selected} is unreachable — showing last known values.
            </Box>
          )}

          <Box
            sx={{
              display: 'grid',
              gridTemplateColumns: 'repeat(auto-fit, minmax(320px, 1fr))',
              gap: 2.5,
              mb: 4,
            }}
          >
            {data.loading && !m ? (
              <>
                <PanelSkeleton />
                <PanelSkeleton />
              </>
            ) : m ? (
              <>
                <MetricPanel
                  title="CPU"
                  headline={`${m.cpu_percent.toFixed(1)}%`}
                  percent={m.cpu_percent}
                  data={data.series.map((p) => ({ t: p.t, v: p.cpu }))}
                  dimmed={data.unreachable}
                />
                <MetricPanel
                  title="Memory"
                  headline={`${formatGB(m.mem_used)} / ${formatGB(m.mem_total)} GB`}
                  percent={m.mem_percent}
                  data={data.series.map((p) => ({ t: p.t, v: p.memPct }))}
                  dimmed={data.unreachable}
                />
              </>
            ) : null}
            <DiskPanel disks={data.disk} dimmed={data.unreachable} />
            <NetworkPanel net={data.net} series={data.netSeries} dimmed={data.unreachable} />
          </Box>

          <ChecksTable checks={data.checks} />
        </Box>
      </Box>
    </Box>
  )
}

function PanelSkeleton() {
  return (
    <Box sx={{ p: 3, borderRadius: 2.5, border: `1px solid ${colors.border}` }}>
      <Skeleton variant="text" width={60} />
      <Skeleton variant="text" width={140} height={48} />
      <Skeleton variant="rounded" height={150} sx={{ mt: 2 }} />
    </Box>
  )
}

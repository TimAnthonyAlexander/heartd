import { useEffect, useState } from 'react'
import { useNavigate, useParams } from 'react-router'
import { Box, Drawer, Skeleton, Tab, Tabs, useMediaQuery } from '@mui/material'
import { Sidebar } from './components/Sidebar'
import { TopBar } from './components/TopBar'
import { MetricPanel } from './components/MetricPanel'
import { DiskPanel } from './components/DiskPanel'
import { NetworkPanel } from './components/NetworkPanel'
import { ChecksTable } from './components/ChecksTable'
import { NodeConfig, type ConfigTab } from './components/NodeConfig'
import { useCluster } from './hooks/useCluster'
import { useNodeData } from './hooks/useNodeData'
import { colors, theme } from './theme'

type NodeTab = 'dashboard' | ConfigTab

const TABS: { value: NodeTab; label: string }[] = [
  { value: 'dashboard', label: 'Dashboard' },
  { value: 'checks', label: 'Checks' },
  { value: 'notifications', label: 'Notifications' },
  { value: 'alerts', label: 'Alerts' },
  { value: 'settings', label: 'Settings' },
]

function formatGB(bytes: number): string {
  return (bytes / 1024 ** 3).toFixed(1)
}

interface AppProps {
  username: string | null
  onLogout: () => void
}

export default function App({ username, onLogout }: AppProps) {
  const { nodes, cpuByNode, summary, ready } = useCluster()
  const navigate = useNavigate()
  const { name } = useParams()
  const selected = name ?? null
  const [rangeMinutes, setRangeMinutes] = useState(60)
  const [paused, setPaused] = useState(false)
  const [drawerOpen, setDrawerOpen] = useState(false)
  const [tab, setTab] = useState<NodeTab>('dashboard')

  const isMobile = useMediaQuery(theme.breakpoints.down('md'))
  const data = useNodeData(selected, rangeMinutes, paused)

  // Default to the local node, or fall back if the URL names an unknown node.
  useEffect(() => {
    if (!ready || nodes.length === 0) return
    const exists = selected && nodes.some((n) => n.name === selected)
    if (!exists) {
      const local = nodes.find((n) => n.local) ?? nodes[0]
      if (local) navigate(`/node/${encodeURIComponent(local.name)}`, { replace: true })
    }
  }, [ready, nodes, selected, navigate])

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
        navigate(`/node/${encodeURIComponent(n)}`)
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
          username={username}
          onLogout={onLogout}
          onSettings={() => navigate('/settings')}
        />

        <Box sx={{ px: { xs: 2, md: 4 }, borderBottom: `1px solid ${colors.border}` }}>
          <Box sx={{ maxWidth: 1280, mx: 'auto' }}>
            <Tabs
              value={tab}
              onChange={(_, v: NodeTab) => setTab(v)}
              variant="scrollable"
              scrollButtons="auto"
              sx={{ minHeight: 44, '& .MuiTab-root': { minHeight: 44, textTransform: 'none', fontSize: 14 } }}
            >
              {TABS.map((t) => (
                <Tab key={t.value} value={t.value} label={t.label} />
              ))}
            </Tabs>
          </Box>
        </Box>

        <Box sx={{ p: { xs: 2, md: 4 }, maxWidth: 1280, mx: 'auto' }}>
          {tab === 'dashboard' ? (
            <>
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
            </>
          ) : selected ? (
            <NodeConfig
              nodeName={selected}
              isLocal={selectedNode?.local ?? false}
              tab={tab}
            />
          ) : null}
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

import { useEffect, useState } from 'react'
import { useNavigate, useParams } from 'react-router'
import { Box, Drawer, Skeleton, useMediaQuery } from '@mui/material'
import { Sidebar } from './components/Sidebar'
import { TopBar } from './components/TopBar'
import { MetricPanel } from './components/MetricPanel'
import { DiskPanel } from './components/DiskPanel'
import { NetworkPanel } from './components/NetworkPanel'
import { DiskIOPanel } from './components/DiskIOPanel'
import { LoadPanel } from './components/LoadPanel'
import { SwapPanel } from './components/SwapPanel'
import { ChecksTable } from './components/ChecksTable'
import { AlertsPanel } from './components/AlertsPanel'
import { NodeConfig, type ConfigTab, type EditTarget } from './components/NodeConfig'
import { SegmentedTabs, type TabItem } from './components/SegmentedTabs'
import { RenameDialog } from './components/RenameDialog'
import { nodeLabel } from './api'
import { useCluster } from './hooks/useCluster'
import { useNodeData } from './hooks/useNodeData'
import { livePreset, RANGE_PRESETS, type TimeRange } from './timerange'
import { useNodeAlerts } from './hooks/useNodeAlerts'
import { useNodeAlertActivity } from './hooks/useNodeAlertActivity'
import { colors, theme } from './theme'

type NodeTab = 'dashboard' | ConfigTab

const TABS: TabItem<NodeTab>[] = [
  { value: 'dashboard', label: 'Dashboard' },
  { value: 'checks', label: 'Checks' },
  { value: 'notifications', label: 'Notifications' },
  { value: 'alerts', label: 'Alerts' },
  { value: 'settings', label: 'Settings' },
]

function asTab(value: string | undefined): NodeTab {
  return TABS.some((t) => t.value === value) ? (value as NodeTab) : 'dashboard'
}

// nodePath builds the URL for a node, omitting the segment for the default tab so
// the dashboard stays at the clean /node/:name path.
function nodePath(name: string, tab: NodeTab): string {
  const base = `/node/${encodeURIComponent(name)}`
  return tab === 'dashboard' ? base : `${base}/${tab}`
}

function formatGB(bytes: number): string {
  return (bytes / 1024 ** 3).toFixed(1)
}

interface AppProps {
  username: string | null
  onLogout: () => void
}

export default function App({ username, onLogout }: AppProps) {
  const { nodes, cpuByNode, summary, ready, reload } = useCluster()
  const navigate = useNavigate()
  const { name, tab: tabParam } = useParams()
  const selected = name ?? null
  const tab = asTab(tabParam)
  // Default to the 1h live preset.
  const [range, setRange] = useState<TimeRange>(() => livePreset(RANGE_PRESETS[1]))
  const [paused, setPaused] = useState(false)
  const [drawerOpen, setDrawerOpen] = useState(false)
  const [renaming, setRenaming] = useState(false)
  const [editTarget, setEditTarget] = useState<EditTarget>(null)

  const goToTab = (t: NodeTab) => {
    if (selected) navigate(nodePath(selected, t))
  }

  // requestEdit jumps from the dashboard to the Checks/Alerts tab with the chosen
  // item's edit form open. The section consumes editTarget once it has loaded.
  const requestEdit = (t: NonNullable<EditTarget>) => {
    if (!selected) return
    setEditTarget(t)
    navigate(nodePath(selected, t.kind === 'check' ? 'checks' : 'alerts'))
  }

  const isMobile = useMediaQuery(theme.breakpoints.down('md'))
  const data = useNodeData(selected, range, paused)
  const alerts = useNodeAlerts(selected, tab === 'dashboard')
  const alertActivity = useNodeAlertActivity(selected, tab === 'dashboard')

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
  const displayName = selectedNode ? nodeLabel(selectedNode) : selected
  const m = data.metrics

  const sidebar = (
    <Sidebar
      nodes={nodes}
      cpuByNode={cpuByNode}
      summary={summary}
      selected={selected}
      onSelect={(n) => {
        navigate(nodePath(n, tab))
        setDrawerOpen(false)
      }}
      onChanged={reload}
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
          nodeName={displayName}
          status={status}
          lastUpdated={data.lastUpdated}
          paused={paused}
          onTogglePause={() => setPaused((p) => !p)}
          range={range}
          onRangeChange={setRange}
          onMenu={isMobile ? () => setDrawerOpen(true) : undefined}
          username={username}
          onLogout={onLogout}
          onSettings={() => navigate('/settings')}
          onRename={selectedNode ? () => setRenaming(true) : undefined}
        />

        <Box sx={{ px: { xs: 2, md: 4 }, pt: 2.5, pb: 0.5 }}>
          <SegmentedTabs items={TABS} value={tab} onChange={goToTab} />
        </Box>

        {/* Left-aligned: the dashboard spreads wide for 4 panels per row; the
            config tabs stay at a readable form width. Neither is centered. */}
        <Box sx={{ p: { xs: 2, md: 4 }, maxWidth: tab === 'dashboard' ? 1760 : 820 }}>
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
                  {displayName} is unreachable — showing last known values.
                </Box>
              )}

              <Box
                sx={{
                  display: 'grid',
                  gridTemplateColumns: 'repeat(auto-fit, minmax(280px, 1fr))',
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
                    {m.swap_total > 0 && (
                      <SwapPanel
                        used={m.swap_used}
                        total={m.swap_total}
                        percent={m.swap_percent}
                        dimmed={data.unreachable}
                      />
                    )}
                    <LoadPanel
                      load1={m.load1}
                      load5={m.load5}
                      load15={m.load15}
                      series={data.series}
                      dimmed={data.unreachable}
                    />
                  </>
                ) : null}
                <DiskPanel disks={data.disk} dimmed={data.unreachable} />
                <NetworkPanel net={data.net} series={data.netSeries} dimmed={data.unreachable} />
                <DiskIOPanel io={data.diskio} series={data.diskioSeries} dimmed={data.unreachable} />
              </Box>

              <Box sx={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
                <ChecksTable
                  checks={data.checks}
                  onEdit={(name) => requestEdit({ kind: 'check', name })}
                />
                <AlertsPanel
                  alerts={alerts}
                  active={alertActivity.active}
                  history={alertActivity.history}
                  onEdit={(id) => requestEdit({ kind: 'alert', id })}
                />
              </Box>
            </>
          ) : selected ? (
            <NodeConfig
              nodeName={selected}
              isLocal={selectedNode?.local ?? false}
              tab={tab}
              editTarget={editTarget}
              onEditConsumed={() => setEditTarget(null)}
            />
          ) : null}
        </Box>
      </Box>

      {renaming && selectedNode && (
        <RenameDialog
          open
          node={selectedNode}
          onClose={() => setRenaming(false)}
          onRenamed={reload}
        />
      )}
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

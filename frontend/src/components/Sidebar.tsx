import { useState } from 'react'
import { Box, IconButton, Tooltip, Typography } from '@mui/material'
import type { Node } from '../api'
import { deletePeer, fetchPeers, nodeLabel } from '../api'
import { colors, statusColor } from '../theme'
import { Sparkline } from './Sparkline'
import { NodeDialog } from './NodeDialog'

interface Props {
  nodes: Node[]
  cpuByNode: Record<string, number[]>
  summary: { up: number; total: number }
  selected: string | null
  onSelect: (name: string) => void
  // Called after a node is added / edited / removed so the cluster list refreshes.
  onChanged: () => void
}

type DialogState =
  | { mode: 'add' }
  | { mode: 'edit'; name: string; url: string; muted: boolean }
  | null

export function Sidebar({ nodes, cpuByNode, summary, selected, onSelect, onChanged }: Props) {
  const allUp = summary.total > 0 && summary.up === summary.total
  const [dialog, setDialog] = useState<DialogState>(null)
  const [busy, setBusy] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  const openEdit = async (name: string) => {
    setError(null)
    try {
      const peers = await fetchPeers()
      const peer = peers.find((p) => p.name === name)
      setDialog({ mode: 'edit', name, url: peer?.url ?? '', muted: peer?.muted ?? false })
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Could not load node')
    }
  }

  const remove = async (name: string) => {
    if (
      !window.confirm(
        `Remove node "${name}"? This stops polling it and permanently deletes its stored metric, check, disk, and network history.`,
      )
    )
      return
    setError(null)
    setBusy(name)
    try {
      await deletePeer(name)
      onChanged()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Remove failed')
    } finally {
      setBusy(null)
    }
  }

  return (
    <Box
      sx={{
        width: 260,
        flexShrink: 0,
        borderRight: `1px solid ${colors.border}`,
        height: '100vh',
        position: 'sticky',
        top: 0,
        display: 'flex',
        flexDirection: 'column',
        bgcolor: colors.bg,
      }}
    >
      <Box sx={{ px: 3, pt: 3, pb: 2 }}>
        <Typography sx={{ fontSize: 20, fontWeight: 700, letterSpacing: '-0.02em' }}>
          heartd
        </Typography>
        <Typography sx={{ color: allUp ? colors.textDim : colors.error, fontSize: 13, mt: 0.5 }}>
          {summary.total > 0 ? `${summary.up}/${summary.total} nodes up` : 'connecting…'}
        </Typography>
      </Box>

      <Box sx={{ overflowY: 'auto', px: 1.5, pb: 2, flex: 1 }}>
        {nodes.map((node) => {
          const isSel = node.name === selected
          const dot = node.muted ? colors.textFaint : statusColor(node.status)
          return (
            <Box
              key={node.name}
              onClick={() => onSelect(node.name)}
              sx={{
                p: 1.5,
                mb: 0.5,
                borderRadius: 2,
                cursor: 'pointer',
                opacity: busy === node.name ? 0.5 : node.muted ? 0.5 : 1,
                bgcolor: isSel ? colors.panel : 'transparent',
                border: `1px solid ${isSel ? colors.border : 'transparent'}`,
                transition: 'background-color 120ms',
                '&:hover': { bgcolor: isSel ? colors.panel : colors.panelHover },
                '&:hover .node-actions': { opacity: 1 },
              }}
            >
              <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                <Box sx={{ width: 8, height: 8, borderRadius: '50%', bgcolor: dot, flexShrink: 0 }} />
                <Typography
                  sx={{ fontSize: 14, fontWeight: 600, flex: 1 }}
                  noWrap
                  title={node.alias ? node.name : undefined}
                >
                  {nodeLabel(node)}
                </Typography>
                {node.local ? (
                  <Typography sx={{ fontSize: 11, color: colors.textFaint }}>local</Typography>
                ) : (
                  <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5 }}>
                    {node.muted && (
                      <Typography sx={{ fontSize: 10.5, color: colors.textFaint, textTransform: 'uppercase', letterSpacing: '0.05em' }}>
                        muted
                      </Typography>
                    )}
                    <Box
                      className="node-actions"
                      sx={{ display: 'flex', gap: 0.25, opacity: 0, transition: 'opacity 120ms' }}
                    >
                    <Tooltip title="Edit">
                      <IconButton
                        size="small"
                        sx={{ color: colors.textDim, p: 0.25 }}
                        onClick={(e) => {
                          e.stopPropagation()
                          void openEdit(node.name)
                        }}
                      >
                        <Box component="span" sx={{ fontSize: 13, lineHeight: 1 }}>
                          ✎
                        </Box>
                      </IconButton>
                    </Tooltip>
                    <Tooltip title="Remove">
                      <IconButton
                        size="small"
                        sx={{ color: colors.textDim, p: 0.25 }}
                        onClick={(e) => {
                          e.stopPropagation()
                          void remove(node.name)
                        }}
                      >
                        <Box component="span" sx={{ fontSize: 13, lineHeight: 1 }}>
                          ✕
                        </Box>
                      </IconButton>
                    </Tooltip>
                    </Box>
                  </Box>
                )}
              </Box>
              <Box sx={{ mt: 0.5, ml: 2, opacity: node.status === 'down' ? 0.3 : 1 }}>
                <Sparkline values={cpuByNode[node.name] ?? []} color={dot} />
              </Box>
            </Box>
          )
        })}

        <Box
          onClick={() => setDialog({ mode: 'add' })}
          sx={{
            display: 'flex',
            alignItems: 'center',
            gap: 1,
            mt: 0.5,
            px: 1.5,
            py: 1.25,
            borderRadius: 2,
            cursor: 'pointer',
            color: colors.textDim,
            border: `1px dashed ${colors.border}`,
            '&:hover': { bgcolor: colors.panelHover, color: colors.text },
          }}
        >
          <Box component="span" sx={{ fontSize: 16, lineHeight: 1, width: 8, textAlign: 'center' }}>
            +
          </Box>
          <Typography sx={{ fontSize: 13, fontWeight: 600 }}>Add node</Typography>
        </Box>

        {error && (
          <Typography sx={{ fontSize: 12, color: colors.error, px: 1.5, mt: 1 }}>{error}</Typography>
        )}
      </Box>

      {dialog && (
        <NodeDialog
          open
          mode={dialog.mode}
          initialName={dialog.mode === 'edit' ? dialog.name : ''}
          initialUrl={dialog.mode === 'edit' ? dialog.url : ''}
          initialMuted={dialog.mode === 'edit' ? dialog.muted : false}
          onClose={() => setDialog(null)}
          onSaved={onChanged}
        />
      )}
    </Box>
  )
}

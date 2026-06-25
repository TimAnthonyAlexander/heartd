import { Box, Typography } from '@mui/material'
import type { Node } from '../api'
import { colors, statusColor } from '../theme'
import { Sparkline } from './Sparkline'

interface Props {
  nodes: Node[]
  cpuByNode: Record<string, number[]>
  summary: { up: number; total: number }
  selected: string | null
  onSelect: (name: string) => void
}

export function Sidebar({ nodes, cpuByNode, summary, selected, onSelect }: Props) {
  const allUp = summary.total > 0 && summary.up === summary.total

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
          const dot = statusColor(node.status)
          return (
            <Box
              key={node.name}
              onClick={() => onSelect(node.name)}
              sx={{
                p: 1.5,
                mb: 0.5,
                borderRadius: 2,
                cursor: 'pointer',
                bgcolor: isSel ? colors.panel : 'transparent',
                border: `1px solid ${isSel ? colors.border : 'transparent'}`,
                transition: 'background-color 120ms',
                '&:hover': { bgcolor: isSel ? colors.panel : colors.panelHover },
              }}
            >
              <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                <Box sx={{ width: 8, height: 8, borderRadius: '50%', bgcolor: dot, flexShrink: 0 }} />
                <Typography sx={{ fontSize: 14, fontWeight: 600, flex: 1 }} noWrap>
                  {node.name}
                </Typography>
                <Typography sx={{ fontSize: 11, color: colors.textFaint }}>
                  {node.local ? 'local' : node.status}
                </Typography>
              </Box>
              <Box sx={{ mt: 0.5, ml: 2, opacity: node.status === 'down' ? 0.3 : 1 }}>
                <Sparkline values={cpuByNode[node.name] ?? []} color={dot} />
              </Box>
            </Box>
          )
        })}
      </Box>
    </Box>
  )
}

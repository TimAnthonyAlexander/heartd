import { Box, Paper, Typography } from '@mui/material'
import type { ProcessInfo } from '../api'
import { colors, percentColor } from '../theme'

interface Props {
  processes: ProcessInfo[]
  dimmed?: boolean
}

// formatRSS renders a resident-set size in MB, switching to GB once it crosses
// 1024 MB so large processes stay readable.
function formatRSS(bytes: number): string {
  const mb = bytes / 1024 ** 2
  if (mb >= 1024) return `${(mb / 1024).toFixed(1)} GB`
  return `${mb.toFixed(0)} MB`
}

export function ProcessTable({ processes, dimmed }: Props) {
  return (
    <Box sx={{ opacity: dimmed ? 0.45 : 1 }}>
      <Box sx={{ display: 'flex', alignItems: 'baseline', gap: 2, mb: 1.5 }}>
        <Typography variant="overline" sx={{ color: colors.textDim }}>
          Top Processes
        </Typography>
        <Typography sx={{ fontSize: 12, color: colors.textFaint }}>
          by CPU share
        </Typography>
      </Box>

      <Paper elevation={0} sx={{ borderRadius: 2.5, overflow: 'hidden' }}>
        {processes.length === 0 ? (
          <Box sx={{ px: 2.5, py: 2.5 }}>
            <Typography sx={{ fontSize: 13, color: colors.textFaint }}>collecting data…</Typography>
          </Box>
        ) : (
          processes.map((p, i) => (
            <Box
              key={p.pid}
              sx={{
                display: 'flex',
                alignItems: 'center',
                gap: 2,
                px: 2.5,
                py: 1.5,
                borderTop: i === 0 ? 'none' : `1px solid ${colors.border}`,
              }}
            >
              <Box sx={{ flex: 1, minWidth: 0 }}>
                <Typography sx={{ fontSize: 14, fontWeight: 600 }} noWrap>
                  {p.name || '—'}
                </Typography>
                <Typography
                  sx={{ fontSize: 11, color: colors.textFaint }}
                  noWrap
                  title={p.command}
                >
                  {p.command || '—'}
                </Typography>
              </Box>
              <Typography sx={{ fontSize: 12, color: colors.textFaint, width: 64, textAlign: 'right' }}>
                {p.pid}
              </Typography>
              <Typography
                sx={{
                  fontSize: 13,
                  fontWeight: 600,
                  color: percentColor(p.cpu_percent),
                  width: 64,
                  textAlign: 'right',
                }}
              >
                {p.cpu_percent.toFixed(1)}%
              </Typography>
              <Typography sx={{ fontSize: 13, color: colors.textDim, width: 64, textAlign: 'right' }}>
                {p.mem_percent.toFixed(1)}%
              </Typography>
              <Typography sx={{ fontSize: 12, color: colors.textFaint, width: 72, textAlign: 'right' }}>
                {formatRSS(p.mem_rss)}
              </Typography>
            </Box>
          ))
        )}
      </Paper>
    </Box>
  )
}

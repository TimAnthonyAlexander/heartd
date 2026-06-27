import { Box, Paper, Typography } from '@mui/material'
import type { CoreInfo } from '../api'
import { colors, percentColor } from '../theme'

interface Props {
  cores: CoreInfo[]
  dimmed?: boolean
}

// PerCorePanel renders a responsive grid of per-core busy meters — one cell per
// logical core — so a single saturated core (a hot single-threaded process) is
// visible even when the machine-wide average reads low. Each cell shows the core
// index, a thin horizontal bar filled to the core's busy percentage and colored
// via percentColor, and the percentage label. Many cores wrap into a tidy grid.
export function PerCorePanel({ cores, dimmed }: Props) {
  return (
    <Paper elevation={0} sx={{ p: 3, borderRadius: 2.5, opacity: dimmed ? 0.45 : 1, minHeight: 240 }}>
      <Typography variant="overline" sx={{ color: colors.textDim }}>
        Per-Core CPU
      </Typography>

      {cores.length === 0 ? (
        <Box sx={{ height: 180, display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
          <Typography sx={{ fontSize: 12, color: colors.textFaint }}>collecting data…</Typography>
        </Box>
      ) : (
        <Box
          sx={{
            mt: 1.5,
            display: 'grid',
            gridTemplateColumns: 'repeat(auto-fill, minmax(120px, 1fr))',
            gap: 1.5,
          }}
        >
          {cores.map((c) => {
            const color = percentColor(c.percent)
            return (
              <Box key={c.core}>
                <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'baseline', mb: 0.5 }}>
                  <Typography sx={{ fontSize: 12, color: colors.textDim, fontWeight: 600 }}>
                    #{c.core}
                  </Typography>
                  <Typography sx={{ fontSize: 12, color, fontWeight: 600 }}>
                    {c.percent.toFixed(0)}%
                  </Typography>
                </Box>
                <Box
                  sx={{
                    height: 6,
                    borderRadius: 3,
                    bgcolor: colors.border,
                    overflow: 'hidden',
                  }}
                >
                  <Box
                    sx={{
                      height: '100%',
                      width: `${Math.min(100, Math.max(0, c.percent))}%`,
                      bgcolor: color,
                      borderRadius: 3,
                    }}
                  />
                </Box>
              </Box>
            )
          })}
        </Box>
      )}
    </Paper>
  )
}

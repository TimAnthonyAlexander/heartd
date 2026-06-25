import { Box, LinearProgress, Paper, Typography } from '@mui/material'
import type { DiskMount } from '../api'
import { colors, percentColor } from '../theme'

interface Props {
  disks: DiskMount[]
  dimmed?: boolean
}

function formatGB(bytes: number): string {
  const gb = bytes / 1024 ** 3
  if (gb >= 1024) return `${(gb / 1024).toFixed(1)} TB`
  return `${gb.toFixed(0)} GB`
}

export function DiskPanel({ disks, dimmed }: Props) {
  return (
    <Paper elevation={0} sx={{ p: 3, borderRadius: 2.5, opacity: dimmed ? 0.45 : 1, minHeight: 240 }}>
      <Typography variant="overline" sx={{ color: colors.textDim }}>
        Disk
      </Typography>

      {disks.length === 0 ? (
        <Box sx={{ height: 180, display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
          <Typography sx={{ fontSize: 12, color: colors.textFaint }}>collecting data…</Typography>
        </Box>
      ) : (
        <Box sx={{ mt: 1.5, display: 'flex', flexDirection: 'column', gap: 2 }}>
          {disks.map((d) => {
            const color = percentColor(d.percent)
            return (
              <Box key={d.mount}>
                <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'baseline', mb: 0.5 }}>
                  <Typography sx={{ fontSize: 14, fontWeight: 600 }} noWrap title={d.mount}>
                    {d.mount}
                  </Typography>
                  <Typography sx={{ fontSize: 12, color: colors.textDim }}>
                    {formatGB(d.used)} / {formatGB(d.total)}
                    <Box component="span" sx={{ color, ml: 1, fontWeight: 600 }}>
                      {d.percent.toFixed(0)}%
                    </Box>
                  </Typography>
                </Box>
                <LinearProgress
                  variant="determinate"
                  value={Math.min(100, d.percent)}
                  sx={{
                    height: 6,
                    borderRadius: 3,
                    bgcolor: colors.border,
                    '& .MuiLinearProgress-bar': { bgcolor: color, borderRadius: 3 },
                  }}
                />
              </Box>
            )
          })}
        </Box>
      )}
    </Paper>
  )
}

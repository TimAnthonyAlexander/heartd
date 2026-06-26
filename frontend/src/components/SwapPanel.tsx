import { Box, LinearProgress, Paper, Typography } from '@mui/material'
import { colors, percentColor } from '../theme'

interface Props {
  used: number
  total: number
  percent: number
  dimmed?: boolean
}

function formatGB(bytes: number): string {
  const gb = bytes / 1024 ** 3
  if (gb >= 1024) return `${(gb / 1024).toFixed(1)} TB`
  if (gb >= 10) return `${gb.toFixed(0)} GB`
  return `${gb.toFixed(1)} GB`
}

// SwapPanel mirrors the Memory rendering as a usage bar. The caller only mounts
// it when swap is configured (total > 0), so there is no "no swap" empty state.
export function SwapPanel({ used, total, percent, dimmed }: Props) {
  const color = percentColor(percent)

  return (
    <Paper elevation={0} sx={{ p: 3, borderRadius: 2.5, opacity: dimmed ? 0.45 : 1, minHeight: 240 }}>
      <Box sx={{ display: 'flex', alignItems: 'baseline', justifyContent: 'space-between' }}>
        <Typography variant="overline" sx={{ color: colors.textDim }}>
          Swap
        </Typography>
        <Typography sx={{ fontSize: 13, fontWeight: 600, color }}>
          {percent.toFixed(0)}%
        </Typography>
      </Box>

      <Typography sx={{ fontSize: 34, fontWeight: 600, letterSpacing: '-0.02em', mt: 0.5 }}>
        {formatGB(used)} / {formatGB(total)}
      </Typography>

      <LinearProgress
        variant="determinate"
        value={Math.min(100, percent)}
        sx={{
          mt: 1.5,
          height: 4,
          borderRadius: 2,
          bgcolor: colors.border,
          '& .MuiLinearProgress-bar': { bgcolor: color, borderRadius: 2 },
        }}
      />

      <Box sx={{ mt: 2, display: 'flex', alignItems: 'center', justifyContent: 'center', height: 120 }}>
        <Typography sx={{ fontSize: 12, color: colors.textFaint }}>
          {formatGB(total - used)} free
        </Typography>
      </Box>
    </Paper>
  )
}

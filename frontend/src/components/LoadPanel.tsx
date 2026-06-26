import { Box, Paper, Typography } from '@mui/material'
import {
  CartesianGrid,
  Line,
  LineChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts'
import type { ChartPoint } from '../hooks/useNodeData'
import { colors } from '../theme'

interface Props {
  load1: number
  load5: number
  load15: number
  series: ChartPoint[]
  dimmed?: boolean
}

// One color per averaging window; all reuse theme tokens (no hardcoded hex).
const ONE = colors.accent
const FIVE = colors.warn
const FIFTEEN = colors.textDim

function fmtTime(t: number): string {
  const d = new Date(t)
  return `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
}

function LoadTooltip({ active, payload }: any) {
  if (!active || !payload?.length) return null
  const p = payload[0].payload as ChartPoint
  return (
    <Box sx={{ bgcolor: colors.bg, border: `1px solid ${colors.border}`, borderRadius: 1, px: 1.25, py: 0.75 }}>
      <Typography sx={{ fontSize: 11, color: colors.textDim }}>{fmtTime(p.t)}</Typography>
      <Typography sx={{ fontSize: 12, color: ONE }}>1m {p.load1.toFixed(2)}</Typography>
      <Typography sx={{ fontSize: 12, color: FIVE }}>5m {p.load5.toFixed(2)}</Typography>
      <Typography sx={{ fontSize: 12, color: FIFTEEN }}>15m {p.load15.toFixed(2)}</Typography>
    </Box>
  )
}

export function LoadPanel({ load1, load5, load15, series, dimmed }: Props) {
  // Whether load was ever reported (some platforms have no load average).
  const hasData = series.some((p) => p.load1 > 0 || p.load5 > 0 || p.load15 > 0)

  return (
    <Paper elevation={0} sx={{ p: 3, borderRadius: 2.5, opacity: dimmed ? 0.45 : 1, minHeight: 240 }}>
      <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'baseline' }}>
        <Typography variant="overline" sx={{ color: colors.textDim }}>
          Load average
        </Typography>
        <Box sx={{ display: 'flex', gap: 2 }}>
          <Typography sx={{ fontSize: 13, color: ONE, fontWeight: 600 }}>{load1.toFixed(2)}</Typography>
          <Typography sx={{ fontSize: 13, color: FIVE, fontWeight: 600 }}>{load5.toFixed(2)}</Typography>
          <Typography sx={{ fontSize: 13, color: FIFTEEN, fontWeight: 600 }}>{load15.toFixed(2)}</Typography>
        </Box>
      </Box>

      <Box sx={{ display: 'flex', justifyContent: 'flex-end', gap: 2, mt: 0.5 }}>
        <Typography sx={{ fontSize: 11, color: colors.textFaint }}>1m</Typography>
        <Typography sx={{ fontSize: 11, color: colors.textFaint }}>5m</Typography>
        <Typography sx={{ fontSize: 11, color: colors.textFaint }}>15m</Typography>
      </Box>

      <Box sx={{ height: 168, mt: 1.5, mx: -1 }}>
        {series.length < 2 || !hasData ? (
          <Box sx={{ height: '100%', display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
            <Typography sx={{ fontSize: 12, color: colors.textFaint }}>
              {series.length >= 2 && !hasData ? 'load average unavailable' : 'collecting data…'}
            </Typography>
          </Box>
        ) : (
          <ResponsiveContainer width="100%" height="100%">
            <LineChart data={series} margin={{ top: 4, right: 8, bottom: 0, left: 8 }}>
              <CartesianGrid stroke={colors.border} vertical={false} />
              <XAxis
                dataKey="t"
                type="number"
                scale="time"
                domain={['dataMin', 'dataMax']}
                tickFormatter={fmtTime}
                tick={{ fontSize: 11, fill: colors.textFaint }}
                stroke={colors.border}
                minTickGap={40}
              />
              <YAxis hide domain={[0, 'dataMax']} />
              <Tooltip content={<LoadTooltip />} cursor={{ stroke: colors.border }} />
              <Line type="monotone" dataKey="load1" stroke={ONE} strokeWidth={2} dot={false} isAnimationActive={false} />
              <Line type="monotone" dataKey="load5" stroke={FIVE} strokeWidth={2} dot={false} isAnimationActive={false} />
              <Line type="monotone" dataKey="load15" stroke={FIFTEEN} strokeWidth={2} dot={false} isAnimationActive={false} />
            </LineChart>
          </ResponsiveContainer>
        )}
      </Box>
    </Paper>
  )
}

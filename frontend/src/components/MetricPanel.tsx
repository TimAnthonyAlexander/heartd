import { Box, LinearProgress, Paper, Typography } from '@mui/material'
import {
  Area,
  AreaChart,
  CartesianGrid,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts'
import { colors, percentColor } from '../theme'

export interface SeriesPoint {
  t: number
  v: number
}

interface Props {
  title: string
  headline: string
  percent: number
  data: SeriesPoint[]
  dimmed?: boolean
}

function fmtTime(t: number): string {
  const d = new Date(t)
  return `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
}

function ChartTooltip({ active, payload }: any) {
  if (!active || !payload?.length) return null
  const p = payload[0].payload as SeriesPoint
  return (
    <Box
      sx={{
        bgcolor: colors.bg,
        border: `1px solid ${colors.border}`,
        borderRadius: 1,
        px: 1.25,
        py: 0.75,
      }}
    >
      <Typography sx={{ fontSize: 11, color: colors.textDim }}>{fmtTime(p.t)}</Typography>
      <Typography sx={{ fontSize: 13, fontWeight: 600 }}>{p.v.toFixed(1)}%</Typography>
    </Box>
  )
}

export function MetricPanel({ title, headline, percent, data, dimmed }: Props) {
  const color = percentColor(percent)
  const gradId = `grad-${title.replace(/\s/g, '')}`

  return (
    <Paper elevation={0} sx={{ p: 3, borderRadius: 2.5, opacity: dimmed ? 0.45 : 1 }}>
      <Box sx={{ display: 'flex', alignItems: 'baseline', justifyContent: 'space-between' }}>
        <Typography variant="overline" sx={{ color: colors.textDim }}>
          {title}
        </Typography>
        <Typography sx={{ fontSize: 13, fontWeight: 600, color }}>
          {percent.toFixed(0)}%
        </Typography>
      </Box>

      <Typography sx={{ fontSize: 34, fontWeight: 600, letterSpacing: '-0.02em', mt: 0.5 }}>
        {headline}
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

      <Box sx={{ height: 150, mt: 2, mx: -1 }}>
        {data.length < 2 ? (
          <Box sx={{ height: '100%', display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
            <Typography sx={{ fontSize: 12, color: colors.textFaint }}>
              collecting data…
            </Typography>
          </Box>
        ) : (
          <ResponsiveContainer width="100%" height="100%">
            <AreaChart data={data} margin={{ top: 4, right: 8, bottom: 0, left: 8 }}>
              <defs>
                <linearGradient id={gradId} x1="0" y1="0" x2="0" y2="1">
                  <stop offset="0%" stopColor={color} stopOpacity={0.25} />
                  <stop offset="100%" stopColor={color} stopOpacity={0} />
                </linearGradient>
              </defs>
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
              <YAxis hide domain={[0, 100]} />
              <Tooltip content={<ChartTooltip />} cursor={{ stroke: colors.border }} />
              <Area
                type="monotone"
                dataKey="v"
                stroke={color}
                strokeWidth={2}
                fill={`url(#${gradId})`}
                isAnimationActive={false}
              />
            </AreaChart>
          </ResponsiveContainer>
        )}
      </Box>
    </Paper>
  )
}

// PlaceholderPanel reserves a grid slot for metrics the backend doesn't expose
// yet (disk per-mount, network in/out) so the layout doesn't reshuffle later.
export function PlaceholderPanel({ title }: { title: string }) {
  return (
    <Paper
      elevation={0}
      sx={{
        p: 3,
        borderRadius: 2.5,
        borderStyle: 'dashed',
        display: 'flex',
        flexDirection: 'column',
        minHeight: 240,
      }}
    >
      <Typography variant="overline" sx={{ color: colors.textFaint }}>
        {title}
      </Typography>
      <Box sx={{ flex: 1, display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
        <Typography sx={{ fontSize: 13, color: colors.textFaint }}>coming soon</Typography>
      </Box>
    </Paper>
  )
}

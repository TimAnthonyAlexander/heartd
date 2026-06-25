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
import type { NetCurrent } from '../api'
import type { NetPoint } from '../hooks/useNodeData'
import { colors } from '../theme'

interface Props {
  net: NetCurrent | null
  series: NetPoint[]
  dimmed?: boolean
}

const RECV = '#6ea8fe'
const SENT = '#c08cff'

export function formatRate(bytesPerSec: number): string {
  if (bytesPerSec >= 1024 ** 2) return `${(bytesPerSec / 1024 ** 2).toFixed(1)} MB/s`
  if (bytesPerSec >= 1024) return `${(bytesPerSec / 1024).toFixed(0)} KB/s`
  return `${bytesPerSec.toFixed(0)} B/s`
}

function fmtTime(t: number): string {
  const d = new Date(t)
  return `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
}

function NetTooltip({ active, payload }: any) {
  if (!active || !payload?.length) return null
  const p = payload[0].payload as NetPoint
  return (
    <Box sx={{ bgcolor: colors.bg, border: `1px solid ${colors.border}`, borderRadius: 1, px: 1.25, py: 0.75 }}>
      <Typography sx={{ fontSize: 11, color: colors.textDim }}>{fmtTime(p.t)}</Typography>
      <Typography sx={{ fontSize: 12, color: RECV }}>↓ {formatRate(p.recv)}</Typography>
      <Typography sx={{ fontSize: 12, color: SENT }}>↑ {formatRate(p.sent)}</Typography>
    </Box>
  )
}

export function NetworkPanel({ net, series, dimmed }: Props) {
  return (
    <Paper elevation={0} sx={{ p: 3, borderRadius: 2.5, opacity: dimmed ? 0.45 : 1, minHeight: 240 }}>
      <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'baseline' }}>
        <Typography variant="overline" sx={{ color: colors.textDim }}>
          Network
        </Typography>
        {net && (
          <Box sx={{ display: 'flex', gap: 2 }}>
            <Typography sx={{ fontSize: 13, color: RECV, fontWeight: 600 }}>↓ {formatRate(net.recv_rate)}</Typography>
            <Typography sx={{ fontSize: 13, color: SENT, fontWeight: 600 }}>↑ {formatRate(net.sent_rate)}</Typography>
          </Box>
        )}
      </Box>

      <Box sx={{ height: 180, mt: 2, mx: -1 }}>
        {series.length < 2 ? (
          <Box sx={{ height: '100%', display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
            <Typography sx={{ fontSize: 12, color: colors.textFaint }}>collecting data…</Typography>
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
              <Tooltip content={<NetTooltip />} cursor={{ stroke: colors.border }} />
              <Line type="monotone" dataKey="recv" stroke={RECV} strokeWidth={2} dot={false} isAnimationActive={false} />
              <Line type="monotone" dataKey="sent" stroke={SENT} strokeWidth={2} dot={false} isAnimationActive={false} />
            </LineChart>
          </ResponsiveContainer>
        )}
      </Box>
    </Paper>
  )
}

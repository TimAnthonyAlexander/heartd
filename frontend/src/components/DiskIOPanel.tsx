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
import type { DiskIOPoint, DiskIOTotals } from '../hooks/useNodeData'
import { colors } from '../theme'
import { formatRate } from './NetworkPanel'

interface Props {
  io: DiskIOTotals | null
  series: DiskIOPoint[]
  dimmed?: boolean
}

// Read/write throughput share the chart; reuse theme tokens (no hardcoded hex).
const READ = colors.accent
const WRITE = colors.warn

function formatIOPS(opsPerSec: number): string {
  if (opsPerSec >= 1000) return `${(opsPerSec / 1000).toFixed(1)}k IOPS`
  return `${opsPerSec.toFixed(0)} IOPS`
}

function fmtTime(t: number): string {
  const d = new Date(t)
  return `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
}

function IOTooltip({ active, payload }: any) {
  if (!active || !payload?.length) return null
  const p = payload[0].payload as DiskIOPoint
  return (
    <Box sx={{ bgcolor: colors.bg, border: `1px solid ${colors.border}`, borderRadius: 1, px: 1.25, py: 0.75 }}>
      <Typography sx={{ fontSize: 11, color: colors.textDim }}>{fmtTime(p.t)}</Typography>
      <Typography sx={{ fontSize: 12, color: READ }}>
        R {formatRate(p.read)} · {formatIOPS(p.readOps)}
      </Typography>
      <Typography sx={{ fontSize: 12, color: WRITE }}>
        W {formatRate(p.write)} · {formatIOPS(p.writeOps)}
      </Typography>
    </Box>
  )
}

export function DiskIOPanel({ io, series, dimmed }: Props) {
  return (
    <Paper elevation={0} sx={{ p: 3, borderRadius: 2.5, opacity: dimmed ? 0.45 : 1, minHeight: 240 }}>
      <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'baseline' }}>
        <Typography variant="overline" sx={{ color: colors.textDim }}>
          Disk I/O
        </Typography>
        {io && (
          <Box sx={{ display: 'flex', gap: 2 }}>
            <Typography sx={{ fontSize: 13, color: READ, fontWeight: 600 }}>R {formatRate(io.read)}</Typography>
            <Typography sx={{ fontSize: 13, color: WRITE, fontWeight: 600 }}>W {formatRate(io.write)}</Typography>
          </Box>
        )}
      </Box>

      {io && (
        <Box sx={{ display: 'flex', justifyContent: 'flex-end', gap: 2, mt: 0.5 }}>
          <Typography sx={{ fontSize: 11, color: colors.textFaint }}>R {formatIOPS(io.readOps)}</Typography>
          <Typography sx={{ fontSize: 11, color: colors.textFaint }}>W {formatIOPS(io.writeOps)}</Typography>
          {io.devices > 1 && (
            <Typography sx={{ fontSize: 11, color: colors.textFaint }}>{io.devices} disks</Typography>
          )}
        </Box>
      )}

      <Box sx={{ height: 168, mt: 1.5, mx: -1 }}>
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
              <Tooltip content={<IOTooltip />} cursor={{ stroke: colors.border }} />
              <Line type="monotone" dataKey="read" stroke={READ} strokeWidth={2} dot={false} isAnimationActive={false} />
              <Line type="monotone" dataKey="write" stroke={WRITE} strokeWidth={2} dot={false} isAnimationActive={false} />
            </LineChart>
          </ResponsiveContainer>
        )}
      </Box>
    </Paper>
  )
}

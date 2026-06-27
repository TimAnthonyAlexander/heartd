import { Box, Paper, Typography } from '@mui/material'
import {
  Area,
  AreaChart,
  Brush,
  CartesianGrid,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts'
import type { CPUState } from '../api'
import type { CPUStatePoint } from '../hooks/useNodeData'
import { colors, cpuStateColors } from '../theme'

interface Props {
  state: CPUState | null
  series: CPUStatePoint[]
  dimmed?: boolean
}

// STATES is the stack order (bottom-up): the active states first so the busy
// part of the CPU reads from the baseline, with idle filling the remainder on
// top. Each key matches a CPUStatePoint field and a cpuStateColors entry.
// The key is restricted to fields shared by CPUState and CPUStatePoint — i.e.
// the seven state percentages, excluding each type's timestamp field.
type StateKey = keyof CPUState & keyof CPUStatePoint
const STATES: { key: StateKey; label: string }[] = [
  { key: 'user', label: 'user' },
  { key: 'system', label: 'system' },
  { key: 'iowait', label: 'iowait' },
  { key: 'steal', label: 'steal' },
  { key: 'irq', label: 'irq' },
  { key: 'nice', label: 'nice' },
  { key: 'idle', label: 'idle' },
]

function fmtTime(t: number): string {
  const d = new Date(t)
  return `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
}

function fmtPct(v: number): string {
  return `${v.toFixed(1)}%`
}

function CPUStateTooltip({ active, payload }: any) {
  if (!active || !payload?.length) return null
  const p = payload[0].payload as CPUStatePoint
  return (
    <Box sx={{ bgcolor: colors.bg, border: `1px solid ${colors.border}`, borderRadius: 1, px: 1.25, py: 0.75 }}>
      <Typography sx={{ fontSize: 11, color: colors.textDim }}>{fmtTime(p.t)}</Typography>
      {STATES.map(({ key, label }) => {
        const v = p[key] as number
        // Skip states that are effectively zero so the tooltip stays compact;
        // always show idle as the anchor.
        if (v < 0.05 && key !== 'idle') return null
        return (
          <Typography key={key} sx={{ fontSize: 12, color: cpuStateColors[label] }}>
            {label} {fmtPct(v)}
          </Typography>
        )
      })}
    </Box>
  )
}

export function CPUBreakdownPanel({ state, series, dimmed }: Props) {
  return (
    <Paper elevation={0} sx={{ p: 3, borderRadius: 2.5, opacity: dimmed ? 0.45 : 1, minHeight: 240 }}>
      <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'baseline', flexWrap: 'wrap', gap: 1 }}>
        <Typography variant="overline" sx={{ color: colors.textDim }}>
          CPU Breakdown
        </Typography>
        {state && (
          <Box sx={{ display: 'flex', gap: 1.25, flexWrap: 'wrap' }}>
            {STATES.map(({ key, label }) => {
              const v = state[key] as number
              if (v < 0.05 && key !== 'idle') return null
              return (
                <Typography key={key} sx={{ fontSize: 12, color: cpuStateColors[label], fontWeight: 600 }}>
                  {label} {fmtPct(v)}
                </Typography>
              )
            })}
          </Box>
        )}
      </Box>

      <Box sx={{ height: 200, mt: 2, mx: -1 }}>
        {series.length < 2 ? (
          <Box sx={{ height: '100%', display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
            <Typography sx={{ fontSize: 12, color: colors.textFaint }}>collecting data…</Typography>
          </Box>
        ) : (
          <ResponsiveContainer width="100%" height="100%">
            <AreaChart data={series} margin={{ top: 4, right: 8, bottom: 0, left: 8 }}>
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
              <YAxis domain={[0, 100]} hide />
              <Tooltip content={<CPUStateTooltip />} cursor={{ stroke: colors.border }} />
              {STATES.map(({ key, label }) => (
                <Area
                  key={key}
                  type="monotone"
                  stackId="1"
                  dataKey={key}
                  stroke={cpuStateColors[label]}
                  fill={cpuStateColors[label]}
                  fillOpacity={label === 'idle' ? 0.08 : 0.5}
                  strokeWidth={1}
                  isAnimationActive={false}
                />
              ))}
              <Brush
                dataKey="t"
                height={14}
                travellerWidth={8}
                gap={4}
                stroke={colors.textFaint}
                fill={colors.bg}
                tickFormatter={fmtTime}
              />
            </AreaChart>
          </ResponsiveContainer>
        )}
      </Box>
    </Paper>
  )
}

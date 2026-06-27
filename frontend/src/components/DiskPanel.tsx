import { Box, LinearProgress, Paper, Typography } from '@mui/material'
import type { DiskHealth, DiskMount } from '../api'
import { colors, percentColor } from '../theme'
import { DiskHealthSections, severityColor, worstSeverity } from './DiskHealthSections'

interface Props {
  disks: DiskMount[]
  // Software-RAID + SMART health for this node, folded into the Disk card. Both
  // arrays are empty on hosts without RAID/SMART (a dev mac, a plain VM), in
  // which case the card renders exactly as it did before (just usage bars).
  health: DiskHealth
  dimmed?: boolean
}

function formatGB(bytes: number): string {
  const gb = bytes / 1024 ** 3
  if (gb >= 1024) return `${(gb / 1024).toFixed(1)} TB`
  return `${gb.toFixed(0)} GB`
}

const MINUTE = 60
const HOUR = 60 * MINUTE
const DAY = 24 * HOUR
const WEEK = 7 * DAY

// formatETA renders a seconds-until-full value as a compact human span: "~45m",
// "~5h", "~3d", "~2w". It picks the coarsest unit that reads cleanly.
function formatETA(seconds: number): string {
  if (seconds >= WEEK) return `~${Math.round(seconds / WEEK)}w`
  if (seconds >= DAY) return `~${Math.round(seconds / DAY)}d`
  if (seconds >= HOUR) return `~${Math.round(seconds / HOUR)}h`
  if (seconds >= MINUTE) return `~${Math.round(seconds / MINUTE)}m`
  return '<1m'
}

// forecastColor escalates as the disk gets closer to full: error within ~2 days,
// warn within ~7 days, else the mount's own usage color.
function forecastColor(seconds: number, percent: number): string {
  if (seconds < 2 * DAY) return colors.error
  if (seconds < WEEK) return colors.warn
  return percentColor(percent)
}

export function DiskPanel({ disks, health, dimmed }: Props) {
  // The card's header badge takes the worst of {RAID state, SMART rollup}; it is
  // null (and so omitted) on hosts with no disk-health data at all.
  const healthBadge = worstSeverity(health)

  return (
    <Paper elevation={0} sx={{ p: 3, borderRadius: 2.5, opacity: dimmed ? 0.45 : 1, minHeight: 240 }}>
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
        {healthBadge && (
          <Box sx={{ width: 8, height: 8, borderRadius: '50%', bgcolor: severityColor(healthBadge) }} />
        )}
        <Typography variant="overline" sx={{ color: colors.textDim }}>
          Disk
        </Typography>
      </Box>

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
                {d.full_eta_seconds != null && (
                  <Typography
                    sx={{
                      mt: 0.5,
                      fontSize: 11,
                      fontWeight: 600,
                      color: forecastColor(d.full_eta_seconds, d.percent),
                    }}
                  >
                    fills in {formatETA(d.full_eta_seconds)}
                  </Typography>
                )}
              </Box>
            )
          })}
        </Box>
      )}

      <DiskHealthSections health={health} />
    </Paper>
  )
}

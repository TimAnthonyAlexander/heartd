import { Box, IconButton, Paper, Tooltip, Typography } from '@mui/material'
import type { AlertRule } from '../api'
import { colors } from '../theme'
import { describe, severityColor } from './settings/AlertsSection'

interface Props {
  alerts: AlertRule[]
  // Click-to-edit: jumps to the Alerts tab with this rule's form open.
  onEdit: (id: number) => void
}

// AlertsPanel shows the node's configured alert rules on the dashboard. It
// reflects configuration (condition, severity, enabled) — not live firing state,
// which the engine tracks server-side and doesn't yet expose. Enabled rules sort
// first, then by name.
export function AlertsPanel({ alerts, onEdit }: Props) {
  if (alerts.length === 0) {
    return (
      <Box>
        <Header counts={{ enabled: 0, total: 0 }} />
        <Typography sx={{ color: colors.textFaint, fontSize: 13 }}>
          No alerts configured for this node.
        </Typography>
      </Box>
    )
  }

  const sorted = [...alerts].sort(
    (a, b) => Number(b.enabled) - Number(a.enabled) || a.name.localeCompare(b.name),
  )
  const counts = { enabled: alerts.filter((a) => a.enabled).length, total: alerts.length }

  return (
    <Box>
      <Header counts={counts} />
      <Paper elevation={0} sx={{ borderRadius: 2.5, overflow: 'hidden' }}>
        {sorted.map((r, i) => {
          const sev = severityColor(r.severity)
          return (
            <Box
              key={r.id}
              sx={{
                display: 'flex',
                alignItems: 'center',
                gap: 2,
                px: 2.5,
                py: 1.5,
                borderTop: i === 0 ? 'none' : `1px solid ${colors.border}`,
                opacity: r.enabled ? 1 : 0.5,
                '&:hover .alert-edit': { opacity: 1 },
              }}
            >
              {/* A hollow severity ring — color marks severity, not firing. */}
              <Box
                sx={{
                  width: 9,
                  height: 9,
                  borderRadius: '50%',
                  flexShrink: 0,
                  bgcolor: 'transparent',
                  border: `2px solid ${sev}`,
                }}
              />
              <Box sx={{ width: 170, flexShrink: 0 }}>
                <Typography sx={{ fontSize: 14, fontWeight: 600 }} noWrap title={r.name}>
                  {r.name}
                </Typography>
                <Typography
                  sx={{ fontSize: 11, fontWeight: 700, textTransform: 'uppercase', letterSpacing: '0.05em', color: sev }}
                >
                  {r.severity}
                  {!r.enabled && (
                    <Box component="span" sx={{ color: colors.textFaint, fontWeight: 600 }}>
                      {'  · disabled'}
                    </Box>
                  )}
                </Typography>
              </Box>
              <Typography sx={{ fontSize: 13, color: colors.textDim, flex: 1 }} noWrap title={describe(r)}>
                {describe(r)}
              </Typography>
              <Tooltip title="Edit">
                <IconButton
                  className="alert-edit"
                  size="small"
                  onClick={() => onEdit(r.id)}
                  sx={{ color: colors.textDim, opacity: 0, transition: 'opacity 120ms', p: 0.5 }}
                >
                  <Box component="span" sx={{ fontSize: 13, lineHeight: 1 }}>
                    ✎
                  </Box>
                </IconButton>
              </Tooltip>
            </Box>
          )
        })}
      </Paper>
    </Box>
  )
}

function Header({ counts }: { counts: { enabled: number; total: number } }) {
  return (
    <Box sx={{ mb: 1.5 }}>
      <Box sx={{ display: 'flex', alignItems: 'baseline', gap: 2 }}>
        <Typography variant="overline" sx={{ color: colors.textDim }}>
          Alerts
        </Typography>
        {counts.total > 0 && (
          <Typography sx={{ fontSize: 12, color: colors.textFaint }}>
            {counts.enabled} enabled · {counts.total - counts.enabled} off
          </Typography>
        )}
      </Box>
      {/* These are configured rules, not live status — make that explicit so the
          severity colors don't read as "currently firing". */}
      <Typography sx={{ fontSize: 11.5, color: colors.textFaint, mt: 0.25 }}>
        Configured rules — color shows severity, not whether they're firing.
      </Typography>
    </Box>
  )
}

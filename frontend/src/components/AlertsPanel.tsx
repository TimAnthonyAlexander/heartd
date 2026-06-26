import { Box, IconButton, Paper, Tooltip, Typography } from '@mui/material'
import type { ActiveAlert, AlertEvent, AlertRule } from '../api'
import { colors, statusColor } from '../theme'
import { describe, severityColor } from './settings/AlertsSection'

interface Props {
  alerts: AlertRule[]
  active: ActiveAlert[]
  history: AlertEvent[]
  // Click-to-edit: jumps to the Alerts tab with this rule's form open.
  onEdit: (id: number) => void
}

// AlertsPanel shows a node's alert activity: what is firing right now (live from
// the engine), a recent incident history (firing -> recovered transitions), and
// the configured rules. The first two reflect real state; the rule list reflects
// configuration (severity color marks severity, not whether a rule is firing).
export function AlertsPanel({ alerts, active, history, onEdit }: Props) {
  return (
    <Box sx={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
      <FiringNow active={active} />
      <RecentHistory history={history} />
      <ConfiguredRules alerts={alerts} onEdit={onEdit} />
    </Box>
  )
}

// --- Firing now ---

function FiringNow({ active }: { active: ActiveAlert[] }) {
  const sorted = [...active].sort(
    (a, b) =>
      sevRank(b.severity) - sevRank(a.severity) ||
      (a.since || '').localeCompare(b.since || ''),
  )
  return (
    <Box>
      <SectionHeader label="Firing now" count={active.length > 0 ? active.length : undefined} accent={active.length > 0 ? colors.error : undefined} />
      {sorted.length === 0 ? (
        <Box
          sx={{
            display: 'flex',
            alignItems: 'center',
            gap: 1.25,
            px: 2.5,
            py: 1.75,
            borderRadius: 2.5,
            border: `1px solid ${colors.border}`,
            color: colors.textDim,
          }}
        >
          <Box sx={{ width: 9, height: 9, borderRadius: '50%', bgcolor: colors.ok, flexShrink: 0 }} />
          <Typography sx={{ fontSize: 13.5 }}>All clear — nothing firing.</Typography>
        </Box>
      ) : (
        <Paper elevation={0} sx={{ borderRadius: 2.5, overflow: 'hidden' }}>
          {sorted.map((a, i) => {
            const sev = severityColor(a.severity)
            return (
              <Box
                key={`${a.entity}|${a.source}|${a.subject}|${i}`}
                sx={{
                  display: 'flex',
                  alignItems: 'center',
                  gap: 2,
                  px: 2.5,
                  py: 1.5,
                  borderTop: i === 0 ? 'none' : `1px solid ${colors.border}`,
                }}
              >
                {/* A solid severity dot — this one really is firing. */}
                <Box sx={{ width: 9, height: 9, borderRadius: '50%', flexShrink: 0, bgcolor: sev }} />
                <Box sx={{ width: 190, flexShrink: 0 }}>
                  <Typography sx={{ fontSize: 14, fontWeight: 600 }} noWrap title={a.subject}>
                    {a.subject || a.source}
                  </Typography>
                  <Typography
                    sx={{
                      fontSize: 11,
                      fontWeight: 700,
                      textTransform: 'uppercase',
                      letterSpacing: '0.05em',
                      color: sev,
                    }}
                  >
                    {a.severity}
                    {entityLabel(a.entity) && (
                      <Box component="span" sx={{ color: colors.textFaint, fontWeight: 600 }}>
                        {`  · ${entityLabel(a.entity)}`}
                      </Box>
                    )}
                  </Typography>
                </Box>
                <Typography sx={{ fontSize: 13, color: colors.textDim, flex: 1 }} noWrap title={a.detail}>
                  {a.detail}
                </Typography>
                {a.since && (
                  <Typography sx={{ fontSize: 12, color: colors.textFaint, flexShrink: 0 }}>
                    firing for {durationSince(a.since)}
                  </Typography>
                )}
              </Box>
            )
          })}
        </Paper>
      )}
    </Box>
  )
}

// --- Recent history ---

function RecentHistory({ history }: { history: AlertEvent[] }) {
  return (
    <Box>
      <SectionHeader label="Recent alert history" />
      {history.length === 0 ? (
        <Typography sx={{ color: colors.textFaint, fontSize: 13 }}>
          No alert activity recorded yet.
        </Typography>
      ) : (
        <Paper elevation={0} sx={{ borderRadius: 2.5, overflow: 'hidden' }}>
          {history.map((e, i) => {
            const firing = e.state === 'firing'
            const dot = firing ? severityColor(e.severity) : statusColor('ok')
            return (
              <Box
                key={`${e.at}|${e.entity}|${e.state}|${i}`}
                sx={{
                  display: 'flex',
                  alignItems: 'center',
                  gap: 2,
                  px: 2.5,
                  py: 1.25,
                  borderTop: i === 0 ? 'none' : `1px solid ${colors.border}`,
                }}
              >
                <Box sx={{ width: 8, height: 8, borderRadius: '50%', flexShrink: 0, bgcolor: dot }} />
                <Box sx={{ width: 190, flexShrink: 0 }}>
                  <Typography sx={{ fontSize: 13.5, fontWeight: 600 }} noWrap title={e.subject}>
                    {e.subject || e.rule_source}
                  </Typography>
                  {entityLabel(e.entity) && (
                    <Typography sx={{ fontSize: 11.5, color: colors.textFaint }} noWrap>
                      {entityLabel(e.entity)}
                    </Typography>
                  )}
                </Box>
                <Typography sx={{ fontSize: 12.5, color: colors.textDim, flex: 1 }} noWrap title={e.detail}>
                  {e.detail}
                </Typography>
                <Box
                  sx={{
                    flexShrink: 0,
                    px: 1,
                    py: 0.25,
                    borderRadius: 1,
                    fontSize: 10.5,
                    fontWeight: 700,
                    textTransform: 'uppercase',
                    letterSpacing: '0.05em',
                    color: dot,
                    border: `1px solid ${dot}66`,
                    bgcolor: `${dot}14`,
                  }}
                >
                  {firing ? 'firing' : 'recovered'}
                </Box>
                <Typography
                  sx={{ fontSize: 12, color: colors.textFaint, flexShrink: 0, width: 76, textAlign: 'right' }}
                  title={new Date(e.at).toLocaleString()}
                >
                  {relativeTime(e.at)}
                </Typography>
              </Box>
            )
          })}
        </Paper>
      )}
    </Box>
  )
}

// --- Configured rules (unchanged behavior, kept below the live views) ---

function ConfiguredRules({ alerts, onEdit }: { alerts: AlertRule[]; onEdit: (id: number) => void }) {
  if (alerts.length === 0) {
    return (
      <Box>
        <RulesHeader counts={{ enabled: 0, total: 0 }} />
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
      <RulesHeader counts={counts} />
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

function RulesHeader({ counts }: { counts: { enabled: number; total: number } }) {
  return (
    <Box sx={{ mb: 1.5 }}>
      <Box sx={{ display: 'flex', alignItems: 'baseline', gap: 2 }}>
        <Typography variant="overline" sx={{ color: colors.textDim }}>
          Configured rules
        </Typography>
        {counts.total > 0 && (
          <Typography sx={{ fontSize: 12, color: colors.textFaint }}>
            {counts.enabled} enabled · {counts.total - counts.enabled} off
          </Typography>
        )}
      </Box>
      <Typography sx={{ fontSize: 11.5, color: colors.textFaint, mt: 0.25 }}>
        Color shows severity, not whether they're firing.
      </Typography>
    </Box>
  )
}

function SectionHeader({ label, count, accent }: { label: string; count?: number; accent?: string }) {
  return (
    <Box sx={{ display: 'flex', alignItems: 'baseline', gap: 1.5, mb: 1.5 }}>
      <Typography variant="overline" sx={{ color: colors.textDim }}>
        {label}
      </Typography>
      {count !== undefined && (
        <Box
          sx={{
            px: 0.9,
            py: 0.1,
            borderRadius: 1,
            fontSize: 11,
            fontWeight: 700,
            color: accent ?? colors.textDim,
            border: `1px solid ${(accent ?? colors.textDim) + '66'}`,
            bgcolor: `${accent ?? colors.textDim}14`,
          }}
        >
          {count}
        </Box>
      )}
    </Box>
  )
}

// --- helpers ---

function entityLabel(entity: string): string {
  return entity === '*' ? '' : entity
}

function sevRank(severity: string): number {
  return severity === 'critical' ? 2 : 1
}

// durationSince renders a compact elapsed time since an ISO timestamp, e.g.
// "3m", "2h 5m", "4d". Used for "firing for X".
function durationSince(iso: string): string {
  const ms = Date.now() - new Date(iso).getTime()
  if (!isFinite(ms) || ms < 0) return '0s'
  const s = Math.floor(ms / 1000)
  if (s < 60) return `${s}s`
  const m = Math.floor(s / 60)
  if (m < 60) return `${m}m`
  const h = Math.floor(m / 60)
  if (h < 24) return `${h}h ${m % 60}m`
  const d = Math.floor(h / 24)
  return `${d}d ${h % 24}h`
}

// relativeTime renders a short "time ago" for the history log.
function relativeTime(iso: string): string {
  const ms = Date.now() - new Date(iso).getTime()
  if (!isFinite(ms)) return ''
  if (ms < 0) return 'just now'
  return `${durationSince(iso)} ago`
}

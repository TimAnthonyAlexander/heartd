import { Box, IconButton, Paper, Tooltip, Typography } from '@mui/material'
import type { Check } from '../api'
import { colors, statusColor } from '../theme'

interface Props {
  checks: Check[]
  // Click-to-edit: jumps to the Checks tab with this check's form open (matched
  // by name). Omit to hide the edit affordance.
  onEdit?: (name: string) => void
}

function relativeTime(iso: string): string {
  if (!iso) return 'never'
  const secs = Math.max(0, Math.round((Date.now() - new Date(iso).getTime()) / 1000))
  if (secs < 60) return `${secs}s ago`
  if (secs < 3600) return `${Math.round(secs / 60)}m ago`
  return `${Math.round(secs / 3600)}h ago`
}

// Sort failing first, then unknown, then ok; stable by name within a group.
const order: Record<Check['status'], number> = { failing: 0, unknown: 1, ok: 2 }

export function ChecksTable({ checks, onEdit }: Props) {
  if (checks.length === 0) {
    return (
      <Typography sx={{ color: colors.textFaint, fontSize: 13 }}>
        No checks configured for this node.
      </Typography>
    )
  }

  const sorted = [...checks].sort(
    (a, b) => order[a.status] - order[b.status] || a.name.localeCompare(b.name),
  )
  const counts = checks.reduce(
    (acc, c) => ({ ...acc, [c.status]: acc[c.status] + 1 }),
    { ok: 0, failing: 0, unknown: 0 },
  )

  return (
    <Box>
      <Box sx={{ display: 'flex', alignItems: 'baseline', gap: 2, mb: 1.5 }}>
        <Typography variant="overline" sx={{ color: colors.textDim }}>
          Checks
        </Typography>
        <Typography sx={{ fontSize: 12, color: colors.textFaint }}>
          {counts.ok} ok · {counts.failing} failing · {counts.unknown} unknown
        </Typography>
      </Box>

      <Paper elevation={0} sx={{ borderRadius: 2.5, overflow: 'hidden' }}>
        {sorted.map((c, i) => {
          const color = statusColor(c.status)
          return (
            <Box
              key={c.name}
              sx={{
                display: 'flex',
                alignItems: 'center',
                gap: 2,
                px: 2.5,
                py: 1.5,
                borderTop: i === 0 ? 'none' : `1px solid ${colors.border}`,
                borderLeft: `2px solid ${c.status === 'failing' ? color : 'transparent'}`,
              ...(onEdit && { '&:hover .check-edit': { opacity: 1 } }),
              }}
            >
              <Box sx={{ width: 8, height: 8, borderRadius: '50%', bgcolor: color, flexShrink: 0 }} />
              <Box sx={{ width: 170, flexShrink: 0 }}>
                <Typography sx={{ fontSize: 14, fontWeight: 600 }} noWrap>
                  {c.name}
                </Typography>
                <Typography sx={{ fontSize: 11, color: colors.textFaint }}>{c.type}</Typography>
              </Box>
              <Typography sx={{ fontSize: 13, color: colors.textDim, flex: 1 }} noWrap title={c.detail}>
                {c.detail || '—'}
              </Typography>
              {c.latency_ms > 0 && (
                <Typography sx={{ fontSize: 12, color: colors.textFaint, width: 56, textAlign: 'right' }}>
                  {c.latency_ms}ms
                </Typography>
              )}
              <Typography sx={{ fontSize: 12, color: colors.textFaint, width: 64, textAlign: 'right' }}>
                {relativeTime(c.last_checked)}
              </Typography>
              {onEdit && (
                <Tooltip title="Edit">
                  <IconButton
                    className="check-edit"
                    size="small"
                    onClick={() => onEdit(c.name)}
                    sx={{ color: colors.textDim, opacity: 0, transition: 'opacity 120ms', p: 0.5 }}
                  >
                    <Box component="span" sx={{ fontSize: 13, lineHeight: 1 }}>
                      ✎
                    </Box>
                  </IconButton>
                </Tooltip>
              )}
            </Box>
          )
        })}
      </Paper>
    </Box>
  )
}

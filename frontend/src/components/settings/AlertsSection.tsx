import { useState } from 'react'
import { Box, Typography } from '@mui/material'
import type { AlertRule } from '../../api'
import { createAlert, deleteAlert, updateAlert } from '../../api'
import { colors } from '../../theme'
import { formatInterval } from './shared'
import { AlertRuleForm, sourceMeta } from './AlertRuleForm'
import { Toggle } from '../Toggle'

interface Props {
  nodeName: string
  alerts: AlertRule[]
  onChange: (alerts: AlertRule[]) => void
}

const SYMBOL: Record<string, string> = { '>=': '≥', '>': '>', '<=': '≤', '<': '<' }

// describe renders the condition in plain language, e.g. "CPU ≥ 90%" or
// "Disk / ≥ 95%" or "Service check failing".
function describe(r: AlertRule): string {
  const meta = sourceMeta(r.source)
  const target = meta.entity && r.entity && r.entity !== '*' ? ` ${r.entity}` : meta.entity ? ' (any)' : ''
  if (meta.numeric) {
    const value = meta.scale ? r.threshold / meta.scale : r.threshold
    return `${meta.label}${target} ${SYMBOL[r.comparator] ?? r.comparator} ${value}${meta.unit ?? ''}`
  }
  return `${meta.label}${target}`
}

function severityColor(severity: string): string {
  return severity === 'critical' ? colors.error : colors.warn
}

export function AlertsSection({ nodeName, alerts, onChange }: Props) {
  const [formOpen, setFormOpen] = useState(false)
  const [editing, setEditing] = useState<AlertRule | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [pending, setPending] = useState<number | null>(null)

  const openCreate = () => {
    setEditing(null)
    setError(null)
    setFormOpen(true)
  }
  const openEdit = (r: AlertRule) => {
    setEditing(r)
    setError(null)
    setFormOpen(true)
  }

  const submit = async (rule: AlertRule) => {
    if (rule.id === 0) {
      const { id: _id, ...payload } = rule
      const created = await createAlert(nodeName, payload)
      onChange([...alerts, created])
    } else {
      await updateAlert(nodeName, rule)
      onChange(alerts.map((a) => (a.id === rule.id ? rule : a)))
    }
    setFormOpen(false)
  }

  const toggle = async (r: AlertRule, enabled: boolean) => {
    setError(null)
    setPending(r.id)
    const next = { ...r, enabled }
    try {
      await updateAlert(nodeName, next)
      onChange(alerts.map((a) => (a.id === r.id ? next : a)))
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Update failed')
    } finally {
      setPending(null)
    }
  }

  const remove = async (r: AlertRule) => {
    if (!window.confirm(`Delete alert "${r.name}"?`)) return
    setError(null)
    try {
      await deleteAlert(nodeName, r.id)
      onChange(alerts.filter((a) => a.id !== r.id))
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Delete failed')
    }
  }

  const sorted = [...alerts].sort((a, b) => a.name.localeCompare(b.name))

  return (
    <Box>
      <Box sx={{ display: 'flex', alignItems: 'flex-end', justifyContent: 'space-between', mb: 2.5 }}>
        <Box>
          <Typography sx={{ fontSize: 16, fontWeight: 600, letterSpacing: '-0.01em', color: colors.text }}>
            Alerts
          </Typography>
          <Typography sx={{ fontSize: 13, color: colors.textFaint, mt: 0.5 }}>
            Fire a notification when a condition holds. Add as many as you like — each is checked live.
          </Typography>
        </Box>
        <AddButton onClick={openCreate} />
      </Box>

      {error && <Typography sx={{ fontSize: 13, color: colors.error, mb: 1.5 }}>{error}</Typography>}

      {sorted.length === 0 ? (
        <Box
          sx={{
            border: `1px dashed ${colors.border}`,
            borderRadius: 2,
            py: 5,
            textAlign: 'center',
            color: colors.textFaint,
            fontSize: 13.5,
          }}
        >
          No alerts yet. Create one to get notified when something needs attention.
        </Box>
      ) : (
        <Box>
          {sorted.map((r, i) => {
            const sev = severityColor(r.severity)
            const timing: string[] = []
            if (r.for_seconds > 0) timing.push(`for ${formatInterval(r.for_seconds)}`)
            if (r.recover_grace_seconds > 0) timing.push(`grace ${formatInterval(r.recover_grace_seconds)}`)
            return (
              <Box
                key={r.id}
                sx={{
                  display: 'flex',
                  alignItems: 'center',
                  gap: 1.75,
                  px: 1.5,
                  py: 1.5,
                  mx: -1.5,
                  borderRadius: 2,
                  borderTop: i === 0 ? 'none' : `1px solid ${colors.border}`,
                  opacity: r.enabled ? 1 : 0.5,
                  transition: 'background-color 120ms',
                  '&:hover': { bgcolor: colors.panelHover },
                  '&:hover .alert-actions': { opacity: 1 },
                }}
              >
                {/* Severity accent */}
                <Box
                  sx={{
                    width: 9,
                    height: 9,
                    borderRadius: '50%',
                    flexShrink: 0,
                    bgcolor: r.enabled ? sev : 'transparent',
                    border: `2px solid ${sev}`,
                    boxShadow: r.enabled ? `0 0 0 3px ${sev}22` : 'none',
                  }}
                />

                <Box sx={{ flex: 1, minWidth: 0 }}>
                  <Box sx={{ display: 'flex', alignItems: 'baseline', gap: 1 }}>
                    <Typography sx={{ fontSize: 14, fontWeight: 600 }} noWrap title={r.name}>
                      {r.name}
                    </Typography>
                    <Typography
                      sx={{ fontSize: 10.5, fontWeight: 700, textTransform: 'uppercase', letterSpacing: '0.05em', color: sev }}
                    >
                      {r.severity}
                    </Typography>
                  </Box>
                  <Typography sx={{ fontSize: 12.5, color: colors.textDim, mt: 0.25 }} noWrap>
                    {describe(r)}
                    {timing.length > 0 && (
                      <Box component="span" sx={{ color: colors.textFaint }}>
                        {'  ·  ' + timing.join('  ·  ')}
                      </Box>
                    )}
                  </Typography>
                </Box>

                <Toggle
                  checked={r.enabled}
                  disabled={pending === r.id}
                  onChange={(v) => toggle(r, v)}
                  aria-label={`Enable ${r.name}`}
                />

                <Box
                  className="alert-actions"
                  sx={{ display: 'flex', gap: 0.5, opacity: 0, transition: 'opacity 120ms' }}
                >
                  <RowAction label="Edit" glyph="✎" onClick={() => openEdit(r)} />
                  <RowAction label="Delete" glyph="✕" onClick={() => remove(r)} />
                </Box>
              </Box>
            )
          })}
        </Box>
      )}

      {formOpen && (
        <AlertRuleForm open={formOpen} initial={editing} onSubmit={submit} onClose={() => setFormOpen(false)} />
      )}
    </Box>
  )
}

function AddButton({ onClick }: { onClick: () => void }) {
  return (
    <Box
      component="button"
      type="button"
      onClick={onClick}
      sx={{
        appearance: 'none',
        cursor: 'pointer',
        flexShrink: 0,
        fontFamily: 'inherit',
        fontSize: 13,
        fontWeight: 600,
        color: colors.text,
        bgcolor: 'rgba(255,255,255,0.06)',
        border: `1px solid ${colors.border}`,
        borderRadius: '8px',
        px: 1.5,
        py: 0.75,
        display: 'inline-flex',
        alignItems: 'center',
        gap: 0.75,
        transition: 'background-color 120ms',
        '&:hover': { bgcolor: 'rgba(255,255,255,0.1)' },
      }}
    >
      <Box component="span" sx={{ fontSize: 15, lineHeight: 1, color: colors.accent }}>
        +
      </Box>
      New alert
    </Box>
  )
}

function RowAction({ label, glyph, onClick }: { label: string; glyph: string; onClick: () => void }) {
  return (
    <Box
      component="button"
      type="button"
      title={label}
      aria-label={label}
      onClick={onClick}
      sx={{
        appearance: 'none',
        cursor: 'pointer',
        width: 26,
        height: 26,
        borderRadius: '7px',
        border: 'none',
        bgcolor: 'transparent',
        color: colors.textDim,
        fontSize: 13,
        display: 'inline-flex',
        alignItems: 'center',
        justifyContent: 'center',
        transition: 'background-color 120ms, color 120ms',
        '&:hover': { bgcolor: 'rgba(255,255,255,0.08)', color: colors.text },
      }}
    >
      {glyph}
    </Box>
  )
}

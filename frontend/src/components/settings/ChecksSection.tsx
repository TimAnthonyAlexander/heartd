import { useEffect, useState } from 'react'
import { Box, Button, Chip, IconButton, Switch, Tooltip, Typography } from '@mui/material'
import type { CheckConfig } from '../../api'
import { createCheck, deleteCheck, updateCheck } from '../../api'
import { colors } from '../../theme'
import { Section, formatInterval } from './shared'
import { CheckForm } from './CheckForm'

interface Props {
  nodeName: string
  checks: CheckConfig[]
  onChange: (checks: CheckConfig[]) => void
  // When set (e.g. from a dashboard "Edit" click), open the form for the check
  // with this name once it's present, then call onEditConsumed. Matched by name
  // because the dashboard knows checks by their runtime status, not their id.
  editName?: string
  onEditConsumed?: () => void
}

// The defining parameter for a check, shown in the row summary.
function checkParam(c: CheckConfig): string {
  switch (c.type) {
    case 'http':
      return `${c.method || 'GET'} ${c.url}`.trim()
    case 'tcp':
      return c.port ? `${c.host}:${c.port}` : c.host
    case 'process':
      return c.process
    case 'shell':
      return c.command
    default:
      return ''
  }
}

export function ChecksSection({ nodeName, checks, onChange, editName, onEditConsumed }: Props) {
  const [formOpen, setFormOpen] = useState(false)
  const [editing, setEditing] = useState<CheckConfig | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [pendingToggle, setPendingToggle] = useState<number | null>(null)

  const openCreate = () => {
    setEditing(null)
    setError(null)
    setFormOpen(true)
  }

  const openEdit = (c: CheckConfig) => {
    setEditing(c)
    setError(null)
    setFormOpen(true)
  }

  // Honor a deep-link edit request from the dashboard: open the matching check's
  // form once checks have loaded, then consume the request (even if unmatched, so
  // a stale name doesn't linger).
  useEffect(() => {
    if (editName == null) return
    const c = checks.find((x) => x.name === editName)
    if (c) openEdit(c)
    onEditConsumed?.()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [editName, checks])

  const submit = async (check: CheckConfig) => {
    setError(null)
    if (check.id === 0) {
      const { id: _id, ...payload } = check
      const created = await createCheck(nodeName, payload)
      onChange([...checks, created])
    } else {
      await updateCheck(nodeName, check)
      onChange(checks.map((c) => (c.id === check.id ? check : c)))
    }
    setFormOpen(false)
  }

  const toggle = async (c: CheckConfig, enabled: boolean) => {
    setError(null)
    setPendingToggle(c.id)
    const next = { ...c, enabled }
    try {
      await updateCheck(nodeName, next)
      onChange(checks.map((x) => (x.id === c.id ? next : x)))
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Update failed')
    } finally {
      setPendingToggle(null)
    }
  }

  const remove = async (c: CheckConfig) => {
    if (!window.confirm(`Delete check "${c.name}"? This cannot be undone.`)) return
    setError(null)
    try {
      await deleteCheck(nodeName, c.id)
      onChange(checks.filter((x) => x.id !== c.id))
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Delete failed')
    }
  }

  const sorted = [...checks].sort((a, b) => a.name.localeCompare(b.name))

  return (
    <Section
      label="Checks"
      description="Health probes run on a schedule. New checks start enabled."
      actions={
        <Button variant="contained" size="small" onClick={openCreate}>
          Add check
        </Button>
      }
    >
      {error && (
        <Typography sx={{ fontSize: 13, color: colors.error, mb: 1.5 }}>{error}</Typography>
      )}

      {sorted.length === 0 ? (
        <Typography sx={{ color: colors.textFaint, fontSize: 13 }}>
          No checks configured yet.
        </Typography>
      ) : (
        <Box sx={{ border: `1px solid ${colors.border}`, borderRadius: 2, overflow: 'hidden' }}>
          {sorted.map((c, i) => (
            <Box
              key={c.id}
              sx={{
                display: 'flex',
                alignItems: 'center',
                gap: 2,
                px: 2,
                py: 1.25,
                borderTop: i === 0 ? 'none' : `1px solid ${colors.border}`,
                opacity: c.enabled ? 1 : 0.55,
              }}
            >
              <Box sx={{ width: 160, flexShrink: 0 }}>
                <Typography sx={{ fontSize: 14, fontWeight: 600 }} noWrap>
                  {c.name}
                </Typography>
                <Chip
                  label={c.type}
                  size="small"
                  sx={{
                    height: 18,
                    fontSize: 11,
                    mt: 0.25,
                    bgcolor: colors.bg,
                    border: `1px solid ${colors.border}`,
                    color: colors.textDim,
                  }}
                />
              </Box>
              <Typography
                sx={{ fontSize: 13, color: colors.textDim, flex: 1, fontFamily: 'monospace' }}
                noWrap
                title={checkParam(c)}
              >
                {checkParam(c) || '—'}
              </Typography>
              <Typography
                sx={{ fontSize: 12, color: colors.textFaint, width: 48, textAlign: 'right' }}
              >
                {formatInterval(c.interval_sec)}
              </Typography>
              <Switch
                size="small"
                checked={c.enabled}
                disabled={pendingToggle === c.id}
                onChange={(e) => toggle(c, e.target.checked)}
              />
              <Tooltip title="Edit">
                <IconButton size="small" onClick={() => openEdit(c)} sx={{ color: colors.textDim }}>
                  <EditGlyph />
                </IconButton>
              </Tooltip>
              <Tooltip title="Delete">
                <IconButton size="small" onClick={() => remove(c)} sx={{ color: colors.textDim }}>
                  <DeleteGlyph />
                </IconButton>
              </Tooltip>
            </Box>
          ))}
        </Box>
      )}

      {formOpen && (
        <CheckForm
          open={formOpen}
          initial={editing}
          onSubmit={submit}
          onClose={() => setFormOpen(false)}
        />
      )}
    </Section>
  )
}

// Lightweight inline glyphs to avoid an icon-library dependency.
function EditGlyph() {
  return (
    <Box component="span" sx={{ fontSize: 15, lineHeight: 1 }}>
      ✎
    </Box>
  )
}

function DeleteGlyph() {
  return (
    <Box component="span" sx={{ fontSize: 15, lineHeight: 1 }}>
      ✕
    </Box>
  )
}

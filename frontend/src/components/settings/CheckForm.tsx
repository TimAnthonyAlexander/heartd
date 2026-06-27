import { useState } from 'react'
import {
  Box,
  Button,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  FormControlLabel,
  MenuItem,
  Stack,
  Switch,
  TextField,
  Typography,
} from '@mui/material'
import type { CheckConfig } from '../../api'
import { colors } from '../../theme'
import { TemplateChips, checkTemplates, type CheckTemplate } from './templates'

type CheckType = CheckConfig['type']

// A fresh check defaults to enabled so it actually runs once created.
function emptyCheck(): CheckConfig {
  return {
    id: 0,
    name: '',
    type: 'http',
    interval_sec: 30,
    timeout_sec: 10,
    url: '',
    method: 'GET',
    host: '',
    port: 0,
    process: '',
    command: '',
    enabled: true,
    accept_any: false,
    accepted_statuses: [],
    user_agent: '',
  }
}

// parseStatusCodes turns free-form input like "200, 401 403" into a deduped,
// ordered list of codes. Non-numeric fragments are dropped so partial typing
// never throws.
function parseStatusCodes(raw: string): number[] {
  const seen = new Set<number>()
  for (const part of raw.split(/[,\s]+/)) {
    if (!part) continue
    const n = Number(part)
    if (Number.isInteger(n)) seen.add(n)
  }
  return [...seen].sort((a, b) => a - b)
}

interface Props {
  open: boolean
  // When editing, the existing check; null when creating.
  initial: CheckConfig | null
  // Submits the form. `id === 0` means create.
  onSubmit: (check: CheckConfig) => Promise<void>
  onClose: () => void
}

export function CheckForm({ open, initial, onSubmit, onClose }: Props) {
  const [check, setCheck] = useState<CheckConfig>(initial ?? emptyCheck())
  // Raw text mirror of accepted_statuses so partial typing ("200, ") survives.
  const [statusText, setStatusText] = useState(() => (initial?.accepted_statuses ?? []).join(', '))
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  const set =
    <K extends keyof CheckConfig>(key: K) =>
    (value: CheckConfig[K]) =>
      setCheck((prev) => ({ ...prev, [key]: value }))

  // Seed the form from a template. The template name is only applied when the
  // name field is still empty, so it never clobbers something you've typed.
  const applyTemplate = (t: CheckTemplate) =>
    setCheck((prev) => {
      const next = {
        ...prev,
        ...t.values,
        name: prev.name.trim() ? prev.name : t.values.name ?? prev.name,
      }
      if (t.values.accepted_statuses) setStatusText(next.accepted_statuses.join(', '))
      return next
    })

  const submit = async () => {
    if (!check.name.trim()) {
      setError('Name is required.')
      return
    }
    setBusy(true)
    setError(null)
    try {
      await onSubmit({ ...check, name: check.name.trim() })
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Save failed')
      setBusy(false)
    }
  }

  return (
    <Dialog open={open} onClose={onClose} maxWidth="sm" fullWidth>
      <DialogTitle>{check.id === 0 ? 'Add check' : `Edit ${initial?.name ?? 'check'}`}</DialogTitle>
      <DialogContent>
        <Stack spacing={2} sx={{ mt: 1 }}>
          {check.id === 0 && (
            <TemplateChips label="Start from a template" items={checkTemplates} onPick={applyTemplate} />
          )}
          <TextField
            label="Name"
            size="small"
            value={check.name}
            onChange={(e) => set('name')(e.target.value)}
            autoFocus
            fullWidth
          />
          <Box sx={{ display: 'grid', gridTemplateColumns: '1fr 1fr 1fr', gap: 2 }}>
            <TextField
              label="Type"
              select
              size="small"
              value={check.type}
              onChange={(e) => set('type')(e.target.value as CheckType)}
              fullWidth
            >
              <MenuItem value="http">http</MenuItem>
              <MenuItem value="tcp">tcp</MenuItem>
              <MenuItem value="process">process</MenuItem>
              <MenuItem value="shell">shell</MenuItem>
            </TextField>
            <TextField
              label="Interval (s)"
              type="number"
              size="small"
              value={check.interval_sec}
              onChange={(e) => set('interval_sec')(Math.round(Number(e.target.value)) || 0)}
              slotProps={{ htmlInput: { min: 1, step: 1 } }}
              fullWidth
            />
            <TextField
              label="Timeout (s)"
              type="number"
              size="small"
              value={check.timeout_sec}
              onChange={(e) => set('timeout_sec')(Math.round(Number(e.target.value)) || 0)}
              slotProps={{ htmlInput: { min: 1, step: 1 } }}
              fullWidth
            />
          </Box>

          {check.type === 'http' && (
            <>
              <Box sx={{ display: 'grid', gridTemplateColumns: '2fr 1fr', gap: 2 }}>
                <TextField
                  label="URL"
                  size="small"
                  value={check.url}
                  onChange={(e) => set('url')(e.target.value)}
                  placeholder="https://example.com/health"
                  fullWidth
                />
                <TextField
                  label="Method"
                  size="small"
                  value={check.method}
                  onChange={(e) => set('method')(e.target.value)}
                  placeholder="GET"
                  fullWidth
                />
              </Box>
              <FormControlLabel
                control={
                  <Switch
                    checked={check.accept_any}
                    onChange={(e) => set('accept_any')(e.target.checked)}
                  />
                }
                label="Accept any HTTP response as healthy"
              />
              <TextField
                label="Accepted status codes"
                size="small"
                value={statusText}
                onChange={(e) => {
                  setStatusText(e.target.value)
                  set('accepted_statuses')(parseStatusCodes(e.target.value))
                }}
                placeholder="200, 401, 403"
                helperText={
                  check.accept_any
                    ? 'Ignored while “Accept any HTTP response” is on.'
                    : 'Comma-separated. Leave blank for the default (2xx only).'
                }
                disabled={check.accept_any}
                fullWidth
              />
              <TextField
                label="User-Agent (optional)"
                size="small"
                value={check.user_agent}
                onChange={(e) => set('user_agent')(e.target.value)}
                placeholder="heartd/<version> (health-check)"
                fullWidth
              />
            </>
          )}

          {check.type === 'tcp' && (
            <Box sx={{ display: 'grid', gridTemplateColumns: '2fr 1fr', gap: 2 }}>
              <TextField
                label="Host"
                size="small"
                value={check.host}
                onChange={(e) => set('host')(e.target.value)}
                placeholder="db.internal"
                fullWidth
              />
              <TextField
                label="Port"
                type="number"
                size="small"
                value={check.port}
                onChange={(e) => set('port')(Math.round(Number(e.target.value)) || 0)}
                slotProps={{ htmlInput: { min: 0, max: 65535, step: 1 } }}
                fullWidth
              />
            </Box>
          )}

          {check.type === 'process' && (
            <TextField
              label="Process name"
              size="small"
              value={check.process}
              onChange={(e) => set('process')(e.target.value)}
              placeholder="nginx"
              fullWidth
            />
          )}

          {check.type === 'shell' && (
            <TextField
              label="Command"
              size="small"
              value={check.command}
              onChange={(e) => set('command')(e.target.value)}
              placeholder="/usr/local/bin/check.sh"
              fullWidth
            />
          )}

          <FormControlLabel
            control={
              <Switch checked={check.enabled} onChange={(e) => set('enabled')(e.target.checked)} />
            }
            label="Enabled"
          />

          {error && <Typography sx={{ fontSize: 13, color: colors.error }}>{error}</Typography>}
        </Stack>
      </DialogContent>
      <DialogActions sx={{ px: 3, pb: 2 }}>
        <Button onClick={onClose} size="small" color="inherit">
          Cancel
        </Button>
        <Button onClick={submit} size="small" variant="contained" disabled={busy}>
          {busy ? 'Saving…' : check.id === 0 ? 'Create' : 'Save'}
        </Button>
      </DialogActions>
    </Dialog>
  )
}

import { useState, type ReactNode } from 'react'
import {
  Box,
  Button,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  InputAdornment,
  MenuItem,
  Stack,
  TextField,
  Typography,
} from '@mui/material'
import type { AlertRule, AlertSource } from '../../api'
import { colors } from '../../theme'
import { SegmentedTabs } from '../SegmentedTabs'
import { Toggle } from '../Toggle'
import { formatInterval } from './shared'

interface SourceMeta {
  value: AlertSource
  label: string
  numeric: boolean
  unit?: string
  entity?: { label: string; placeholder: string }
  scale?: number
  group: string
}

export const SOURCES: SourceMeta[] = [
  { value: 'cpu', label: 'CPU usage', numeric: true, unit: '%', group: 'Metrics' },
  { value: 'mem', label: 'Memory usage', numeric: true, unit: '%', group: 'Metrics' },
  { value: 'disk', label: 'Disk usage', numeric: true, unit: '%', entity: { label: 'Mount', placeholder: '/  (or * for any)' }, group: 'Metrics' },
  { value: 'net_recv', label: 'Network in', numeric: true, unit: 'MB/s', scale: 1e6, group: 'Network' },
  { value: 'net_sent', label: 'Network out', numeric: true, unit: 'MB/s', scale: 1e6, group: 'Network' },
  { value: 'check_status', label: 'Service check failing', numeric: false, entity: { label: 'Check', placeholder: 'check name (or * for any)' }, group: 'Service checks' },
  { value: 'check_latency', label: 'Service check latency', numeric: true, unit: 'ms', entity: { label: 'Check', placeholder: 'check name (or * for any)' }, group: 'Service checks' },
  { value: 'peer', label: 'Node unreachable', numeric: false, entity: { label: 'Node', placeholder: 'peer name (or * for any)' }, group: 'Cluster' },
  { value: 'nodata', label: 'No data (stale)', numeric: true, unit: 's', entity: { label: 'Node', placeholder: 'peer name (or * for any)' }, group: 'Cluster' },
]

export function sourceMeta(source: AlertSource): SourceMeta {
  return SOURCES.find((s) => s.value === source) ?? SOURCES[0]
}

const SYMBOL: Record<string, string> = { '>=': '≥', '>': '>', '<=': '≤', '<': '<' }
const COMPARATORS = [
  { value: '>=', label: '≥' },
  { value: '>', label: '>' },
  { value: '<=', label: '≤' },
  { value: '<', label: '<' },
]
const SEVERITIES = [
  { value: 'warning', label: 'Warning' },
  { value: 'critical', label: 'Critical' },
]

interface Props {
  open: boolean
  initial: AlertRule | null // null = create
  onSubmit: (rule: AlertRule) => Promise<void>
  onClose: () => void
}

export function AlertRuleForm({ open, initial, onSubmit, onClose }: Props) {
  const isEdit = initial !== null
  const [name, setName] = useState(initial?.name ?? '')
  const [source, setSource] = useState<AlertSource>(initial?.source ?? 'cpu')
  const [entity, setEntity] = useState(initial?.entity ?? '')
  const [comparator, setComparator] = useState(initial?.comparator || '>=')
  const [severity, setSeverity] = useState<'warning' | 'critical'>(initial?.severity ?? 'warning')
  const [enabled, setEnabled] = useState(initial?.enabled ?? true)
  const [forSec, setForSec] = useState(String(initial?.for_seconds ?? 0))
  const [graceSec, setGraceSec] = useState(String(initial?.recover_grace_seconds ?? 0))
  const [error, setError] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)

  const meta = sourceMeta(source)
  const initialThreshold =
    initial && meta.scale ? initial.threshold / meta.scale : (initial?.threshold ?? 0)
  const [threshold, setThreshold] = useState(String(initialThreshold))

  const save = async () => {
    if (name.trim() === '') {
      setError('Give the alert a name.')
      return
    }
    const scale = meta.scale ?? 1
    const rule: AlertRule = {
      id: initial?.id ?? 0,
      name: name.trim(),
      enabled,
      source,
      entity: meta.entity ? entity.trim() || '*' : '',
      comparator: meta.numeric ? comparator : '',
      threshold: meta.numeric ? Number(threshold) * scale : 0,
      for_seconds: Math.max(0, Math.round(Number(forSec) || 0)),
      recover_grace_seconds: Math.max(0, Math.round(Number(graceSec) || 0)),
      severity,
    }
    setError(null)
    setSaving(true)
    try {
      await onSubmit(rule)
      onClose()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Save failed')
    } finally {
      setSaving(false)
    }
  }

  // Plain-English summary shown live as you build the rule.
  const target = meta.entity && entity.trim() && entity.trim() !== '*' ? ` ${entity.trim()}` : meta.entity ? ' (any)' : ''
  const condition = meta.numeric
    ? `${meta.label}${target} ${SYMBOL[comparator] ?? comparator} ${threshold || 0}${meta.unit ?? ''}`
    : `${meta.label}${target}`
  const forN = Math.round(Number(forSec) || 0)

  const groups = [...new Set(SOURCES.map((s) => s.group))]

  return (
    <Dialog open={open} onClose={onClose} maxWidth="sm" fullWidth>
      <DialogTitle sx={{ pb: 1 }}>{isEdit ? 'Edit alert' : 'New alert'}</DialogTitle>
      <DialogContent>
        <Stack spacing={2.5} sx={{ mt: 0.5 }}>
          <TextField
            label="Name"
            size="small"
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="High CPU on the API box"
            autoFocus
            fullWidth
          />

          <TextField
            select
            label="Trigger"
            size="small"
            value={source}
            onChange={(e) => {
              setSource(e.target.value as AlertSource)
              setEntity('')
            }}
            fullWidth
          >
            {groups.flatMap((g) => [
              <MenuItem key={`h-${g}`} disabled sx={{ opacity: 0.6, fontSize: 11, textTransform: 'uppercase', letterSpacing: '0.06em' }}>
                {g}
              </MenuItem>,
              ...SOURCES.filter((s) => s.group === g).map((s) => (
                <MenuItem key={s.value} value={s.value} sx={{ pl: 3 }}>
                  {s.label}
                </MenuItem>
              )),
            ])}
          </TextField>

          {meta.entity && (
            <TextField
              label={meta.entity.label}
              size="small"
              value={entity}
              onChange={(e) => setEntity(e.target.value)}
              placeholder={meta.entity.placeholder}
              fullWidth
            />
          )}

          {meta.numeric && (
            <Box>
              <FieldLabel>Condition</FieldLabel>
              <Box sx={{ display: 'flex', alignItems: 'center', gap: 1.5 }}>
                <SegmentedTabs items={COMPARATORS} value={comparator} onChange={setComparator} />
                <TextField
                  size="small"
                  type="number"
                  value={threshold}
                  onChange={(e) => setThreshold(e.target.value)}
                  slotProps={{ input: { endAdornment: <InputAdornment position="end">{meta.unit}</InputAdornment> } }}
                  sx={{ flex: 1 }}
                />
              </Box>
            </Box>
          )}

          <Box sx={{ display: 'grid', gridTemplateColumns: 'auto 1fr 1fr', gap: 2, alignItems: 'end' }}>
            <Box>
              <FieldLabel>Severity</FieldLabel>
              <SegmentedTabs items={SEVERITIES} value={severity} onChange={(v) => setSeverity(v as 'warning' | 'critical')} />
            </Box>
            <TextField
              label="Fire after"
              type="number"
              size="small"
              value={forSec}
              onChange={(e) => setForSec(e.target.value)}
              slotProps={{ htmlInput: { min: 0 }, input: { endAdornment: <InputAdornment position="end">s</InputAdornment> } }}
            />
            <TextField
              label="Recovery grace"
              type="number"
              size="small"
              value={graceSec}
              onChange={(e) => setGraceSec(e.target.value)}
              slotProps={{ htmlInput: { min: 0 }, input: { endAdornment: <InputAdornment position="end">s</InputAdornment> } }}
            />
          </Box>

          <Box sx={{ display: 'flex', alignItems: 'center', gap: 1.5 }}>
            <Toggle checked={enabled} onChange={setEnabled} aria-label="Enabled" />
            <Typography sx={{ fontSize: 13, color: colors.textDim }}>
              {enabled ? 'Enabled' : 'Disabled'}
            </Typography>
          </Box>

          {/* Live preview */}
          <Box
            sx={{
              borderRadius: 2,
              border: `1px solid ${colors.border}`,
              bgcolor: 'rgba(255,255,255,0.025)',
              px: 2,
              py: 1.5,
            }}
          >
            <FieldLabel>Preview</FieldLabel>
            <Typography sx={{ fontSize: 13.5, color: colors.text, lineHeight: 1.5 }}>
              Send a{' '}
              <Box component="span" sx={{ color: severity === 'critical' ? colors.error : colors.warn, fontWeight: 700 }}>
                {severity}
              </Box>{' '}
              alert when <Box component="span" sx={{ fontWeight: 600 }}>{condition}</Box>
              {forN > 0 ? ` for ${formatInterval(forN)}` : ''}.
            </Typography>
          </Box>

          {error && <Typography sx={{ fontSize: 13, color: colors.error }}>{error}</Typography>}
        </Stack>
      </DialogContent>
      <DialogActions sx={{ px: 3, pb: 2 }}>
        <Button onClick={onClose} size="small" color="inherit">
          Cancel
        </Button>
        <Button onClick={save} size="small" variant="contained" disabled={saving}>
          {saving ? 'Saving…' : isEdit ? 'Save' : 'Create alert'}
        </Button>
      </DialogActions>
    </Dialog>
  )
}

function FieldLabel({ children }: { children: ReactNode }) {
  return (
    <Typography
      sx={{ fontSize: 11, fontWeight: 600, textTransform: 'uppercase', letterSpacing: '0.06em', color: colors.textFaint, mb: 0.75 }}
    >
      {children}
    </Typography>
  )
}

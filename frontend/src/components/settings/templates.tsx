import { Box, Tooltip, Typography } from '@mui/material'
import type { AlertSource, CheckConfig } from '../../api'
import { colors } from '../../theme'

// Templates are convenience presets that pre-fill the create form. They're a
// pure frontend concern — picking one just seeds field values you then tweak and
// save like any other check/alert. Hosts/URLs default to localhost so the values
// are obviously placeholders to edit.

export interface CheckTemplate {
  id: string
  label: string
  hint: string
  // Merged over the current form. `name` is treated as a suggestion (only used
  // when the name field is still empty).
  values: Partial<CheckConfig>
}

export const checkTemplates: CheckTemplate[] = [
  {
    id: 'https',
    label: 'HTTPS alive',
    hint: 'GET an HTTPS URL and expect a 2xx response',
    values: { name: 'https-alive', type: 'http', method: 'GET', url: 'https://example.com', interval_sec: 60, timeout_sec: 10 },
  },
  {
    id: 'http-health',
    label: 'HTTP health',
    hint: 'GET a service health endpoint',
    values: { name: 'http-health', type: 'http', method: 'GET', url: 'http://localhost:8080/health', interval_sec: 30, timeout_sec: 10 },
  },
  {
    id: 'ssh',
    label: 'SSH',
    hint: 'TCP connect to port 22',
    values: { name: 'ssh', type: 'tcp', host: 'localhost', port: 22, interval_sec: 60, timeout_sec: 5 },
  },
  {
    id: 'postgres',
    label: 'PostgreSQL',
    hint: 'TCP connect to port 5432',
    values: { name: 'postgres', type: 'tcp', host: 'localhost', port: 5432, interval_sec: 30, timeout_sec: 5 },
  },
  {
    id: 'mysql',
    label: 'MySQL',
    hint: 'TCP connect to port 3306',
    values: { name: 'mysql', type: 'tcp', host: 'localhost', port: 3306, interval_sec: 30, timeout_sec: 5 },
  },
  {
    id: 'redis',
    label: 'Redis',
    hint: 'TCP connect to port 6379',
    values: { name: 'redis', type: 'tcp', host: 'localhost', port: 6379, interval_sec: 30, timeout_sec: 5 },
  },
  {
    id: 'process',
    label: 'Process',
    hint: 'A named process is running',
    values: { name: 'nginx', type: 'process', process: 'nginx', interval_sec: 30, timeout_sec: 5 },
  },
]

export interface AlertTemplate {
  id: string
  label: string
  hint: string
  name: string
  source: AlertSource
  entity?: string
  comparator?: string
  // In DISPLAY units (%, MB/s, ms, s) — same as the form field; it's scaled to
  // storage units on save.
  threshold?: number
  severity: 'warning' | 'critical'
  forSeconds?: number
  graceSeconds?: number
}

export const alertTemplates: AlertTemplate[] = [
  { id: 'cpu', label: 'High CPU', hint: 'CPU ≥ 90% sustained for 2m', name: 'High CPU', source: 'cpu', comparator: '>=', threshold: 90, severity: 'warning', forSeconds: 120 },
  { id: 'mem', label: 'High memory', hint: 'Memory ≥ 90% sustained for 2m', name: 'High memory', source: 'mem', comparator: '>=', threshold: 90, severity: 'warning', forSeconds: 120 },
  { id: 'disk', label: 'Disk almost full', hint: 'Any mount ≥ 90% used', name: 'Disk almost full', source: 'disk', entity: '*', comparator: '>=', threshold: 90, severity: 'critical' },
  { id: 'check', label: 'Service check failing', hint: 'Any check is failing', name: 'Service check failing', source: 'check_status', entity: '*', severity: 'critical' },
  { id: 'peer', label: 'Node unreachable', hint: 'A peer node is down for 1m', name: 'Node unreachable', source: 'peer', entity: '*', severity: 'critical', forSeconds: 60 },
  { id: 'nodata', label: 'No data (stale)', hint: 'No samples received for 2m', name: 'No data', source: 'nodata', entity: '*', comparator: '>=', threshold: 120, severity: 'warning' },
  { id: 'netout', label: 'High network out', hint: 'Egress ≥ 100 MB/s', name: 'High network out', source: 'net_sent', comparator: '>=', threshold: 100, severity: 'warning' },
]

// TemplateChips renders a labeled row of pill buttons; clicking one calls onPick.
export function TemplateChips<T extends { id: string; label: string; hint: string }>({
  label,
  items,
  onPick,
}: {
  label: string
  items: T[]
  onPick: (item: T) => void
}) {
  return (
    <Box>
      <Typography
        sx={{ fontSize: 11, fontWeight: 600, textTransform: 'uppercase', letterSpacing: '0.06em', color: colors.textFaint, mb: 0.75 }}
      >
        {label}
      </Typography>
      <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 0.75 }}>
        {items.map((it) => (
          <Tooltip key={it.id} title={it.hint}>
            <Box
              component="button"
              type="button"
              onClick={() => onPick(it)}
              sx={{
                appearance: 'none',
                cursor: 'pointer',
                fontFamily: 'inherit',
                fontSize: 12.5,
                color: colors.textDim,
                bgcolor: 'rgba(255,255,255,0.04)',
                border: `1px solid ${colors.border}`,
                borderRadius: '999px',
                px: 1.25,
                py: 0.4,
                transition: 'background-color 120ms, color 120ms',
                '&:hover': { bgcolor: 'rgba(255,255,255,0.09)', color: colors.text },
              }}
            >
              {it.label}
            </Box>
          </Tooltip>
        ))}
      </Box>
    </Box>
  )
}

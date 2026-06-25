import { Box, Chip, Paper, Stack, Typography } from '@mui/material'
import type { Check } from '../api'

interface Props {
  checks: Check[]
}

const statusColor: Record<Check['status'], string> = {
  ok: '#4caf50',
  failing: '#f44336',
  unknown: '#9e9e9e',
}

function relativeTime(iso: string): string {
  if (!iso) return 'never'
  const then = new Date(iso).getTime()
  const secs = Math.max(0, Math.round((Date.now() - then) / 1000))
  if (secs < 60) return `${secs}s ago`
  if (secs < 3600) return `${Math.round(secs / 60)}m ago`
  return `${Math.round(secs / 3600)}h ago`
}

export function CheckList({ checks }: Props) {
  if (checks.length === 0) {
    return (
      <Typography sx={{ color: '#777', mt: 4 }}>
        No checks configured. Add a <code>checks:</code> section to heartd.yaml.
      </Typography>
    )
  }

  return (
    <Box sx={{ mt: 4 }}>
      <Typography variant="overline" sx={{ color: '#888' }}>
        Checks
      </Typography>
      <Stack spacing={1} sx={{ mt: 1 }}>
        {checks.map((c) => (
          <Paper
            key={c.name}
            elevation={0}
            sx={{
              p: 1.5,
              bgcolor: '#1d1d1d',
              color: '#eee',
              display: 'flex',
              alignItems: 'center',
              gap: 1.5,
            }}
          >
            <Box
              sx={{ width: 10, height: 10, borderRadius: '50%', bgcolor: statusColor[c.status], flexShrink: 0 }}
            />
            <Box sx={{ minWidth: 160 }}>
              <Typography sx={{ fontWeight: 600 }}>{c.name}</Typography>
              <Typography variant="caption" sx={{ color: '#777' }}>
                {c.type}
              </Typography>
            </Box>
            <Typography sx={{ color: '#bbb', flex: 1 }} noWrap title={c.detail}>
              {c.detail || '—'}
            </Typography>
            {c.latency_ms > 0 && (
              <Chip label={`${c.latency_ms}ms`} size="small" sx={{ bgcolor: '#2a2a2a', color: '#bbb' }} />
            )}
            <Typography variant="caption" sx={{ color: '#777', width: 70, textAlign: 'right' }}>
              {relativeTime(c.last_checked)}
            </Typography>
          </Paper>
        ))}
      </Stack>
    </Box>
  )
}

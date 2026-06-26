import { useState } from 'react'
import { Box, Button, Popover, TextField, Typography } from '@mui/material'
import { customRange, RETENTION_MS, type TimeRange } from '../timerange'
import { colors } from '../theme'

interface Props {
  anchorEl: HTMLElement | null
  current: TimeRange
  onClose: () => void
  onApply: (r: TimeRange) => void
}

// toLocalInput formats an epoch-ms instant as the value a <input type="datetime-local">
// expects ("YYYY-MM-DDTHH:mm"), in the browser's local timezone.
function toLocalInput(ms: number): string {
  const d = new Date(ms - new Date(ms).getTimezoneOffset() * 60_000)
  return d.toISOString().slice(0, 16)
}

// CustomRangePopover lets the user pick an absolute from/to window, clamped to the
// 7-day retention horizon. Uses native datetime-local inputs (no extra dependency).
export function CustomRangePopover({ anchorEl, current, onClose, onApply }: Props) {
  const now = Date.now()
  const defaultFrom = current.from ?? now - (current.spanMs || 60 * 60_000)
  const defaultTo = current.to ?? now
  const [from, setFrom] = useState(() => toLocalInput(defaultFrom))
  const [to, setTo] = useState(() => toLocalInput(defaultTo))
  const [error, setError] = useState('')

  const minInput = toLocalInput(now - RETENTION_MS)
  const maxInput = toLocalInput(now)

  const apply = () => {
    const f = new Date(from).getTime()
    const t = new Date(to).getTime()
    if (Number.isNaN(f) || Number.isNaN(t)) {
      setError('Pick both a start and an end.')
      return
    }
    if (t <= f) {
      setError('End must be after start.')
      return
    }
    onApply(customRange(f, t))
    onClose()
  }

  const fieldSx = {
    '& input': { colorScheme: 'dark', fontSize: 13 },
  } as const

  return (
    <Popover
      open={Boolean(anchorEl)}
      anchorEl={anchorEl}
      onClose={onClose}
      anchorOrigin={{ vertical: 'bottom', horizontal: 'right' }}
      transformOrigin={{ vertical: 'top', horizontal: 'right' }}
      slotProps={{ paper: { sx: { mt: 1, p: 2, bgcolor: colors.panel, width: 260 } } }}
    >
      <Typography sx={{ fontSize: 12, color: colors.textDim, mb: 1.5 }}>
        Custom range (last 7 days)
      </Typography>
      <Box sx={{ display: 'flex', flexDirection: 'column', gap: 1.5 }}>
        <TextField
          type="datetime-local"
          label="From"
          size="small"
          value={from}
          onChange={(e) => setFrom(e.target.value)}
          slotProps={{ inputLabel: { shrink: true }, htmlInput: { min: minInput, max: maxInput } }}
          sx={fieldSx}
        />
        <TextField
          type="datetime-local"
          label="To"
          size="small"
          value={to}
          onChange={(e) => setTo(e.target.value)}
          slotProps={{ inputLabel: { shrink: true }, htmlInput: { min: minInput, max: maxInput } }}
          sx={fieldSx}
        />
        {error && (
          <Typography sx={{ fontSize: 12, color: colors.error }}>{error}</Typography>
        )}
        <Box sx={{ display: 'flex', justifyContent: 'flex-end', gap: 1, mt: 0.5 }}>
          <Button size="small" onClick={onClose} sx={{ color: colors.textDim }}>
            Cancel
          </Button>
          <Button size="small" variant="contained" onClick={apply}>
            Apply
          </Button>
        </Box>
      </Box>
    </Popover>
  )
}

import { useEffect, useState } from 'react'
import { Box, Chip, IconButton, ToggleButton, ToggleButtonGroup, Tooltip, Typography } from '@mui/material'
import type { NodeStatus } from '../api'
import { colors, statusColor } from '../theme'

export const RANGES = [
  { label: '15m', minutes: 15 },
  { label: '1h', minutes: 60 },
  { label: '24h', minutes: 1440 },
]

interface Props {
  nodeName: string | null
  status: NodeStatus | null
  lastUpdated: number | null
  paused: boolean
  onTogglePause: () => void
  rangeMinutes: number
  onRangeChange: (minutes: number) => void
  onMenu?: () => void
}

function useAgo(ts: number | null): string {
  const [, force] = useState(0)
  useEffect(() => {
    const id = setInterval(() => force((n) => n + 1), 1000)
    return () => clearInterval(id)
  }, [])
  if (!ts) return '—'
  const secs = Math.max(0, Math.round((Date.now() - ts) / 1000))
  if (secs < 60) return `${secs}s ago`
  return `${Math.round(secs / 60)}m ago`
}

export function TopBar({
  nodeName,
  status,
  lastUpdated,
  paused,
  onTogglePause,
  rangeMinutes,
  onRangeChange,
  onMenu,
}: Props) {
  const ago = useAgo(lastUpdated)
  const fresh = lastUpdated != null && Date.now() - lastUpdated < 6000

  return (
    <Box
      sx={{
        display: 'flex',
        alignItems: 'center',
        gap: 2,
        px: 4,
        py: 2.5,
        borderBottom: `1px solid ${colors.border}`,
        position: 'sticky',
        top: 0,
        bgcolor: 'rgba(13,15,19,0.85)',
        backdropFilter: 'blur(8px)',
        zIndex: 10,
      }}
    >
      {onMenu && (
        <IconButton size="small" onClick={onMenu} sx={{ color: colors.textDim, ml: -1 }}>
          <MenuIcon />
        </IconButton>
      )}
      <Typography sx={{ fontSize: 22, fontWeight: 700, letterSpacing: '-0.02em' }}>
        {nodeName ?? '—'}
      </Typography>
      {status && (
        <Chip
          size="small"
          label={status}
          sx={{
            bgcolor: 'transparent',
            border: `1px solid ${statusColor(status)}`,
            color: statusColor(status),
            fontWeight: 600,
            textTransform: 'capitalize',
            height: 22,
          }}
        />
      )}

      <Box sx={{ flex: 1 }} />

      <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
        <Box
          sx={{
            width: 7,
            height: 7,
            borderRadius: '50%',
            bgcolor: paused ? colors.textFaint : fresh ? colors.ok : colors.warn,
            boxShadow: !paused && fresh ? `0 0 0 3px ${colors.ok}22` : 'none',
            transition: 'background-color 200ms',
          }}
        />
        <Typography sx={{ fontSize: 12, color: colors.textDim, width: 64 }}>
          {paused ? 'paused' : ago}
        </Typography>
      </Box>

      <Tooltip title={paused ? 'Resume auto-refresh' : 'Pause auto-refresh'}>
        <IconButton size="small" onClick={onTogglePause} sx={{ color: colors.textDim }}>
          {paused ? <PlayIcon /> : <PauseIcon />}
        </IconButton>
      </Tooltip>

      <ToggleButtonGroup
        size="small"
        exclusive
        value={rangeMinutes}
        onChange={(_, v) => v != null && onRangeChange(v)}
      >
        {RANGES.map((r) => (
          <ToggleButton key={r.minutes} value={r.minutes} sx={{ px: 1.5, py: 0.25, fontSize: 12 }}>
            {r.label}
          </ToggleButton>
        ))}
      </ToggleButtonGroup>
    </Box>
  )
}

// Inline SVG icons to avoid pulling in @mui/icons-material.
function PauseIcon() {
  return (
    <svg width="16" height="16" viewBox="0 0 24 24" fill="currentColor">
      <rect x="6" y="5" width="4" height="14" rx="1" />
      <rect x="14" y="5" width="4" height="14" rx="1" />
    </svg>
  )
}
function PlayIcon() {
  return (
    <svg width="16" height="16" viewBox="0 0 24 24" fill="currentColor">
      <path d="M8 5v14l11-7z" />
    </svg>
  )
}
function MenuIcon() {
  return (
    <svg width="18" height="18" viewBox="0 0 24 24" fill="currentColor">
      <rect x="3" y="6" width="18" height="2" rx="1" />
      <rect x="3" y="11" width="18" height="2" rx="1" />
      <rect x="3" y="16" width="18" height="2" rx="1" />
    </svg>
  )
}

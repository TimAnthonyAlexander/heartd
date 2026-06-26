import { useEffect, useState } from 'react'
import { Box, Chip, IconButton, ToggleButton, ToggleButtonGroup, Tooltip, Typography } from '@mui/material'
import type { NodeStatus } from '../api'
import { colors, statusColor } from '../theme'
import { livePreset, RANGE_PRESETS, type TimeRange } from '../timerange'
import { CustomRangePopover } from './CustomRangePopover'

interface Props {
  nodeName: string | null
  status: NodeStatus | null
  lastUpdated: number | null
  paused: boolean
  onTogglePause: () => void
  range: TimeRange
  onRangeChange: (range: TimeRange) => void
  onMenu?: () => void
  username?: string | null
  onLogout?: () => void
  onSettings?: () => void
  // When set, shows a rename control next to the node title.
  onRename?: () => void
}

// customLabel renders a compact "Jun 20 14:00 – Jun 21 09:00" summary for the
// custom toggle when a fixed range is active.
function customLabel(range: TimeRange): string {
  if (range.from == null || range.to == null) return 'Custom…'
  const fmt = (ms: number) => {
    const d = new Date(ms)
    const day = d.toLocaleDateString(undefined, { month: 'short', day: 'numeric' })
    const time = `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
    return `${day} ${time}`
  }
  return `${fmt(range.from)} – ${fmt(range.to)}`
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
  range,
  onRangeChange,
  onMenu,
  username,
  onLogout,
  onSettings,
  onRename,
}: Props) {
  const ago = useAgo(lastUpdated)
  const fresh = lastUpdated != null && Date.now() - lastUpdated < 6000
  const [customAnchor, setCustomAnchor] = useState<HTMLElement | null>(null)

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
        <IconButton aria-label="Menu" size="small" onClick={onMenu} sx={{ color: colors.textDim, ml: -1 }}>
          <MenuIcon />
        </IconButton>
      )}
      <Typography sx={{ fontSize: 22, fontWeight: 700, letterSpacing: '-0.02em' }}>
        {nodeName ?? '—'}
      </Typography>
      {onRename && (
        <Tooltip title="Rename">
          <IconButton aria-label="Rename node" size="small" onClick={onRename} sx={{ color: colors.textDim, ml: -0.5 }}>
            <EditIcon />
          </IconButton>
        </Tooltip>
      )}
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
        <IconButton aria-label={paused ? 'Resume' : 'Pause'} size="small" onClick={onTogglePause} sx={{ color: colors.textDim }}>
          {paused ? <PlayIcon /> : <PauseIcon />}
        </IconButton>
      </Tooltip>

      <ToggleButtonGroup
        size="small"
        exclusive
        value={range.key}
        onChange={(e, v) => {
          if (v == null) return
          if (v === 'custom') {
            setCustomAnchor(e.currentTarget as HTMLElement)
            return
          }
          const p = RANGE_PRESETS.find((x) => x.key === v)
          if (p) onRangeChange(livePreset(p))
        }}
      >
        {RANGE_PRESETS.map((p) => (
          <ToggleButton key={p.key} value={p.key} sx={{ px: 1.5, py: 0.25, fontSize: 12 }}>
            {p.label}
          </ToggleButton>
        ))}
        <ToggleButton value="custom" sx={{ px: 1.5, py: 0.25, fontSize: 12, textTransform: 'none' }}>
          {range.key === 'custom' ? customLabel(range) : 'Custom…'}
        </ToggleButton>
      </ToggleButtonGroup>

      <CustomRangePopover
        anchorEl={customAnchor}
        current={range}
        onClose={() => setCustomAnchor(null)}
        onApply={onRangeChange}
      />

      {onSettings && (
        <Tooltip title="Settings">
          <IconButton aria-label="Settings" size="small" onClick={onSettings} sx={{ color: colors.textDim, ml: 0.5 }}>
            <GearIcon />
          </IconButton>
        </Tooltip>
      )}

      {onLogout && (
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5, ml: 0.5, pl: 1.5, borderLeft: `1px solid ${colors.border}` }}>
          {username && (
            <Typography sx={{ fontSize: 13, color: colors.textDim, mr: 0.5 }}>{username}</Typography>
          )}
          <Tooltip title="Sign out">
            <IconButton aria-label="Sign out" size="small" onClick={onLogout} sx={{ color: colors.textDim }}>
              <LogoutIcon />
            </IconButton>
          </Tooltip>
        </Box>
      )}
    </Box>
  )
}

// Inline SVG icons to avoid pulling in @mui/icons-material.
function EditIcon() {
  return (
    <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
      <path d="M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7" />
      <path d="M18.5 2.5a2.121 2.121 0 0 1 3 3L12 15l-4 1 1-4 9.5-9.5z" />
    </svg>
  )
}
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
function GearIcon() {
  return (
    <svg width="17" height="17" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
      <circle cx="12" cy="12" r="3" />
      <path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z" />
    </svg>
  )
}
function LogoutIcon() {
  return (
    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
      <path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4" />
      <polyline points="16 17 21 12 16 7" />
      <line x1="21" y1="12" x2="9" y2="12" />
    </svg>
  )
}

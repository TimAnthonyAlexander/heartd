import type { ReactNode } from 'react'
import { Box, Button, Typography } from '@mui/material'
import { colors } from '../../theme'

// Format an interval in seconds to a compact label: "30s", "5m", "2h".
export function formatInterval(sec: number): string {
  if (sec <= 0) return '0s'
  if (sec % 3600 === 0) return `${sec / 3600}h`
  if (sec % 60 === 0) return `${sec / 60}m`
  return `${sec}s`
}

// Per-section save state. A string value is an error message.
export type Feedback = 'idle' | 'saving' | 'saved' | { error: string }

// Renders the trailing feedback text for a section save action.
export function FeedbackText({ feedback }: { feedback: Feedback }) {
  if (feedback === 'idle' || feedback === 'saving') return null
  if (feedback === 'saved') {
    return <Typography sx={{ fontSize: 13, color: colors.ok }}>Saved</Typography>
  }
  return <Typography sx={{ fontSize: 13, color: colors.error }}>{feedback.error}</Typography>
}

interface SectionProps {
  label: string
  description?: string
  children: ReactNode
  // The action row rendered at the bottom (save button + feedback).
  actions?: ReactNode
}

// A free-standing settings block: a heading and its fields sit directly on the
// page rather than inside a card. Inner tables/lists keep their own borders for
// structure; the section itself has no chrome.
export function Section({ label, description, children, actions }: SectionProps) {
  return (
    <Box>
      <Typography
        sx={{ fontSize: 16, fontWeight: 600, letterSpacing: '-0.01em', color: colors.text }}
      >
        {label}
      </Typography>
      {description && (
        <Typography sx={{ fontSize: 13, color: colors.textFaint, mt: 0.5 }}>
          {description}
        </Typography>
      )}
      <Box sx={{ mt: 2.5 }}>{children}</Box>
      {actions && (
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 2, mt: 3 }}>{actions}</Box>
      )}
    </Box>
  )
}

interface SaveButtonProps {
  feedback: Feedback
  onClick: () => void
  disabled?: boolean
  label?: string
}

export function SaveButton({ feedback, onClick, disabled, label = 'Save' }: SaveButtonProps) {
  return (
    <Button
      variant="contained"
      size="small"
      onClick={onClick}
      disabled={disabled || feedback === 'saving'}
    >
      {feedback === 'saving' ? 'Saving…' : label}
    </Button>
  )
}

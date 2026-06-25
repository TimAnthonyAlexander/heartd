import type { ReactNode } from 'react'
import { Box, Button, Paper, Typography } from '@mui/material'
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

// A settings panel matching the dashboard's Paper sections.
export function Section({ label, description, children, actions }: SectionProps) {
  return (
    <Paper elevation={0} sx={{ p: 3, borderRadius: 2.5 }}>
      <Typography variant="overline" sx={{ color: colors.textDim }}>
        {label}
      </Typography>
      {description && (
        <Typography sx={{ fontSize: 13, color: colors.textFaint, mt: 0.25, mb: 0.5 }}>
          {description}
        </Typography>
      )}
      <Box sx={{ mt: 2 }}>{children}</Box>
      {actions && (
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 2, mt: 3 }}>{actions}</Box>
      )}
    </Paper>
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

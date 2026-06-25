import { Box, Button, Paper, Typography } from '@mui/material'
import { colors } from '../theme'

interface Props {
  onClose: () => void
}

// The global settings page is now a thin placeholder: per-node configuration
// (checks, notifications, thresholds, sampling) moved onto each node's own tabs.
// This page is reserved for genuinely cluster-wide concerns and account
// management, which don't exist yet.
export function SettingsPage({ onClose }: Props) {
  return (
    <Box sx={{ minHeight: '100vh', bgcolor: colors.bg }}>
      <Box sx={{ maxWidth: 880, mx: 'auto', p: { xs: 2, md: 4 } }}>
        <Box
          sx={{
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'space-between',
            mb: 3,
          }}
        >
          <Typography sx={{ fontSize: 24, fontWeight: 700, letterSpacing: '-0.02em' }}>
            Settings
          </Typography>
          <Button variant="outlined" size="small" onClick={onClose}>
            Back to dashboard
          </Button>
        </Box>

        <Paper elevation={0} sx={{ p: 3, borderRadius: 2.5 }}>
          <Typography variant="overline" sx={{ color: colors.textDim }}>
            Per-node configuration moved
          </Typography>
          <Typography sx={{ fontSize: 14, color: colors.textDim, mt: 1 }}>
            Checks, notifications, alert thresholds, and sampling are now configured
            on each node directly — open a node and use its Checks, Notifications,
            Alerts, and Settings tabs. Editing a remote node proxies the change to it
            over the secured peer link.
          </Typography>
          <Typography sx={{ fontSize: 13, color: colors.textFaint, mt: 2 }}>
            This page is reserved for future cluster-wide settings and account
            management.
          </Typography>
        </Paper>
      </Box>
    </Box>
  )
}

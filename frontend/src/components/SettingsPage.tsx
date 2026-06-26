import { Box, Button, Stack, Typography } from '@mui/material'
import { colors } from '../theme'
import { UsersSection } from './settings/UsersSection'

interface Props {
  onClose: () => void
}

// The global settings page holds genuinely cluster-wide concerns. Per-node
// configuration (checks, notifications, thresholds, sampling) lives on each
// node's own tabs; this page currently manages user accounts.
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

        <Stack spacing={3}>
          <UsersSection />
          <Typography sx={{ fontSize: 13, color: colors.textFaint }}>
            Looking for checks, notifications, thresholds, or sampling? Those are now
            configured per node — open a node and use its Checks, Notifications,
            Alerts, and Settings tabs.
          </Typography>
        </Stack>
      </Box>
    </Box>
  )
}

import { useEffect, useState } from 'react'
import { Alert, Box, Button, CircularProgress, Stack, Typography } from '@mui/material'
import type { AllSettings, CheckConfig, GeneralSettings, NotifySettings } from '../api'
import { fetchSettings } from '../api'
import { colors } from '../theme'
import { GeneralSection } from './settings/GeneralSection'
import { NotifySection } from './settings/NotifySection'
import { ChecksSection } from './settings/ChecksSection'

interface Props {
  onClose: () => void
}

export function SettingsPage({ onClose }: Props) {
  const [settings, setSettings] = useState<AllSettings | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let active = true
    setLoading(true)
    setError(null)
    fetchSettings()
      .then((s) => {
        if (active) setSettings(s)
      })
      .catch((err: unknown) => {
        if (active) setError(err instanceof Error ? err.message : 'Failed to load settings')
      })
      .finally(() => {
        if (active) setLoading(false)
      })
    return () => {
      active = false
    }
  }, [])

  const setGeneral = (general: GeneralSettings) =>
    setSettings((prev) => (prev ? { ...prev, general } : prev))

  const setNotify = (notify: NotifySettings) =>
    setSettings((prev) => (prev ? { ...prev, notify } : prev))

  const setChecks = (checks: CheckConfig[]) =>
    setSettings((prev) => (prev ? { ...prev, checks } : prev))

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

        {loading && (
          <Box sx={{ display: 'flex', justifyContent: 'center', py: 10 }}>
            <CircularProgress />
          </Box>
        )}

        {!loading && error && <Alert severity="error">{error}</Alert>}

        {!loading && !error && settings && (
          <Stack spacing={3}>
            <GeneralSection initial={settings.general} onSaved={setGeneral} />
            <NotifySection initial={settings.notify} onSaved={setNotify} />
            <ChecksSection checks={settings.checks} onChange={setChecks} />
          </Stack>
        )}
      </Box>
    </Box>
  )
}

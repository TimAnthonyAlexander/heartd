import { Alert, Box, CircularProgress, Stack } from '@mui/material'
import { useNodeSettings } from '../hooks/useNodeSettings'
import { ChecksSection } from './settings/ChecksSection'
import { NotifySection } from './settings/NotifySection'
import { ThresholdsSection } from './settings/ThresholdsSection'
import { SamplingSection } from './settings/SamplingSection'

export type ConfigTab = 'checks' | 'notifications' | 'alerts' | 'settings'

interface Props {
  nodeName: string
  isLocal: boolean
  tab: ConfigTab
}

// NodeConfig hosts the per-node configuration tabs. It loads the node's settings
// once (proxied to the node over the peer link when it isn't local) and renders
// the section for the active tab, all writing back to the same node.
export function NodeConfig({ nodeName, isLocal, tab }: Props) {
  const { settings, loading, error, setGeneral, setNotify, setChecks } = useNodeSettings(nodeName)

  if (loading) {
    return (
      <Box sx={{ display: 'flex', justifyContent: 'center', py: 10 }}>
        <CircularProgress />
      </Box>
    )
  }

  if (error || !settings) {
    return (
      <Alert severity="error">
        Couldn't load {nodeName}'s configuration{isLocal ? '' : ' (this node may be unreachable)'}:{' '}
        {error ?? 'no data'}
      </Alert>
    )
  }

  return (
    <Stack spacing={3}>
      {tab === 'checks' && (
        <ChecksSection nodeName={nodeName} checks={settings.checks} onChange={setChecks} />
      )}
      {tab === 'notifications' && (
        <NotifySection nodeName={nodeName} initial={settings.notify} onSaved={setNotify} />
      )}
      {tab === 'alerts' && (
        <ThresholdsSection nodeName={nodeName} initial={settings.general} onSaved={setGeneral} />
      )}
      {tab === 'settings' && (
        <SamplingSection nodeName={nodeName} initial={settings.general} onSaved={setGeneral} />
      )}
    </Stack>
  )
}

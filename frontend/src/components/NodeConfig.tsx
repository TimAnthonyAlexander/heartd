import { Alert, Box, CircularProgress, Stack } from '@mui/material'
import { useNodeSettings } from '../hooks/useNodeSettings'
import { ChecksSection } from './settings/ChecksSection'
import { NotifySection } from './settings/NotifySection'
import { AlertsSection } from './settings/AlertsSection'
import { SamplingSection } from './settings/SamplingSection'

export type ConfigTab = 'checks' | 'notifications' | 'alerts' | 'settings'

// EditTarget is a deep-link request to open a specific item's edit form, set when
// the user clicks "Edit" on the dashboard. Checks are addressed by name (the
// dashboard knows their runtime status, not their config id); alerts by id.
export type EditTarget =
  | { kind: 'check'; name: string }
  | { kind: 'alert'; id: number }
  | null

interface Props {
  nodeName: string
  isLocal: boolean
  tab: ConfigTab
  editTarget?: EditTarget
  onEditConsumed?: () => void
}

// NodeConfig hosts the per-node configuration tabs. It loads the node's settings
// once (proxied to the node over the peer link when it isn't local) and renders
// the section for the active tab, all writing back to the same node.
export function NodeConfig({ nodeName, isLocal, tab, editTarget, onEditConsumed }: Props) {
  const { settings, loading, error, setGeneral, setNotify, setChecks, setAlerts } =
    useNodeSettings(nodeName)

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
        <ChecksSection
          nodeName={nodeName}
          checks={settings.checks}
          onChange={setChecks}
          editName={editTarget?.kind === 'check' ? editTarget.name : undefined}
          onEditConsumed={onEditConsumed}
        />
      )}
      {tab === 'notifications' && (
        <NotifySection nodeName={nodeName} initial={settings.notify} onSaved={setNotify} />
      )}
      {tab === 'alerts' && (
        <AlertsSection
          nodeName={nodeName}
          alerts={settings.alerts}
          onChange={setAlerts}
          editId={editTarget?.kind === 'alert' ? editTarget.id : undefined}
          onEditConsumed={onEditConsumed}
        />
      )}
      {tab === 'settings' && (
        <SamplingSection nodeName={nodeName} initial={settings.general} onSaved={setGeneral} />
      )}
    </Stack>
  )
}

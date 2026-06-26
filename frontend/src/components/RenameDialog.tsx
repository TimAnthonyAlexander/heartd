import { useState } from 'react'
import {
  Button,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  Stack,
  TextField,
  Typography,
} from '@mui/material'
import { setNodeAlias, type Node } from '../api'
import { colors } from '../theme'

interface Props {
  open: boolean
  node: Node
  onClose: () => void
  onRenamed: () => void
}

// RenameDialog sets a display name (alias) for a node — local or peer. The node's
// real name is its identity key and never changes; this only relabels it in this
// dashboard. Clearing the field reverts to the real name.
export function RenameDialog({ open, node, onClose, onRenamed }: Props) {
  const [alias, setAlias] = useState(node.alias ?? '')
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const save = async () => {
    setError(null)
    setSaving(true)
    try {
      await setNodeAlias(node.name, alias.trim())
      onRenamed()
      onClose()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Rename failed')
    } finally {
      setSaving(false)
    }
  }

  return (
    <Dialog open={open} onClose={onClose} maxWidth="xs" fullWidth>
      <DialogTitle>Rename {node.name}</DialogTitle>
      <DialogContent>
        <Stack spacing={2} sx={{ mt: 0.5 }}>
          <Typography sx={{ fontSize: 13, color: colors.textFaint }}>
            Set a display name for this node. It only changes the label shown in this
            dashboard — the node's real name (<b>{node.name}</b>) stays its identity, so
            history and alerts are unaffected. Leave blank to use the real name.
          </Typography>
          <TextField
            label="Display name"
            size="small"
            value={alias}
            onChange={(e) => setAlias(e.target.value)}
            placeholder={node.name}
            autoFocus
            fullWidth
            slotProps={{ htmlInput: { maxLength: 64 } }}
            onKeyDown={(e) => {
              if (e.key === 'Enter' && !saving) void save()
            }}
          />
          {error && <Typography sx={{ fontSize: 13, color: colors.error }}>{error}</Typography>}
        </Stack>
      </DialogContent>
      <DialogActions sx={{ px: 3, pb: 2 }}>
        <Button onClick={onClose} size="small" color="inherit">
          Cancel
        </Button>
        <Button onClick={save} size="small" variant="contained" disabled={saving}>
          {saving ? 'Saving…' : 'Save'}
        </Button>
      </DialogActions>
    </Dialog>
  )
}

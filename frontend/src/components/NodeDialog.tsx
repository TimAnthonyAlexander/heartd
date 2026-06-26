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
import { createPeer, updatePeer } from '../api'
import { colors } from '../theme'

interface Props {
  open: boolean
  mode: 'add' | 'edit'
  initialName?: string
  initialUrl?: string
  onClose: () => void
  onSaved: () => void
}

// NodeDialog adds a new peer node or edits an existing one's URL/secret. The name
// is the identity key, so it's only editable when adding.
export function NodeDialog({ open, mode, initialName = '', initialUrl = '', onClose, onSaved }: Props) {
  const [name, setName] = useState(initialName)
  const [url, setUrl] = useState(initialUrl)
  const [secret, setSecret] = useState('')
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const isEdit = mode === 'edit'

  const save = async () => {
    setError(null)
    setSaving(true)
    try {
      if (isEdit) {
        await updatePeer(initialName, url.trim(), secret)
      } else {
        await createPeer({ name: name.trim(), url: url.trim(), secret })
      }
      onSaved()
      onClose()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Save failed')
    } finally {
      setSaving(false)
    }
  }

  return (
    <Dialog open={open} onClose={onClose} maxWidth="sm" fullWidth>
      <DialogTitle>{isEdit ? `Edit ${initialName}` : 'Add node'}</DialogTitle>
      <DialogContent>
        <Stack spacing={2} sx={{ mt: 0.5 }}>
          <Typography sx={{ fontSize: 13, color: colors.textFaint }}>
            Point this node at another heartd instance. Use the SAME shared secret on
            both ends of the link — set the matching peer on the other node too.
          </Typography>
          {!isEdit && (
            <TextField
              label="Node name"
              size="small"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="db-01"
              autoFocus
              fullWidth
            />
          )}
          <TextField
            label="URL"
            size="small"
            value={url}
            onChange={(e) => setUrl(e.target.value)}
            placeholder="https://db-01.internal:9300"
            fullWidth
          />
          <TextField
            label={isEdit ? 'Shared secret (leave blank to keep current)' : 'Shared secret'}
            type="password"
            size="small"
            value={secret}
            onChange={(e) => setSecret(e.target.value)}
            autoComplete="new-password"
            fullWidth
          />
          {error && (
            <Typography sx={{ fontSize: 13, color: colors.error }}>{error}</Typography>
          )}
        </Stack>
      </DialogContent>
      <DialogActions sx={{ px: 3, pb: 2 }}>
        <Button onClick={onClose} size="small" color="inherit">
          Cancel
        </Button>
        <Button onClick={save} size="small" variant="contained" disabled={saving}>
          {saving ? 'Saving…' : isEdit ? 'Save' : 'Add node'}
        </Button>
      </DialogActions>
    </Dialog>
  )
}

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
import { changeUserPassword, createUser } from '../../api'
import { colors } from '../../theme'

const MIN_PASSWORD = 8

interface Props {
  // 'add' creates a new user; 'password' changes an existing user's password.
  mode: 'add' | 'password'
  username?: string // required for 'password' mode
  onClose: () => void
  onSaved: () => void
}

// UserDialog adds a new account or changes an existing account's password. Every
// user is an admin, so any signed-in user can perform these.
export function UserDialog({ mode, username = '', onClose, onSaved }: Props) {
  const [name, setName] = useState('')
  const [password, setPassword] = useState('')
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const isAdd = mode === 'add'

  const save = async () => {
    if (isAdd && name.trim() === '') {
      setError('Username is required.')
      return
    }
    if (password.length < MIN_PASSWORD) {
      setError(`Password must be at least ${MIN_PASSWORD} characters.`)
      return
    }
    setError(null)
    setSaving(true)
    try {
      if (isAdd) {
        await createUser(name.trim(), password)
      } else {
        await changeUserPassword(username, password)
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
    <Dialog open onClose={onClose} maxWidth="xs" fullWidth>
      <DialogTitle>{isAdd ? 'Add user' : `Change password — ${username}`}</DialogTitle>
      <DialogContent>
        <Stack spacing={2} sx={{ mt: 0.5 }}>
          {isAdd && (
            <TextField
              label="Username"
              size="small"
              value={name}
              onChange={(e) => setName(e.target.value)}
              autoFocus
              fullWidth
            />
          )}
          <TextField
            label={isAdd ? 'Password' : 'New password'}
            type="password"
            size="small"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            autoComplete="new-password"
            autoFocus={!isAdd}
            fullWidth
            helperText={`At least ${MIN_PASSWORD} characters`}
          />
          {error && <Typography sx={{ fontSize: 13, color: colors.error }}>{error}</Typography>}
        </Stack>
      </DialogContent>
      <DialogActions sx={{ px: 3, pb: 2 }}>
        <Button onClick={onClose} size="small" color="inherit">
          Cancel
        </Button>
        <Button onClick={save} size="small" variant="contained" disabled={saving}>
          {saving ? 'Saving…' : isAdd ? 'Add user' : 'Change password'}
        </Button>
      </DialogActions>
    </Dialog>
  )
}

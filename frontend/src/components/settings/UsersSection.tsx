import { useCallback, useEffect, useState } from 'react'
import { Box, Button, Chip, CircularProgress, IconButton, Tooltip, Typography } from '@mui/material'
import type { UserInfo } from '../../api'
import { deleteUser, fetchUsers } from '../../api'
import { colors } from '../../theme'
import { Section } from './shared'
import { UserDialog } from './UserDialog'

type DialogState = { mode: 'add' } | { mode: 'password'; username: string } | null

// UsersSection manages heartd accounts: list, add, change password, and remove.
// Every user is an admin, so any signed-in user sees this. The last remaining
// user can't be removed (the backend enforces this too).
export function UsersSection() {
  const [users, setUsers] = useState<UserInfo[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState<string | null>(null)
  const [dialog, setDialog] = useState<DialogState>(null)

  const load = useCallback(async () => {
    setError(null)
    try {
      setUsers(await fetchUsers())
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load users')
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  const remove = async (username: string) => {
    if (!window.confirm(`Delete user "${username}"? They will be signed out and removed.`)) return
    setError(null)
    setBusy(username)
    try {
      await deleteUser(username)
      await load()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Delete failed')
    } finally {
      setBusy(null)
    }
  }

  const onlyOne = (users?.length ?? 0) <= 1

  return (
    <Section
      label="Users"
      description="Accounts that can sign in to heartd. Every user is an administrator."
      actions={
        <Button variant="contained" size="small" onClick={() => setDialog({ mode: 'add' })}>
          Add user
        </Button>
      }
    >
      {error && <Typography sx={{ fontSize: 13, color: colors.error, mb: 1.5 }}>{error}</Typography>}

      {users === null ? (
        <Box sx={{ display: 'flex', justifyContent: 'center', py: 4 }}>
          <CircularProgress size={22} />
        </Box>
      ) : (
        <Box sx={{ border: `1px solid ${colors.border}`, borderRadius: 2, overflow: 'hidden' }}>
          {users.map((u, i) => (
            <Box
              key={u.username}
              sx={{
                display: 'flex',
                alignItems: 'center',
                gap: 1.5,
                px: 2,
                py: 1.25,
                borderTop: i === 0 ? 'none' : `1px solid ${colors.border}`,
                opacity: busy === u.username ? 0.5 : 1,
              }}
            >
              <Typography sx={{ fontSize: 14, fontWeight: 600, flex: 1 }} noWrap>
                {u.username}
              </Typography>
              {u.self && (
                <Chip
                  label="you"
                  size="small"
                  sx={{
                    height: 18,
                    fontSize: 11,
                    bgcolor: colors.bg,
                    border: `1px solid ${colors.border}`,
                    color: colors.textDim,
                  }}
                />
              )}
              <Button
                size="small"
                variant="text"
                sx={{ color: colors.textDim, minWidth: 0 }}
                onClick={() => setDialog({ mode: 'password', username: u.username })}
              >
                Change password
              </Button>
              <Tooltip
                title={u.self ? "You can't delete your own account" : onlyOne ? 'The last user cannot be deleted' : 'Delete'}
              >
                <span>
                  <IconButton
                    size="small"
                    sx={{ color: colors.textDim }}
                    disabled={u.self || onlyOne}
                    onClick={() => remove(u.username)}
                  >
                    <Box component="span" sx={{ fontSize: 15, lineHeight: 1 }}>
                      ✕
                    </Box>
                  </IconButton>
                </span>
              </Tooltip>
            </Box>
          ))}
        </Box>
      )}

      {dialog && (
        <UserDialog
          mode={dialog.mode}
          username={dialog.mode === 'password' ? dialog.username : undefined}
          onClose={() => setDialog(null)}
          onSaved={load}
        />
      )}
    </Section>
  )
}

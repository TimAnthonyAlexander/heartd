import { useState, type FormEvent } from 'react'
import { Box, Button, Paper, TextField, Typography } from '@mui/material'
import { colors } from '../theme'

interface Props {
  mode: 'init' | 'login'
  onSubmit: (username: string, password: string) => Promise<void>
}

export function AuthScreen({ mode, onSubmit }: Props) {
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  const isInit = mode === 'init'

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setError(null)
    setBusy(true)
    try {
      await onSubmit(username.trim(), password)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Something went wrong')
      setBusy(false)
    }
  }

  return (
    <Box
      sx={{
        minHeight: '100vh',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        bgcolor: colors.bg,
        p: 2,
      }}
    >
      <Paper elevation={0} sx={{ p: 4, borderRadius: 3, width: '100%', maxWidth: 380 }}>
        <Typography sx={{ fontSize: 22, fontWeight: 700, letterSpacing: '-0.02em' }}>heartd</Typography>
        <Typography sx={{ color: colors.textDim, fontSize: 14, mt: 0.5, mb: 3 }}>
          {isInit ? 'Create the first admin account to get started.' : 'Sign in to continue.'}
        </Typography>

        <form onSubmit={submit}>
          <TextField
            label="Username"
            value={username}
            onChange={(e) => setUsername(e.target.value)}
            fullWidth
            autoFocus
            autoComplete="username"
            size="small"
            sx={{ mb: 2 }}
          />
          <TextField
            label="Password"
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            fullWidth
            autoComplete={isInit ? 'new-password' : 'current-password'}
            helperText={isInit ? 'At least 8 characters.' : undefined}
            size="small"
            sx={{ mb: 2 }}
          />

          {error && (
            <Typography sx={{ color: colors.error, fontSize: 13, mb: 2 }}>{error}</Typography>
          )}

          <Button
            type="submit"
            variant="contained"
            fullWidth
            disabled={busy || !username.trim() || !password}
          >
            {busy ? 'Please wait…' : isInit ? 'Create admin' : 'Sign in'}
          </Button>
        </form>
      </Paper>
    </Box>
  )
}

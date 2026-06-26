import { useCallback, useEffect, useState } from 'react'
import { HashRouter, Navigate, Route, Routes, useNavigate } from 'react-router'
import { Box, CircularProgress } from '@mui/material'
import App from './App'
import { SettingsPage } from './components/SettingsPage'
import { AuthScreen } from './components/AuthScreen'
import {
  authInit,
  authLogin,
  authLogout,
  fetchAuthStatus,
  setUnauthorizedHandler,
  type AuthStatus,
} from './api'
import { colors } from './theme'

type Phase = 'loading' | 'init' | 'login' | 'authed'

export function AuthGate() {
  const [phase, setPhase] = useState<Phase>('loading')
  const [username, setUsername] = useState<string | null>(null)

  const refresh = useCallback(async () => {
    try {
      const status: AuthStatus = await fetchAuthStatus()
      if (status.authenticated) {
        setUsername(status.username ?? null)
        setPhase('authed')
      } else {
        setPhase(status.initialized ? 'login' : 'init')
      }
    } catch {
      // If even status fails, show login as a safe default.
      setPhase('login')
    }
  }, [])

  useEffect(() => {
    refresh()
  }, [refresh])

  // Any 401 from a data request drops us back to the login screen.
  useEffect(() => {
    setUnauthorizedHandler(() => {
      setPhase((p) => (p === 'authed' ? 'login' : p))
    })
    return () => setUnauthorizedHandler(null)
  }, [])

  if (phase === 'loading') {
    return (
      <Box sx={{ minHeight: '100vh', display: 'grid', placeItems: 'center', bgcolor: colors.bg }}>
        <CircularProgress size={28} />
      </Box>
    )
  }

  if (phase === 'init') {
    return (
      <AuthScreen
        mode="init"
        onSubmit={async (u, p) => {
          await authInit(u, p)
          await refresh()
        }}
      />
    )
  }

  if (phase === 'login') {
    return (
      <AuthScreen
        mode="login"
        onSubmit={async (u, p) => {
          await authLogin(u, p)
          await refresh()
        }}
      />
    )
  }

  const onLogout = async () => {
    try {
      await authLogout()
    } finally {
      setUsername(null)
      setPhase('login')
    }
  }

  return (
    <HashRouter>
      <Routes>
        <Route path="/settings" element={<SettingsRoute />} />
        <Route path="/node/:name/:tab" element={<App username={username} onLogout={onLogout} />} />
        <Route path="/node/:name" element={<App username={username} onLogout={onLogout} />} />
        <Route path="/" element={<App username={username} onLogout={onLogout} />} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes>
    </HashRouter>
  )
}

function SettingsRoute() {
  const navigate = useNavigate()
  return <SettingsPage onClose={() => navigate('/')} />
}

import { useCallback, useEffect, useState } from 'react'
import type { AlertRule, AllSettings, CheckConfig, GeneralSettings, NotifySettings } from '../api'
import { fetchSettings } from '../api'

export interface NodeSettingsState {
  settings: AllSettings | null
  loading: boolean
  error: string | null
  reload: () => void
  setGeneral: (g: GeneralSettings) => void
  setNotify: (n: NotifySettings) => void
  setChecks: (c: CheckConfig[]) => void
  setAlerts: (a: AlertRule[]) => void
}

// useNodeSettings loads the runtime config for one node. For the local node the
// backend reads its own settings; for a peer it proxies over the shared-secret
// link, so a peer that's down surfaces as an error here. It fetches once per node
// (no polling) and exposes setters so sections can update the cache after a save.
export function useNodeSettings(nodeName: string | null): NodeSettingsState {
  const [settings, setSettings] = useState<AllSettings | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [nonce, setNonce] = useState(0)

  useEffect(() => {
    if (!nodeName) return
    let active = true
    const ctrl = new AbortController()
    setLoading(true)
    setError(null)
    setSettings(null)
    fetchSettings(nodeName, ctrl.signal)
      .then((s) => {
        if (active) setSettings(s)
      })
      .catch((err: unknown) => {
        if (!active) return
        if (err instanceof Error && err.name === 'AbortError') return
        setError(err instanceof Error ? err.message : 'Failed to load settings')
      })
      .finally(() => {
        if (active) setLoading(false)
      })
    return () => {
      active = false
      ctrl.abort()
    }
  }, [nodeName, nonce])

  const reload = useCallback(() => setNonce((n) => n + 1), [])
  const setGeneral = useCallback(
    (general: GeneralSettings) => setSettings((p) => (p ? { ...p, general } : p)),
    [],
  )
  const setNotify = useCallback(
    (notify: NotifySettings) => setSettings((p) => (p ? { ...p, notify } : p)),
    [],
  )
  const setChecks = useCallback(
    (checks: CheckConfig[]) => setSettings((p) => (p ? { ...p, checks } : p)),
    [],
  )
  const setAlerts = useCallback(
    (alerts: AlertRule[]) => setSettings((p) => (p ? { ...p, alerts } : p)),
    [],
  )

  return { settings, loading, error, reload, setGeneral, setNotify, setChecks, setAlerts }
}

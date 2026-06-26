import { useEffect, useState } from 'react'
import { fetchSettings, type AlertRule } from '../api'

// useNodeAlerts loads a node's configured alert rules for display on the
// dashboard. Alert config changes rarely, so this fetches once per node (and
// whenever `enabled` flips back on — e.g. returning to the dashboard tab after
// editing) rather than polling. Best-effort: a failed/proxied fetch yields [].
export function useNodeAlerts(node: string | null, enabled: boolean): AlertRule[] {
  const [alerts, setAlerts] = useState<AlertRule[]>([])

  useEffect(() => {
    if (!node || !enabled) {
      setAlerts([])
      return
    }
    let active = true
    const controller = new AbortController()
    fetchSettings(node, controller.signal)
      .then((s) => {
        if (active) setAlerts(s.alerts)
      })
      .catch(() => {
        /* best-effort; the node may be unreachable */
      })
    return () => {
      active = false
      controller.abort()
    }
  }, [node, enabled])

  return alerts
}

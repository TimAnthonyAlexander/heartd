import { useEffect, useState } from 'react'
import { fetchActiveAlerts, fetchAlertHistory, type ActiveAlert, type AlertEvent } from '../api'

const POLL_MS = 5000

export interface NodeAlertActivity {
  active: ActiveAlert[]
  history: AlertEvent[]
}

const EMPTY: NodeAlertActivity = { active: [], history: [] }

// useNodeAlertActivity polls a node's live firing state and recent incident
// history on a 5s self-scheduling timer, aborting in-flight requests when the
// node changes or the panel is hidden. Best-effort: a failed/proxied fetch keeps
// the last values rather than throwing.
export function useNodeAlertActivity(node: string | null, enabled: boolean): NodeAlertActivity {
  const [data, setData] = useState<NodeAlertActivity>(EMPTY)

  useEffect(() => {
    if (!node || !enabled) {
      setData(EMPTY)
      return
    }

    let active = true
    let timer: ReturnType<typeof setTimeout>
    const controller = new AbortController()

    const tick = async () => {
      try {
        const [act, hist] = await Promise.all([
          fetchActiveAlerts(node, controller.signal),
          fetchAlertHistory(node, 1440, 100, controller.signal),
        ])
        if (active) setData({ active: act, history: hist })
      } catch {
        /* best-effort; the node may be unreachable — keep last values */
      } finally {
        if (active) timer = setTimeout(tick, POLL_MS)
      }
    }

    tick()

    return () => {
      active = false
      clearTimeout(timer)
      controller.abort()
    }
  }, [node, enabled])

  return data
}

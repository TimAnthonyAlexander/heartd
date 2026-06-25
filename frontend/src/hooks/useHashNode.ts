import { useCallback, useEffect, useState } from 'react'

const PREFIX = /^#\/?node\//

function readHash(): string | null {
  const raw = window.location.hash.replace(PREFIX, '')
  return raw ? decodeURIComponent(raw) : null
}

// useHashNode reflects the selected node in the URL hash (#/node/<name>) so a
// reload restores the view and node links are shareable. No router needed.
export function useHashNode(): {
  node: string | null
  select: (name: string) => void
  replace: (name: string) => void
} {
  const [node, setNode] = useState<string | null>(readHash)

  useEffect(() => {
    const onHash = () => setNode(readHash())
    window.addEventListener('hashchange', onHash)
    return () => window.removeEventListener('hashchange', onHash)
  }, [])

  // select pushes a history entry (user navigation).
  const select = useCallback((name: string) => {
    window.location.hash = `#/node/${encodeURIComponent(name)}`
  }, [])

  // replace sets the hash without adding a history entry (initial default).
  const replace = useCallback((name: string) => {
    const url = `${window.location.pathname}${window.location.search}#/node/${encodeURIComponent(name)}`
    window.history.replaceState(null, '', url)
    setNode(name)
  }, [])

  return { node, select, replace }
}

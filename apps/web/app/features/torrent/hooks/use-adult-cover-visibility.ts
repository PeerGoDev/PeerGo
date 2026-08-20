import * as React from "react"

const storageKey = "peergo-adult-covers-visible"
const legacyStorageKey = "nsfw_visible"
const preferenceEvent = "peergo:adult-cover-visibility"

function readStoredPreference() {
  try {
    return (
      globalThis.localStorage?.getItem(storageKey) === "true" ||
      (!globalThis.localStorage?.getItem(storageKey) &&
        globalThis.localStorage?.getItem(legacyStorageKey) === "true")
    )
  } catch {
    return false
  }
}

/**
 * Keeps the adult-cover preference local to the browser. The server never
 * receives this privacy choice; a custom event synchronizes catalog surfaces
 * in the same tab while the storage event covers other open tabs.
 */
export function useAdultCoverVisibility() {
  const [visible, setVisibleState] = React.useState(readStoredPreference)

  React.useEffect(() => {
    function synchronize() {
      setVisibleState(readStoredPreference())
    }

    globalThis.addEventListener?.("storage", synchronize)
    globalThis.addEventListener?.(preferenceEvent, synchronize)
    return () => {
      globalThis.removeEventListener?.("storage", synchronize)
      globalThis.removeEventListener?.(preferenceEvent, synchronize)
    }
  }, [])

  const setVisible = React.useCallback((next: boolean) => {
    setVisibleState(next)
    try {
      globalThis.localStorage?.setItem(storageKey, String(next))
      globalThis.dispatchEvent?.(new Event(preferenceEvent))
    } catch {
      // The in-memory preference remains usable when storage is unavailable.
    }
  }, [])

  return [visible, setVisible] as const
}

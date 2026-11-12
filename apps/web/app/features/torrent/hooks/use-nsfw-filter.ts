import * as React from "react"

const storageKey = "peergo-hide-nsfw"
const legacyRevealKeys = ["peergo-adult-covers-visible", "nsfw_visible"]
const preferenceEvent = "peergo:nsfw-filter"

function readStoredPreference() {
  try {
    const stored = globalThis.localStorage?.getItem(storageKey)
    if (stored === "true" || stored === "false") return stored === "true"
    // Members who already confirmed their age for adult covers keep seeing them.
    return !legacyRevealKeys.some(
      (key) => globalThis.localStorage?.getItem(key) === "true"
    )
  } catch {
    return true
  }
}

/**
 * Keeps the NSFW preference local to the browser; the server only receives the
 * resulting list filter. A custom event synchronizes catalog surfaces in the
 * same tab while the storage event covers other open tabs.
 */
export function useNsfwFilter() {
  const [hideNsfw, setHideNsfwState] = React.useState(readStoredPreference)

  React.useEffect(() => {
    function synchronize() {
      setHideNsfwState(readStoredPreference())
    }

    globalThis.addEventListener?.("storage", synchronize)
    globalThis.addEventListener?.(preferenceEvent, synchronize)
    return () => {
      globalThis.removeEventListener?.("storage", synchronize)
      globalThis.removeEventListener?.(preferenceEvent, synchronize)
    }
  }, [])

  const setHideNsfw = React.useCallback((next: boolean) => {
    setHideNsfwState(next)
    try {
      globalThis.localStorage?.setItem(storageKey, String(next))
      globalThis.dispatchEvent?.(new Event(preferenceEvent))
    } catch {
      // The in-memory preference remains usable when storage is unavailable.
    }
  }, [])

  return [hideNsfw, setHideNsfw] as const
}

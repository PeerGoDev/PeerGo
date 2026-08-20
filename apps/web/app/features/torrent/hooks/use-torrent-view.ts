import * as React from "react"

import {
  parseTorrentView,
  type TorrentView,
} from "~/features/torrent/model/torrent-view"

const STORAGE_KEY = "peergo-torrent-view"
// Read the former frontend's preference once so a same-origin rollout does not
// unexpectedly switch a member from their familiar poster/list presentation.
// PeerGo only writes its namespaced key and therefore remains independent from
// the legacy application's storage contract after the first choice.
const LEGACY_STORAGE_KEY = "torrent_view_mode"

function readStoredView() {
  try {
    return (
      parseTorrentView(globalThis.localStorage?.getItem(STORAGE_KEY)) ??
      parseTorrentView(globalThis.localStorage?.getItem(LEGACY_STORAGE_KEY))
    )
  } catch {
    return undefined
  }
}

/**
 * Keeps the member's catalog presentation consistent across all torrent
 * surfaces. The staff-configured view remains the fallback until the member
 * explicitly chooses a preference, and storage events synchronize open tabs.
 */
export function useTorrentView(
  configuredView: TorrentView | undefined
): readonly [TorrentView, (view: TorrentView) => void] {
  const [preferredView, setPreferredView] = React.useState<
    TorrentView | undefined
  >(readStoredView)

  React.useEffect(() => {
    function handleStorage(event: StorageEvent) {
      if (event.key === STORAGE_KEY) {
        setPreferredView(parseTorrentView(event.newValue))
      } else if (
        event.key === LEGACY_STORAGE_KEY &&
        !globalThis.localStorage?.getItem(STORAGE_KEY)
      ) {
        setPreferredView(parseTorrentView(event.newValue))
      }
    }

    globalThis.addEventListener?.("storage", handleStorage)
    return () => globalThis.removeEventListener?.("storage", handleStorage)
  }, [])

  const setView = React.useCallback((view: TorrentView) => {
    setPreferredView(view)
    try {
      globalThis.localStorage?.setItem(STORAGE_KEY, view)
    } catch {
      // The in-memory preference still works when storage is unavailable.
    }
  }, [])

  return [preferredView ?? configuredView ?? "list", setView] as const
}

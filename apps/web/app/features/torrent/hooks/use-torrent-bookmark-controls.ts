import * as React from "react"

import { useWebSession } from "~/features/auth/api/session.mutations"
import { useCapabilities } from "~/features/authz/api/capabilities.queries"
import {
  useSetTorrentBookmark,
  useTorrentBookmarkStatuses,
} from "~/features/torrent/api/torrent-bookmarks.queries"

export type TorrentBookmarkControls = {
  visible: boolean
  writable: boolean
  ready: boolean
  busy: boolean
  bookmarkedIds: ReadonlySet<number>
  pendingTorrentId: number | undefined
  failedTorrentId: number | undefined
  toggle: (torrentId: number) => void
}

export function useTorrentBookmarkControls(
  torrentIds: readonly number[]
): TorrentBookmarkControls {
  const normalizedIds = React.useMemo(
    () =>
      [...new Set(torrentIds.filter(Number.isSafeInteger))].sort(
        (left, right) => left - right
      ),
    [torrentIds]
  )
  const session = useWebSession()
  const capabilities = useCapabilities(session.data?.user.id)
  const canRead = Boolean(
    capabilities.data?.items.some(
      (capability) => capability.action === "torrent.bookmark.read.self"
    )
  )
  const canWrite = Boolean(
    capabilities.data?.items.some(
      (capability) => capability.action === "torrent.bookmark.write.self"
    )
  )
  const statuses = useTorrentBookmarkStatuses(
    session.data?.user.id,
    normalizedIds,
    canRead
  )
  const mutation = useSetTorrentBookmark(
    session.data?.user.id,
    session.data?.csrf_token,
    normalizedIds
  )
  const bookmarkedIds = React.useMemo(
    () => new Set(statuses.data?.bookmarked_ids ?? []),
    [statuses.data?.bookmarked_ids]
  )

  return {
    visible: Boolean(session.data && canRead),
    writable: canWrite,
    ready: canRead && !statuses.isPending && !statuses.isError,
    busy: mutation.isPending,
    bookmarkedIds,
    pendingTorrentId: mutation.isPending
      ? mutation.variables?.torrentId
      : undefined,
    failedTorrentId: mutation.isError
      ? mutation.variables?.torrentId
      : undefined,
    toggle: (torrentId) => {
      if (!canWrite || mutation.isPending) {
        return
      }
      mutation.reset()
      mutation.mutate({
        torrentId,
        bookmarked: !bookmarkedIds.has(torrentId),
      })
    },
  }
}

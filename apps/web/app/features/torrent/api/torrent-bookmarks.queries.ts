import {
  queryOptions,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query"

import type { components } from "~/generated/api"
import { apiClient } from "~/shared/api/client"
import { ApiProblemError } from "~/shared/api/problem"

export type TorrentBookmarkPage = components["schemas"]["TorrentBookmarkPage"]
export type TorrentBookmarkStatusList =
  components["schemas"]["TorrentBookmarkStatusList"]

export const torrentBookmarkKeys = {
  all: (userId: string) =>
    ["torrents", "current-user", userId, "bookmarks"] as const,
  lists: (userId: string) =>
    [...torrentBookmarkKeys.all(userId), "list"] as const,
  list: (userId: string, limit: number, offset: number) =>
    [...torrentBookmarkKeys.lists(userId), { limit, offset }] as const,
  statuses: (userId: string, torrentIds: readonly number[]) =>
    [
      ...torrentBookmarkKeys.all(userId),
      "statuses",
      { torrentIds: normalizeTorrentIds(torrentIds) },
    ] as const,
}

export function myTorrentBookmarksQueryOptions(
  userId: string,
  limit: number,
  offset: number
) {
  return queryOptions({
    queryKey: torrentBookmarkKeys.list(userId, limit, offset),
    queryFn: async (): Promise<TorrentBookmarkPage> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/me/torrent-bookmarks",
        { params: { query: { limit, offset } } }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    staleTime: 15_000,
    retry: false,
  })
}

export function torrentBookmarkStatusesQueryOptions(
  userId: string,
  torrentIds: readonly number[]
) {
  const normalizedIds = normalizeTorrentIds(torrentIds)
  return queryOptions({
    queryKey: torrentBookmarkKeys.statuses(userId, normalizedIds),
    queryFn: async (): Promise<TorrentBookmarkStatusList> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/me/torrent-bookmark-statuses",
        { params: { query: { torrent_id: normalizedIds } } }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    staleTime: 15_000,
    retry: false,
  })
}

export function useMyTorrentBookmarks(
  userId: string | undefined,
  limit: number,
  offset: number,
  enabled = true
) {
  return useQuery({
    ...myTorrentBookmarksQueryOptions(userId ?? "anonymous", limit, offset),
    enabled: Boolean(userId) && enabled,
  })
}

export function useTorrentBookmarkStatuses(
  userId: string | undefined,
  torrentIds: readonly number[],
  enabled = true
) {
  const normalizedIds = normalizeTorrentIds(torrentIds)
  return useQuery({
    ...torrentBookmarkStatusesQueryOptions(
      userId ?? "anonymous",
      normalizedIds
    ),
    enabled: Boolean(userId) && normalizedIds.length > 0 && enabled,
  })
}

export function useSetTorrentBookmark(
  userId: string | undefined,
  csrfToken: string | undefined,
  visibleTorrentIds: readonly number[]
) {
  const queryClient = useQueryClient()
  const normalizedIds = normalizeTorrentIds(visibleTorrentIds)
  const statusKey = torrentBookmarkKeys.statuses(
    userId ?? "anonymous",
    normalizedIds
  )

  return useMutation({
    mutationFn: async ({
      torrentId,
      bookmarked,
    }: {
      torrentId: number
      bookmarked: boolean
    }) => {
      if (!userId || !csrfToken) {
        throw new Error("bookmark session is unavailable")
      }
      const request = {
        params: {
          path: { torrent_id: torrentId },
          header: { "X-CSRF-Token": csrfToken },
        },
      }
      if (bookmarked) {
        const { data, error, response } = await apiClient.PUT(
          "/api/v1/me/torrent-bookmarks/{torrent_id}",
          request
        )
        if (!response.ok || !data) {
          throw new ApiProblemError(response.status, error)
        }
        return
      }
      const { error, response } = await apiClient.DELETE(
        "/api/v1/me/torrent-bookmarks/{torrent_id}",
        request
      )
      if (!response.ok) {
        throw new ApiProblemError(response.status, error)
      }
    },
    onMutate: async ({ torrentId, bookmarked }) => {
      await queryClient.cancelQueries({ queryKey: statusKey })
      const previous =
        queryClient.getQueryData<TorrentBookmarkStatusList>(statusKey)
      queryClient.setQueryData<TorrentBookmarkStatusList>(
        statusKey,
        (current) => {
          const ids = new Set(current?.bookmarked_ids ?? [])
          if (bookmarked) {
            ids.add(torrentId)
          } else {
            ids.delete(torrentId)
          }
          return { bookmarked_ids: [...ids].sort() }
        }
      )
      return { previous }
    },
    onError: (_error, _variables, context) => {
      if (context?.previous) {
        queryClient.setQueryData(statusKey, context.previous)
      } else {
        queryClient.removeQueries({ queryKey: statusKey, exact: true })
      }
    },
    onSettled: async () => {
      if (!userId) {
        return
      }
      await Promise.all([
        queryClient.invalidateQueries({
          queryKey: torrentBookmarkKeys.lists(userId),
        }),
        queryClient.invalidateQueries({
          queryKey: [...torrentBookmarkKeys.all(userId), "statuses"],
        }),
      ])
    },
  })
}

function normalizeTorrentIds(torrentIds: readonly number[]) {
  return [...new Set(torrentIds.filter(Number.isSafeInteger))].sort(
    (left, right) => left - right
  )
}

import {
  queryOptions,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query"

import { economyKeys } from "~/features/economy/api/economy.queries"
import type { components } from "~/generated/api"
import { apiClient } from "~/shared/api/client"
import { ApiProblemError } from "~/shared/api/problem"

export type TorrentPurchaseHistoryPage =
  components["schemas"]["TorrentPurchaseHistoryPage"]
export type TorrentPurchaseStatus =
  components["schemas"]["TorrentPurchaseStatus"]
export type TorrentPurchaseReceipt =
  components["schemas"]["TorrentPurchaseReceipt"]

export const torrentPurchaseHistoryKeys = {
  all: ["torrents", "current-user", "purchases"] as const,
  status: (userId: string, torrentId: number) =>
    [...torrentPurchaseHistoryKeys.all, userId, "status", torrentId] as const,
  list: (userId: string, limit: number, offset: number) =>
    [...torrentPurchaseHistoryKeys.all, userId, { limit, offset }] as const,
}

export function useTorrentPurchaseStatus(
  userId: string | undefined,
  torrentId: number,
  enabled: boolean
) {
  return useQuery({
    queryKey: torrentPurchaseHistoryKeys.status(
      userId ?? "anonymous",
      torrentId
    ),
    queryFn: async (): Promise<TorrentPurchaseStatus> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/torrents/{torrent_id}/purchase",
        { params: { path: { torrent_id: torrentId } } }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    enabled: Boolean(userId) && enabled,
    staleTime: 10_000,
    retry: false,
  })
}

export function usePurchaseTorrent(
  userId: string | undefined,
  csrfToken: string | undefined,
  torrentId: number
) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (
      idempotencyKey: string
    ): Promise<TorrentPurchaseReceipt> => {
      if (!csrfToken) {
        throw new Error("missing CSRF token")
      }
      const { data, error, response } = await apiClient.POST(
        "/api/v1/torrents/{torrent_id}/purchase",
        {
          params: {
            path: { torrent_id: torrentId },
            header: {
              "X-CSRF-Token": csrfToken,
              "Idempotency-Key": idempotencyKey,
            },
          },
        }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({
          queryKey: torrentPurchaseHistoryKeys.status(
            userId ?? "anonymous",
            torrentId
          ),
        }),
        queryClient.invalidateQueries({
          queryKey: torrentPurchaseHistoryKeys.all,
        }),
        queryClient.invalidateQueries({ queryKey: economyKeys.all }),
      ])
    },
  })
}

export function myTorrentPurchasesQueryOptions(
  userId: string,
  limit: number,
  offset: number
) {
  return queryOptions({
    queryKey: torrentPurchaseHistoryKeys.list(userId, limit, offset),
    queryFn: async (): Promise<TorrentPurchaseHistoryPage> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/me/torrent-purchases",
        { params: { query: { limit, offset } } }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    staleTime: 10_000,
    retry: false,
  })
}

export function useMyTorrentPurchases(
  userId: string | undefined,
  limit: number,
  offset: number,
  enabled: boolean
) {
  return useQuery({
    ...myTorrentPurchasesQueryOptions(userId ?? "anonymous", limit, offset),
    enabled: Boolean(userId) && enabled,
  })
}

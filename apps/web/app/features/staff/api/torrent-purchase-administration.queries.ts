import {
  queryOptions,
  useMutation,
  useQueryClient,
} from "@tanstack/react-query"

import type { components } from "~/generated/api"
import { economyKeys } from "~/features/economy/api/economy.queries"
import { operationsKeys } from "~/features/staff/api/operations.queries"
import type { ManagedPurchaseFilters } from "~/features/staff/model/torrent-purchase-administration"
import { torrentPurchaseHistoryKeys } from "~/features/torrent/api/torrent-purchases.queries"
import { apiClient } from "~/shared/api/client"
import { ApiProblemError } from "~/shared/api/problem"

export type ManagedTorrentPurchase =
  components["schemas"]["ManagedTorrentPurchase"]
export type ManagedTorrentPurchasePage =
  components["schemas"]["ManagedTorrentPurchasePage"]
export type RefundTorrentPurchaseRequest =
  components["schemas"]["RefundTorrentPurchaseRequest"]
export type TorrentPurchaseRefundReceipt =
  components["schemas"]["TorrentPurchaseRefundReceipt"]

export const torrentPurchaseAdministrationKeys = {
  all: ["staff", "torrent-purchases"] as const,
  list: (filters: ManagedPurchaseFilters) =>
    [...torrentPurchaseAdministrationKeys.all, "list", filters] as const,
}

export function managedTorrentPurchaseListQueryOptions(
  filters: ManagedPurchaseFilters
) {
  return queryOptions({
    queryKey: torrentPurchaseAdministrationKeys.list(filters),
    queryFn: async (): Promise<ManagedTorrentPurchasePage> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/admin/torrent-purchases",
        {
          params: {
            query: {
              query: filters.query || undefined,
              status: filters.status === "all" ? undefined : filters.status,
              source: filters.source === "all" ? undefined : filters.source,
              limit: filters.pageSize,
              offset: (filters.page - 1) * filters.pageSize,
            },
          },
        }
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

export function useRefundTorrentPurchase() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      buyerNumericId: number
      torrentId: number
      csrfToken: string
      idempotencyKey: string
      body: RefundTorrentPurchaseRequest
    }): Promise<TorrentPurchaseRefundReceipt> => {
      const { data, error, response } = await apiClient.POST(
        "/api/v1/admin/torrent-purchases/{user_numeric_id}/{torrent_id}/refund",
        {
          params: {
            path: {
              user_numeric_id: input.buyerNumericId,
              torrent_id: input.torrentId,
            },
            header: {
              "X-CSRF-Token": input.csrfToken,
              "Idempotency-Key": input.idempotencyKey,
            },
          },
          body: input.body,
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
          queryKey: torrentPurchaseAdministrationKeys.all,
        }),
        queryClient.invalidateQueries({
          queryKey: torrentPurchaseHistoryKeys.all,
        }),
        queryClient.invalidateQueries({ queryKey: economyKeys.all }),
        queryClient.invalidateQueries({ queryKey: operationsKeys.all }),
      ])
    },
  })
}

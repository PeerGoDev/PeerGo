import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"

import { economyKeys } from "~/features/economy/api/economy.queries"
import type { components } from "~/generated/api"
import { apiClient } from "~/shared/api/client"
import { ApiProblemError } from "~/shared/api/problem"

export type PromotionProductOffer =
  components["schemas"]["PromotionProductOffer"]
export type PromotionProductOrder =
  components["schemas"]["PromotionProductOrder"]
export type PromotionProductOrderPage =
  components["schemas"]["PromotionProductOrderPage"]
export type PurchasePromotionProductsRequest =
  components["schemas"]["PurchasePromotionProductsRequest"]

export const promotionProductKeys = {
  all: ["promotion-products"] as const,
  offer: (userId: string, torrentId: number) =>
    [...promotionProductKeys.all, "offer", userId, torrentId] as const,
  orders: (userId: string, limit: number, offset: number) =>
    [...promotionProductKeys.all, "orders", userId, limit, offset] as const,
}

export function usePromotionProductOffer(
  userId: string | undefined,
  torrentId: number,
  enabled: boolean
) {
  return useQuery({
    queryKey: promotionProductKeys.offer(userId ?? "anonymous", torrentId),
    queryFn: async (): Promise<PromotionProductOffer> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/torrents/{torrent_id}/promotion-products",
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

export function usePurchasePromotionProducts(input: {
  userId: string | undefined
  csrfToken: string | undefined
  torrentId: number
}) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (command: {
      idempotencyKey: string
      body: PurchasePromotionProductsRequest
    }): Promise<PromotionProductOrder> => {
      if (!input.csrfToken) throw new Error("missing CSRF token")
      const { data, error, response } = await apiClient.POST(
        "/api/v1/torrents/{torrent_id}/promotion-products",
        {
          params: {
            path: { torrent_id: input.torrentId },
            header: {
              "X-CSRF-Token": input.csrfToken,
              "Idempotency-Key": command.idempotencyKey,
            },
          },
          body: command.body,
        }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: promotionProductKeys.all }),
        queryClient.invalidateQueries({ queryKey: economyKeys.all }),
        queryClient.invalidateQueries({ queryKey: ["torrents"] }),
        queryClient.invalidateQueries({ queryKey: ["staff", "promotions"] }),
      ])
    },
  })
}

export function useMyPromotionProductOrders(
  userId: string | undefined,
  limit: number,
  offset: number,
  enabled: boolean
) {
  return useQuery({
    queryKey: promotionProductKeys.orders(userId ?? "anonymous", limit, offset),
    queryFn: async (): Promise<PromotionProductOrderPage> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/me/promotion-product-orders",
        { params: { query: { limit, offset } } }
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

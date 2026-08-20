import {
  queryOptions,
  useMutation,
  useQueryClient,
} from "@tanstack/react-query"

import type { components } from "~/generated/api"
import { apiClient } from "~/shared/api/client"
import { ApiProblemError } from "~/shared/api/problem"

export type PromotionCampaign = components["schemas"]["PromotionCampaign"]
export type PromotionCampaignPage =
  components["schemas"]["PromotionCampaignPage"]
export type SchedulePromotionCampaignRequest =
  components["schemas"]["SchedulePromotionCampaignRequest"]
export type PromotionProductPolicy =
  components["schemas"]["PromotionProductPolicy"]
export type PromotionProductOrderPage =
  components["schemas"]["PromotionProductOrderPage"]
export type UpdatePromotionProductPolicyRequest =
  components["schemas"]["UpdatePromotionProductPolicyRequest"]

export const promotionAdministrationKeys = {
  all: ["staff", "promotions"] as const,
  list: (limit: number, offset: number) =>
    [...promotionAdministrationKeys.all, "list", limit, offset] as const,
  productPolicy: () =>
    [...promotionAdministrationKeys.all, "product-policy"] as const,
  productOrders: (query: string, limit: number, offset: number) =>
    [
      ...promotionAdministrationKeys.all,
      "product-orders",
      query,
      limit,
      offset,
    ] as const,
}

export function promotionProductPolicyQueryOptions() {
  return queryOptions({
    queryKey: promotionAdministrationKeys.productPolicy(),
    queryFn: async (): Promise<PromotionProductPolicy> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/admin/settings/promotion-products"
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

export function promotionProductOrdersQueryOptions(
  query = "",
  limit = 20,
  offset = 0
) {
  return queryOptions({
    queryKey: promotionAdministrationKeys.productOrders(query, limit, offset),
    queryFn: async (): Promise<PromotionProductOrderPage> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/admin/promotion-product-orders",
        {
          params: {
            query: { query: query || undefined, limit, offset },
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

export function promotionCampaignListQueryOptions(limit = 50, offset = 0) {
  return queryOptions({
    queryKey: promotionAdministrationKeys.list(limit, offset),
    queryFn: async (): Promise<PromotionCampaignPage> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/admin/promotions",
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

export function useSchedulePromotionCampaign() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      csrfToken: string
      idempotencyKey: string
      body: SchedulePromotionCampaignRequest
    }): Promise<PromotionCampaign> => {
      const { data, error, response } = await apiClient.POST(
        "/api/v1/admin/promotions",
        {
          params: {
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
      await queryClient.invalidateQueries({
        queryKey: promotionAdministrationKeys.all,
      })
      await queryClient.invalidateQueries({
        queryKey: ["staff", "torrents", "administration"],
      })
      await queryClient.invalidateQueries({ queryKey: ["torrents"] })
    },
  })
}

export function useUpdatePromotionProductPolicy() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      csrfToken: string
      idempotencyKey: string
      body: UpdatePromotionProductPolicyRequest
    }): Promise<PromotionProductPolicy> => {
      const { data, error, response } = await apiClient.PUT(
        "/api/v1/admin/settings/promotion-products",
        {
          params: {
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
      await queryClient.invalidateQueries({
        queryKey: promotionAdministrationKeys.all,
      })
    },
  })
}

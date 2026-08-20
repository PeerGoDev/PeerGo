import {
  queryOptions,
  useMutation,
  useQueryClient,
} from "@tanstack/react-query"

import type { components } from "~/generated/api"
import { apiClient } from "~/shared/api/client"
import { ApiProblemError } from "~/shared/api/problem"

export type RSSSubscription = components["schemas"]["RSSSubscription"]
export type RSSSubscriptionInput = components["schemas"]["RSSSubscriptionInput"]
export type IssuedRSSSubscription =
  components["schemas"]["IssuedRSSSubscription"]

export const rssKeys = {
  all: ["rss-subscriptions"] as const,
  list: (userId: string | undefined) => [...rssKeys.all, userId] as const,
}

export function rssSubscriptionsQueryOptions(userId: string | undefined) {
  return queryOptions({
    queryKey: rssKeys.list(userId),
    queryFn: async (): Promise<RSSSubscription[]> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/me/rss-subscriptions"
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data.items
    },
    enabled: Boolean(userId),
    staleTime: 10_000,
    retry: false,
  })
}

export function useCreateRSSSubscription() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      csrfToken: string
      body: RSSSubscriptionInput
    }): Promise<IssuedRSSSubscription> => {
      const { data, error, response } = await apiClient.POST(
        "/api/v1/me/rss-subscriptions",
        {
          params: { header: { "X-CSRF-Token": input.csrfToken } },
          body: input.body,
        }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: rssKeys.all })
    },
  })
}

export function useUpdateRSSSubscription() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      csrfToken: string
      subscription: RSSSubscription
      body: RSSSubscriptionInput
    }): Promise<RSSSubscription> => {
      const { data, error, response } = await apiClient.PUT(
        "/api/v1/me/rss-subscriptions/{subscription_id}",
        {
          params: {
            path: { subscription_id: input.subscription.id },
            header: { "X-CSRF-Token": input.csrfToken },
          },
          body: {
            ...input.body,
            expected_version: input.subscription.version,
          },
        }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: rssKeys.all })
    },
  })
}

export function useRotateRSSSubscriptionToken() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      csrfToken: string
      subscription: RSSSubscription
    }): Promise<IssuedRSSSubscription> => {
      const { data, error, response } = await apiClient.POST(
        "/api/v1/me/rss-subscriptions/{subscription_id}/token-rotations",
        {
          params: {
            path: { subscription_id: input.subscription.id },
            header: { "X-CSRF-Token": input.csrfToken },
          },
          body: { expected_version: input.subscription.version },
        }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: rssKeys.all })
    },
  })
}

export function useDeleteRSSSubscription() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      csrfToken: string
      subscription: RSSSubscription
    }): Promise<void> => {
      const { error, response } = await apiClient.DELETE(
        "/api/v1/me/rss-subscriptions/{subscription_id}",
        {
          params: {
            path: { subscription_id: input.subscription.id },
            query: { expected_version: input.subscription.version },
            header: { "X-CSRF-Token": input.csrfToken },
          },
        }
      )
      if (!response.ok) {
        throw new ApiProblemError(response.status, error)
      }
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: rssKeys.all })
    },
  })
}

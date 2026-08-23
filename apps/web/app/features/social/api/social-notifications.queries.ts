import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"

import type { components } from "~/generated/api"
import { apiClient } from "~/shared/api/client"
import { ApiProblemError } from "~/shared/api/problem"

export type SocialNotification = components["schemas"]["SocialNotification"]
export type SocialNotificationCategory =
  components["schemas"]["SocialNotificationCategory"]
export type SocialNotificationPage =
  components["schemas"]["SocialNotificationPage"]
export type SocialNotificationSummary =
  components["schemas"]["SocialNotificationSummary"]

export const socialNotificationKeys = {
  all: (userId: string) =>
    ["social", "notifications", "current-user", userId] as const,
  pages: (userId: string) =>
    [...socialNotificationKeys.all(userId), "page"] as const,
  page: (
    userId: string,
    category: SocialNotificationCategory,
    limit: number,
    offset: number
  ) =>
    [
      ...socialNotificationKeys.pages(userId),
      { category, limit, offset },
    ] as const,
  summary: (userId: string) =>
    [...socialNotificationKeys.all(userId), "summary"] as const,
}

export function useSocialNotifications(
  userId: string | undefined,
  category: SocialNotificationCategory,
  limit: number,
  offset: number,
  enabled = true
) {
  return useQuery({
    queryKey: socialNotificationKeys.page(
      userId ?? "anonymous",
      category,
      limit,
      offset
    ),
    queryFn: async (): Promise<SocialNotificationPage> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/social/notifications",
        { params: { query: { category, limit, offset } } }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    enabled: Boolean(userId) && enabled,
    staleTime: 15_000,
    retry: false,
  })
}

export function useSocialNotificationSummary(
  userId: string | undefined,
  enabled = true
) {
  return useQuery({
    queryKey: socialNotificationKeys.summary(userId ?? "anonymous"),
    queryFn: async (): Promise<SocialNotificationSummary> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/social/notifications/summary"
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    enabled: Boolean(userId) && enabled,
    staleTime: 15_000,
    refetchInterval: 60_000,
    retry: false,
  })
}

export function useMarkSocialNotificationRead(
  userId: string | undefined,
  csrfToken: string | undefined
) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (notificationId: string) => {
      if (!userId || !csrfToken) {
        throw new Error("social notification session is unavailable")
      }
      const { data, error, response } = await apiClient.PUT(
        "/api/v1/social/notifications/{notification_id}/read",
        {
          params: {
            path: { notification_id: notificationId },
            header: { "X-CSRF-Token": csrfToken },
          },
        }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    onSuccess: async () =>
      refreshSocialNotificationQueries(queryClient, userId),
  })
}

export function useMarkAllSocialNotificationsRead(
  userId: string | undefined,
  csrfToken: string | undefined
) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async () => {
      if (!userId || !csrfToken) {
        throw new Error("social notification session is unavailable")
      }
      const { data, error, response } = await apiClient.PUT(
        "/api/v1/social/notifications/read-all",
        { params: { header: { "X-CSRF-Token": csrfToken } } }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    onSuccess: async () =>
      refreshSocialNotificationQueries(queryClient, userId),
  })
}

async function refreshSocialNotificationQueries(
  queryClient: ReturnType<typeof useQueryClient>,
  userId: string | undefined
) {
  if (!userId) return
  await Promise.all([
    queryClient.invalidateQueries({
      queryKey: socialNotificationKeys.pages(userId),
    }),
    queryClient.invalidateQueries({
      queryKey: socialNotificationKeys.summary(userId),
    }),
  ])
}

import {
  queryOptions,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query"

import type { components } from "~/generated/api"
import { apiClient } from "~/shared/api/client"
import { ApiProblemError } from "~/shared/api/problem"

export type MyNotificationPage = components["schemas"]["MyNotificationPage"]
export type MyNotificationSummary =
  components["schemas"]["MyNotificationSummary"]

export const notificationKeys = {
  all: (userId: string) => ["notifications", "current-user", userId] as const,
  pages: (userId: string) => [...notificationKeys.all(userId), "page"] as const,
  page: (userId: string, limit: number, offset: number, unreadOnly: boolean) =>
    [...notificationKeys.pages(userId), { limit, offset, unreadOnly }] as const,
  summary: (userId: string) =>
    [...notificationKeys.all(userId), "summary"] as const,
}

export function myNotificationPageQueryOptions(
  userId: string,
  limit: number,
  offset: number,
  unreadOnly: boolean
) {
  return queryOptions({
    queryKey: notificationKeys.page(userId, limit, offset, unreadOnly),
    queryFn: async (): Promise<MyNotificationPage> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/me/notifications",
        { params: { query: { limit, offset, unread_only: unreadOnly } } }
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

export function myNotificationSummaryQueryOptions(userId: string) {
  return queryOptions({
    queryKey: notificationKeys.summary(userId),
    queryFn: async (): Promise<MyNotificationSummary> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/me/notifications/summary"
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

export function useMyNotificationPage(
  userId: string | undefined,
  limit: number,
  offset: number,
  unreadOnly: boolean,
  enabled = true
) {
  return useQuery({
    ...myNotificationPageQueryOptions(
      userId ?? "anonymous",
      limit,
      offset,
      unreadOnly
    ),
    enabled: Boolean(userId) && enabled,
  })
}

export function useMyNotificationSummary(
  userId: string | undefined,
  enabled = true
) {
  return useQuery({
    ...myNotificationSummaryQueryOptions(userId ?? "anonymous"),
    enabled: Boolean(userId) && enabled,
  })
}

export function useMarkMyNotificationRead(
  userId: string | undefined,
  csrfToken: string | undefined
) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (notificationId: string) => {
      if (!userId || !csrfToken) {
        throw new Error("notification session is unavailable")
      }
      const { data, error, response } = await apiClient.PUT(
        "/api/v1/me/notifications/{notification_id}/read",
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
    onSuccess: async () => refreshNotificationQueries(queryClient, userId),
  })
}

export function useMarkAllMyNotificationsRead(
  userId: string | undefined,
  csrfToken: string | undefined
) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async () => {
      if (!userId || !csrfToken) {
        throw new Error("notification session is unavailable")
      }
      const { data, error, response } = await apiClient.PUT(
        "/api/v1/me/notifications/read-all",
        { params: { header: { "X-CSRF-Token": csrfToken } } }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    onSuccess: async () => refreshNotificationQueries(queryClient, userId),
  })
}

export function useArchiveAllMyNotifications(
  userId: string | undefined,
  csrfToken: string | undefined
) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async () => {
      if (!userId || !csrfToken) {
        throw new Error("notification session is unavailable")
      }
      const { data, error, response } = await apiClient.PUT(
        "/api/v1/me/notifications/archive-all",
        { params: { header: { "X-CSRF-Token": csrfToken } } }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    onSuccess: async () => refreshNotificationQueries(queryClient, userId),
  })
}

export function useCreateNotificationFeedback(
  userId: string | undefined,
  csrfToken: string | undefined
) {
  return useMutation({
    mutationFn: async (input: { title: string; content: string }) => {
      if (!userId || !csrfToken) {
        throw new Error("notification session is unavailable")
      }
      const { data, error, response } = await apiClient.POST(
        "/api/v1/me/notifications/feedback",
        {
          params: { header: { "X-CSRF-Token": csrfToken } },
          body: input,
        }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
  })
}

async function refreshNotificationQueries(
  queryClient: ReturnType<typeof useQueryClient>,
  userId: string | undefined
) {
  if (!userId) return
  await Promise.all([
    queryClient.invalidateQueries({ queryKey: notificationKeys.pages(userId) }),
    queryClient.invalidateQueries({
      queryKey: notificationKeys.summary(userId),
    }),
  ])
}

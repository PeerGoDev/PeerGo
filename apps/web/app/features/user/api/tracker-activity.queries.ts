import { queryOptions, useQuery } from "@tanstack/react-query"

import type { components } from "~/generated/api"
import { apiClient } from "~/shared/api/client"
import { ApiProblemError } from "~/shared/api/problem"

export type UserTrackerActivity = components["schemas"]["UserTrackerActivity"]

export const trackerActivityKeys = {
  all: ["user-tracker-activity"] as const,
  mine: (userId: string) =>
    [...trackerActivityKeys.all, "mine", userId] as const,
  managed: (userId: string) =>
    [...trackerActivityKeys.all, "managed", userId] as const,
}

export function myTrackerActivityQueryOptions(userId: string) {
  return queryOptions({
    queryKey: trackerActivityKeys.mine(userId),
    queryFn: async (): Promise<UserTrackerActivity> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/me/tracker-activity"
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    enabled: Boolean(userId),
    staleTime: 10_000,
    refetchInterval: 30_000,
    retry: false,
  })
}

export function managedTrackerActivityQueryOptions(userId: string) {
  return queryOptions({
    queryKey: trackerActivityKeys.managed(userId),
    queryFn: async (): Promise<UserTrackerActivity> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/admin/users/{user_id}/tracker-activity",
        { params: { path: { user_id: userId } } }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    enabled: Boolean(userId),
    staleTime: 10_000,
    refetchInterval: 30_000,
    retry: false,
  })
}

export function useMyTrackerActivity(userId: string | undefined) {
  return useQuery({
    ...myTrackerActivityQueryOptions(userId ?? "anonymous"),
    enabled: Boolean(userId),
  })
}

import { queryOptions, useQuery } from "@tanstack/react-query"

import type { components } from "~/generated/api"
import { apiClient } from "~/shared/api/client"
import { ApiProblemError } from "~/shared/api/problem"

export type TrafficOverview = components["schemas"]["TrafficOverview"]

export const trafficKeys = {
  all: ["traffic"] as const,
  current: (userId: string) =>
    [...trafficKeys.all, "current-user", userId] as const,
}

export function trafficOverviewQueryOptions(userId: string) {
  return queryOptions({
    queryKey: trafficKeys.current(userId),
    queryFn: async (): Promise<TrafficOverview> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/me/traffic",
        { params: { query: { limit: 20 } } }
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

export function useTrafficOverview(userId: string | undefined) {
  return useQuery({
    ...trafficOverviewQueryOptions(userId ?? "anonymous"),
    enabled: Boolean(userId),
  })
}

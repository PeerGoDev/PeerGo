import { queryOptions, useQuery } from "@tanstack/react-query"

import type { components } from "~/generated/api"
import { apiClient } from "~/shared/api/client"
import { ApiProblemError } from "~/shared/api/problem"

export type EconomyOverview = components["schemas"]["EconomyOverview"]

export const economyKeys = {
  all: ["economy"] as const,
  current: (userId: string) => [...economyKeys.all, "current", userId] as const,
}

export function economyOverviewQueryOptions(userId: string) {
  return queryOptions({
    queryKey: economyKeys.current(userId),
    queryFn: async (): Promise<EconomyOverview> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/me/economy",
        { params: { query: { limit: 30 } } }
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

export function useEconomyOverview(userId: string | undefined) {
  return useQuery({
    ...economyOverviewQueryOptions(userId ?? "anonymous"),
    enabled: Boolean(userId),
  })
}

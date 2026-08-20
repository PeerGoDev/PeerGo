import { queryOptions, useQuery } from "@tanstack/react-query"

import type { components } from "~/generated/api"
import { apiClient } from "~/shared/api/client"
import { ApiProblemError } from "~/shared/api/problem"

export type CapabilityList = components["schemas"]["CapabilityList"]

export const capabilityKeys = {
  all: ["capabilities"] as const,
  current: (userId: string) =>
    [...capabilityKeys.all, "current-user", userId] as const,
}

export function capabilityQueryOptions(userId: string) {
  return queryOptions({
    queryKey: capabilityKeys.current(userId),
    queryFn: async (): Promise<CapabilityList> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/me/capabilities"
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    staleTime: 30_000,
    retry: false,
  })
}

export function useCapabilities(userId: string | undefined) {
  return useQuery({
    ...capabilityQueryOptions(userId ?? "anonymous"),
    enabled: Boolean(userId),
  })
}

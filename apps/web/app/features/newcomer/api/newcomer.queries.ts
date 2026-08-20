import { queryOptions, useQuery } from "@tanstack/react-query"

import type { components } from "~/generated/api"
import { apiClient } from "~/shared/api/client"
import { ApiProblemError } from "~/shared/api/problem"

export type MyNewcomerAssessmentStatus =
  components["schemas"]["MyNewcomerAssessmentStatus"]

export const newcomerKeys = {
  all: ["newcomer-assessment"] as const,
  current: (userId: string) =>
    [...newcomerKeys.all, "current-user", userId] as const,
}

export function myNewcomerAssessmentQueryOptions(userId: string) {
  return queryOptions({
    queryKey: newcomerKeys.current(userId),
    queryFn: async (): Promise<MyNewcomerAssessmentStatus> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/me/newcomer-assessment"
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

export function useMyNewcomerAssessment(
  userId: string | undefined,
  enabled: boolean
) {
  return useQuery({
    ...myNewcomerAssessmentQueryOptions(userId ?? "anonymous"),
    enabled: Boolean(userId && enabled),
  })
}

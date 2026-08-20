import {
  queryOptions,
  useMutation,
  useQueryClient,
} from "@tanstack/react-query"

import type { components } from "~/generated/api"
import { apiClient } from "~/shared/api/client"
import { ApiProblemError } from "~/shared/api/problem"

export type HNRAppeal = components["schemas"]["HNRAppeal"]
export type HNRAppealFilter = components["schemas"]["HNRAppealFilter"]
export type HNRAppealPage = components["schemas"]["HNRAppealPage"]

export const hnrAppealAdministrationKeys = {
  all: ["staff", "hnr-appeals"] as const,
  list: (filter: HNRAppealFilter) =>
    [...hnrAppealAdministrationKeys.all, filter] as const,
}

export function hnrAppealListQueryOptions(filter: HNRAppealFilter = "all") {
  return queryOptions({
    queryKey: hnrAppealAdministrationKeys.list(filter),
    queryFn: async (): Promise<HNRAppealPage> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/admin/hnr/appeals",
        { params: { query: { filter, limit: 30, offset: 0, q: "" } } }
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

export function useDecideHNRAppeal() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      appealId: string
      decision: "approved" | "rejected"
      expectedObligationVersion: number
      response: string
      csrfToken: string
    }): Promise<HNRAppeal> => {
      const { data, error, response } = await apiClient.POST(
        "/api/v1/admin/hnr/appeals/{appeal_id}/decision",
        {
          params: {
            path: { appeal_id: input.appealId },
            header: { "X-CSRF-Token": input.csrfToken },
          },
          body: {
            decision: input.decision,
            expected_obligation_version: input.expectedObligationVersion,
            response: input.response,
          },
        }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({
        queryKey: hnrAppealAdministrationKeys.all,
      })
    },
  })
}

import {
  queryOptions,
  useMutation,
  useQueryClient,
} from "@tanstack/react-query"

import type { components } from "~/generated/api"
import { userAdministrationKeys } from "~/features/staff/api/user-administration.queries"
import { apiClient } from "~/shared/api/client"
import { ApiProblemError } from "~/shared/api/problem"

export type AccountAccessAppeal = components["schemas"]["AccountAccessAppeal"]
export type AccountAccessAppealFilter =
  components["schemas"]["AccountAccessAppealFilter"]
export type AccountAccessAppealPage =
  components["schemas"]["AccountAccessAppealPage"]

export const accountAccessAppealAdministrationKeys = {
  all: ["staff", "identity", "account-access-appeals"] as const,
  list: (filter: AccountAccessAppealFilter) =>
    [...accountAccessAppealAdministrationKeys.all, filter] as const,
}

export function accountAccessAppealListQueryOptions(
  filter: AccountAccessAppealFilter = "pending",
  enabled = true
) {
  return queryOptions({
    queryKey: accountAccessAppealAdministrationKeys.list(filter),
    queryFn: async (): Promise<AccountAccessAppealPage> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/admin/account-access-appeals",
        {
          params: {
            query: { status: filter, limit: 30, offset: 0 },
          },
        }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    enabled,
    staleTime: 10_000,
    retry: false,
  })
}

export function useDecideAccountAccessAppeal() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      appealId: string
      decision: "approved" | "rejected"
      expectedSourceVersion: number
      response: string
      csrfToken: string
    }): Promise<AccountAccessAppeal> => {
      const { data, error, response } = await apiClient.POST(
        "/api/v1/admin/account-access-appeals/{appeal_id}/decision",
        {
          params: {
            path: { appeal_id: input.appealId },
            header: { "X-CSRF-Token": input.csrfToken },
          },
          body: {
            decision: input.decision,
            expected_source_version: input.expectedSourceVersion,
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
      await Promise.all([
        queryClient.invalidateQueries({
          queryKey: accountAccessAppealAdministrationKeys.all,
        }),
        queryClient.invalidateQueries({ queryKey: userAdministrationKeys.all }),
      ])
    },
  })
}

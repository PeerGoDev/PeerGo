import {
  queryOptions,
  useMutation,
  useQueryClient,
} from "@tanstack/react-query"

import { operationsKeys } from "~/features/staff/api/operations.queries"
import type { components } from "~/generated/api"
import { apiClient } from "~/shared/api/client"
import { ApiProblemError } from "~/shared/api/problem"

export type ContentTipPolicy = components["schemas"]["ContentTipPolicy"]
export type ContentTipPolicyPage = components["schemas"]["ContentTipPolicyPage"]
export type ContentTipPolicySettings =
  components["schemas"]["ContentTipPolicySettings"]

export const contentTipAdministrationKeys = {
  all: ["staff", "content-tip-policies"] as const,
  list: () => [...contentTipAdministrationKeys.all, "list"] as const,
}

export function contentTipPolicyListQueryOptions() {
  return queryOptions({
    queryKey: contentTipAdministrationKeys.list(),
    queryFn: async (): Promise<ContentTipPolicyPage> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/admin/economy/content-tip-policies",
        { params: { query: { limit: 30, offset: 0 } } }
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

export function useIssueContentTipPolicy() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      csrfToken: string
      idempotencyKey: string
      settings: ContentTipPolicySettings
      reason: string
    }): Promise<ContentTipPolicy> => {
      const { data, error, response } = await apiClient.POST(
        "/api/v1/admin/economy/content-tip-policies",
        {
          params: {
            header: {
              "X-CSRF-Token": input.csrfToken,
              "Idempotency-Key": input.idempotencyKey,
            },
          },
          body: { settings: input.settings, reason: input.reason },
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
          queryKey: contentTipAdministrationKeys.all,
        }),
        queryClient.invalidateQueries({ queryKey: operationsKeys.all }),
      ])
    },
  })
}

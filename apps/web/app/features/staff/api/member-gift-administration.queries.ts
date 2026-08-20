import {
  queryOptions,
  useMutation,
  useQueryClient,
} from "@tanstack/react-query"

import { operationsKeys } from "~/features/staff/api/operations.queries"
import type { components } from "~/generated/api"
import { apiClient } from "~/shared/api/client"
import { ApiProblemError } from "~/shared/api/problem"

export type MemberGiftPolicy = components["schemas"]["MemberGiftPolicy"]
export type MemberGiftPolicyPage = components["schemas"]["MemberGiftPolicyPage"]
export type MemberGiftPolicySettings =
  components["schemas"]["MemberGiftPolicySettings"]

export const memberGiftAdministrationKeys = {
  all: ["staff", "member-gift-policies"] as const,
  list: () => [...memberGiftAdministrationKeys.all, "list"] as const,
}

export function memberGiftPolicyListQueryOptions() {
  return queryOptions({
    queryKey: memberGiftAdministrationKeys.list(),
    queryFn: async (): Promise<MemberGiftPolicyPage> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/admin/economy/member-gift-policies",
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

export function useIssueMemberGiftPolicy() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      csrfToken: string
      idempotencyKey: string
      settings: MemberGiftPolicySettings
      reason: string
    }): Promise<MemberGiftPolicy> => {
      const { data, error, response } = await apiClient.POST(
        "/api/v1/admin/economy/member-gift-policies",
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
          queryKey: memberGiftAdministrationKeys.all,
        }),
        queryClient.invalidateQueries({ queryKey: operationsKeys.all }),
      ])
    },
  })
}

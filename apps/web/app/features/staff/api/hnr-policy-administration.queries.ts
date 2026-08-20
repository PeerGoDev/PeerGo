import {
  queryOptions,
  useMutation,
  useQueryClient,
} from "@tanstack/react-query"

import type { components } from "~/generated/api"
import { apiClient } from "~/shared/api/client"
import { ApiProblemError } from "~/shared/api/problem"

export type HNRPolicyInput = components["schemas"]["HNRPolicyInput"]
export type HNRPolicyPreview = components["schemas"]["HNRPolicyPreview"]
export type HNRPolicyRevision = components["schemas"]["HNRPolicyRevision"]
export type HNRPolicyRevisionPage =
  components["schemas"]["HNRPolicyRevisionPage"]

export const hnrPolicyAdministrationKeys = {
  all: ["staff", "hnr-policy"] as const,
  list: () => [...hnrPolicyAdministrationKeys.all, "list"] as const,
}

export function hnrPolicyRevisionListQueryOptions() {
  return queryOptions({
    queryKey: hnrPolicyAdministrationKeys.list(),
    queryFn: async (): Promise<HNRPolicyRevisionPage> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/admin/settings/hnr",
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

export function usePreviewHNRPolicy() {
  return useMutation({
    mutationFn: async (body: HNRPolicyInput): Promise<HNRPolicyPreview> => {
      const { data, error, response } = await apiClient.POST(
        "/api/v1/admin/settings/hnr/preview",
        { body }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
  })
}

export function useIssueHNRPolicy() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      csrfToken: string
      idempotencyKey: string
      policy: HNRPolicyInput
      effectiveAt: string
      reason: string
    }): Promise<HNRPolicyRevision> => {
      const { data, error, response } = await apiClient.POST(
        "/api/v1/admin/settings/hnr",
        {
          params: {
            header: {
              "X-CSRF-Token": input.csrfToken,
              "Idempotency-Key": input.idempotencyKey,
            },
          },
          body: {
            policy: input.policy,
            effective_at: input.effectiveAt,
            reason: input.reason,
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
        queryKey: hnrPolicyAdministrationKeys.all,
      })
    },
  })
}

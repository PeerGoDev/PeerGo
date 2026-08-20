import {
  queryOptions,
  useMutation,
  useQueryClient,
} from "@tanstack/react-query"

import type { components } from "~/generated/api"
import { staffSessionKeys } from "~/features/staff/api/staff-session.mutations"
import { apiClient } from "~/shared/api/client"
import { ApiProblemError } from "~/shared/api/problem"

export type GrantAdministrationOverview =
  components["schemas"]["GrantAdministrationOverview"]
export type GrantAdministrationGrant =
  components["schemas"]["GrantAdministrationGrant"]
export type GrantRevocationRequest =
  components["schemas"]["GrantRevocationRequest"]
export type ReviewDomain = "governance" | "security"

export const governanceKeys = {
  all: [...staffSessionKeys.all, "governance"] as const,
  overview: () => [...governanceKeys.all, "overview"] as const,
}

export const governanceOverviewQueryOptions = queryOptions({
  queryKey: governanceKeys.overview(),
  queryFn: async (): Promise<GrantAdministrationOverview> => {
    const { data, error, response } = await apiClient.GET(
      "/api/v1/admin/authz/grants"
    )
    if (!response.ok || !data) {
      throw new ApiProblemError(response.status, error)
    }
    return data
  },
  staleTime: 10_000,
  retry: false,
})

export function useCreateGrantRevocation() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      csrfToken: string
      grantId: string
      expectedGrantVersion: number
      reason: string
    }): Promise<GrantRevocationRequest> => {
      const { data, error, response } = await apiClient.POST(
        "/api/v1/admin/authz/grant-revocations",
        {
          params: { header: { "X-CSRF-Token": input.csrfToken } },
          body: {
            grant_id: input.grantId,
            expected_grant_version: input.expectedGrantVersion,
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
      await queryClient.invalidateQueries({ queryKey: governanceKeys.all })
    },
  })
}

export function useReviewGrantRevocation() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      csrfToken: string
      requestId: string
      domain: ReviewDomain
      decision: "approve" | "reject"
      reason: string
    }): Promise<GrantRevocationRequest> => {
      const path =
        input.domain === "governance"
          ? "/api/v1/admin/authz/grant-revocations/{request_id}/governance-review"
          : "/api/v1/admin/authz/grant-revocations/{request_id}/security-review"
      const { data, error, response } = await apiClient.POST(path, {
        params: {
          header: { "X-CSRF-Token": input.csrfToken },
          path: { request_id: input.requestId },
        },
        body: { decision: input.decision, reason: input.reason },
      })
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: governanceKeys.all })
    },
  })
}

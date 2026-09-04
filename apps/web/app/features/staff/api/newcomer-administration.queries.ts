import {
  queryOptions,
  useMutation,
  useQueryClient,
} from "@tanstack/react-query"

import type { components } from "~/generated/api"
import { apiClient } from "~/shared/api/client"
import { ApiProblemError } from "~/shared/api/problem"

export type NewcomerPolicyInput = components["schemas"]["NewcomerPolicyInput"]
export type NewcomerPolicyPage = components["schemas"]["NewcomerPolicyPage"]
export type NewcomerPolicyRevision =
  components["schemas"]["NewcomerPolicyRevision"]
export type NewcomerAssessment = components["schemas"]["NewcomerAssessment"]
export type NewcomerAssessmentFilter =
  components["schemas"]["NewcomerAssessmentFilter"]
export type NewcomerAssessmentPage =
  components["schemas"]["NewcomerAssessmentPage"]

export const newcomerAdministrationKeys = {
  all: ["staff", "newcomer"] as const,
  policies: () => [...newcomerAdministrationKeys.all, "policies"] as const,
  assessments: (filter: NewcomerAssessmentFilter, query: string) =>
    [...newcomerAdministrationKeys.all, "assessments", filter, query] as const,
}

export function newcomerPolicyListQueryOptions() {
  return queryOptions({
    queryKey: newcomerAdministrationKeys.policies(),
    queryFn: async (): Promise<NewcomerPolicyPage> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/admin/newcomer/policies",
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

export function newcomerAssessmentListQueryOptions(
  filter: NewcomerAssessmentFilter = "active",
  query = ""
) {
  return queryOptions({
    queryKey: newcomerAdministrationKeys.assessments(filter, query),
    queryFn: async (): Promise<NewcomerAssessmentPage> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/admin/newcomer/assessments",
        { params: { query: { filter, q: query, limit: 50, offset: 0 } } }
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

export function useIssueNewcomerPolicy() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      csrfToken: string
      idempotencyKey: string
      policy: NewcomerPolicyInput
      effectiveAt: string
      reason: string
    }): Promise<NewcomerPolicyRevision> => {
      const { data, error, response } = await apiClient.POST(
        "/api/v1/admin/newcomer/policies",
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
        queryKey: newcomerAdministrationKeys.all,
      })
    },
  })
}

export function useExemptNewcomerAssessment() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      assessmentId: string
      expectedVersion: number
      reason: string
      csrfToken: string
      idempotencyKey: string
    }): Promise<NewcomerAssessment> => {
      const { data, error, response } = await apiClient.POST(
        "/api/v1/admin/newcomer/assessments/{assessment_id}/exemption",
        {
          params: {
            path: { assessment_id: input.assessmentId },
            header: {
              "X-CSRF-Token": input.csrfToken,
              "Idempotency-Key": input.idempotencyKey,
            },
          },
          body: {
            expected_version: input.expectedVersion,
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
        queryKey: newcomerAdministrationKeys.all,
      })
    },
  })
}

export function useAssignNewcomerAssessment() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      userId: string
      reason: string
      csrfToken: string
      idempotencyKey: string
    }): Promise<NewcomerAssessment> => {
      const { data, error, response } = await apiClient.POST(
        "/api/v1/admin/newcomer/assessments",
        {
          params: {
            header: {
              "X-CSRF-Token": input.csrfToken,
              "Idempotency-Key": input.idempotencyKey,
            },
          },
          body: { user_id: input.userId, reason: input.reason || undefined },
        }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({
        queryKey: newcomerAdministrationKeys.all,
      })
    },
  })
}

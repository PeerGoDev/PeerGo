import {
  queryOptions,
  useMutation,
  useQueryClient,
} from "@tanstack/react-query"

import type { components } from "~/generated/api"
import { apiClient } from "~/shared/api/client"
import { ApiProblemError } from "~/shared/api/problem"

export type RatioWatchPolicyInput =
  components["schemas"]["RatioWatchPolicyInput"]
export type RatioWatchPolicyRevision =
  components["schemas"]["RatioWatchPolicyRevision"]
export type RatioWatchPolicyPage = components["schemas"]["RatioWatchPolicyPage"]
export type RatioWatchPolicyImpactPreview =
  components["schemas"]["RatioWatchPolicyImpactPreview"]
export type RatioWatchAssessment = components["schemas"]["RatioWatchAssessment"]
export type RatioWatchAssessmentFilter =
  components["schemas"]["RatioWatchAssessmentFilter"]
export type RatioWatchAssessmentPage =
  components["schemas"]["RatioWatchAssessmentPage"]
export type RatioWatchAppeal = components["schemas"]["RatioWatchAppeal"]
export type RatioWatchAppealFilter =
  components["schemas"]["RatioWatchAppealFilter"]
export type RatioWatchAppealPage = components["schemas"]["RatioWatchAppealPage"]

export const ratioWatchAdministrationKeys = {
  all: ["staff", "ratio-watch"] as const,
  policies: () => [...ratioWatchAdministrationKeys.all, "policies"] as const,
  assessments: (filter: RatioWatchAssessmentFilter) =>
    [...ratioWatchAdministrationKeys.all, "assessments", filter] as const,
  appeals: (filter: RatioWatchAppealFilter) =>
    [...ratioWatchAdministrationKeys.all, "appeals", filter] as const,
}

export function ratioWatchAppealListQueryOptions(
  filter: RatioWatchAppealFilter = "pending"
) {
  return queryOptions({
    queryKey: ratioWatchAdministrationKeys.appeals(filter),
    queryFn: async (): Promise<RatioWatchAppealPage> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/admin/ratio-watch/appeals",
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

export function ratioWatchPolicyListQueryOptions() {
  return queryOptions({
    queryKey: ratioWatchAdministrationKeys.policies(),
    queryFn: async (): Promise<RatioWatchPolicyPage> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/admin/settings/ratio-watch",
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

export function ratioWatchAssessmentListQueryOptions(
  filter: RatioWatchAssessmentFilter = "active"
) {
  return queryOptions({
    queryKey: ratioWatchAdministrationKeys.assessments(filter),
    queryFn: async (): Promise<RatioWatchAssessmentPage> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/admin/ratio-watch/assessments",
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

export function usePreviewRatioWatchPolicy() {
  return useMutation({
    mutationFn: async (
      body: RatioWatchPolicyInput
    ): Promise<RatioWatchPolicyImpactPreview> => {
      const { data, error, response } = await apiClient.POST(
        "/api/v1/admin/settings/ratio-watch/preview",
        { body }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
  })
}

export function useIssueRatioWatchPolicy() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      csrfToken: string
      idempotencyKey: string
      policy: RatioWatchPolicyInput
      effectiveAt: string
      reason: string
    }): Promise<RatioWatchPolicyRevision> => {
      const { data, error, response } = await apiClient.POST(
        "/api/v1/admin/settings/ratio-watch",
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
        queryKey: ratioWatchAdministrationKeys.all,
      })
    },
  })
}

export function useClearRatioWatchAssessment() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      assessmentId: string
      expectedVersion: number
      reason: string
      csrfToken: string
    }): Promise<RatioWatchAssessment> => {
      const { data, error, response } = await apiClient.POST(
        "/api/v1/admin/ratio-watch/assessments/{assessment_id}/clear",
        {
          params: {
            path: { assessment_id: input.assessmentId },
            header: { "X-CSRF-Token": input.csrfToken },
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
        queryKey: ratioWatchAdministrationKeys.all,
      })
    },
  })
}

export function useDecideRatioWatchAppeal() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      appealId: string
      decision: "approved" | "rejected"
      expectedAssessmentVersion: number
      response: string
      csrfToken: string
    }): Promise<RatioWatchAppeal> => {
      const { data, error, response } = await apiClient.POST(
        "/api/v1/admin/ratio-watch/appeals/{appeal_id}/decision",
        {
          params: {
            path: { appeal_id: input.appealId },
            header: { "X-CSRF-Token": input.csrfToken },
          },
          body: {
            decision: input.decision,
            expected_assessment_version: input.expectedAssessmentVersion,
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
        queryKey: ratioWatchAdministrationKeys.all,
      })
    },
  })
}

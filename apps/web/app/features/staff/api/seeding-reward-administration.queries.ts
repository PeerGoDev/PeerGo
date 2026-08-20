import {
  queryOptions,
  useMutation,
  useQueryClient,
} from "@tanstack/react-query"

import type { components } from "~/generated/api"
import { apiClient } from "~/shared/api/client"
import { ApiProblemError } from "~/shared/api/problem"

export type SeedingRewardPolicy = components["schemas"]["SeedingRewardPolicy"]
export type SeedingRewardPolicyInput =
  components["schemas"]["SeedingRewardPolicyInput"]
export type SeedingRewardPolicyPage =
  components["schemas"]["SeedingRewardPolicyPage"]
export type SeedingRewardPolicyPreview =
  components["schemas"]["SeedingRewardPolicyPreview"]
export type LevelPolicyList = components["schemas"]["LevelPolicyList"]
export type LevelPolicy = components["schemas"]["LevelPolicy"]
export type IssueLevelPolicyRequest =
  components["schemas"]["IssueLevelPolicyRequest"]
export type ContributionExperiencePolicy =
  components["schemas"]["ContributionExperiencePolicy"]
export type ContributionExperiencePolicyPage =
  components["schemas"]["ContributionExperiencePolicyPage"]
export type IssueContributionExperiencePolicyRequest =
  components["schemas"]["IssueContributionExperiencePolicyRequest"]

export const seedingRewardAdministrationKeys = {
  all: ["staff", "seeding-rewards"] as const,
  list: () => [...seedingRewardAdministrationKeys.all, "list"] as const,
  levels: () => [...seedingRewardAdministrationKeys.all, "levels"] as const,
  contributions: () =>
    [...seedingRewardAdministrationKeys.all, "contributions"] as const,
}

export function contributionExperiencePolicyListQueryOptions() {
  return queryOptions({
    queryKey: seedingRewardAdministrationKeys.contributions(),
    queryFn: async (): Promise<ContributionExperiencePolicyPage> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/admin/settings/progression/contributions",
        { params: { query: { limit: 30, offset: 0 } } }
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

export function useIssueContributionExperiencePolicy() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      csrfToken: string
      body: IssueContributionExperiencePolicyRequest
    }): Promise<ContributionExperiencePolicy> => {
      const { data, error, response } = await apiClient.POST(
        "/api/v1/admin/settings/progression/contributions",
        {
          params: { header: { "X-CSRF-Token": input.csrfToken } },
          body: input.body,
        }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({
        queryKey: seedingRewardAdministrationKeys.contributions(),
      })
    },
  })
}

export function levelPolicyListQueryOptions() {
  return queryOptions({
    queryKey: seedingRewardAdministrationKeys.levels(),
    queryFn: async (): Promise<LevelPolicyList> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/admin/settings/progression/levels"
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

export function useIssueLevelPolicy() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      csrfToken: string
      idempotencyKey: string
      body: IssueLevelPolicyRequest
    }): Promise<LevelPolicy> => {
      const { data, error, response } = await apiClient.POST(
        "/api/v1/admin/settings/progression/levels",
        {
          params: {
            header: {
              "X-CSRF-Token": input.csrfToken,
              "Idempotency-Key": input.idempotencyKey,
            },
          },
          body: input.body,
        }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({
        queryKey: seedingRewardAdministrationKeys.levels(),
      })
    },
  })
}

export function seedingRewardPolicyListQueryOptions() {
  return queryOptions({
    queryKey: seedingRewardAdministrationKeys.list(),
    queryFn: async (): Promise<SeedingRewardPolicyPage> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/admin/settings/seeding-rewards",
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

export function usePreviewSeedingRewardPolicy() {
  return useMutation({
    mutationFn: async (
      body: SeedingRewardPolicyInput
    ): Promise<SeedingRewardPolicyPreview> => {
      const { data, error, response } = await apiClient.POST(
        "/api/v1/admin/settings/seeding-rewards/preview",
        { body }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
  })
}

export function useIssueSeedingRewardPolicy() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      csrfToken: string
      policy: SeedingRewardPolicyInput
      reason: string
    }): Promise<SeedingRewardPolicy> => {
      const { data, error, response } = await apiClient.POST(
        "/api/v1/admin/settings/seeding-rewards",
        {
          params: { header: { "X-CSRF-Token": input.csrfToken } },
          body: { policy: input.policy, reason: input.reason },
        }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({
        queryKey: seedingRewardAdministrationKeys.all,
      })
    },
  })
}

import {
  queryOptions,
  useMutation,
  useQueryClient,
} from "@tanstack/react-query"

import { socialPostKeys } from "~/features/social/api/posts.queries"
import { socialAdministrationKeys } from "~/features/staff/api/social-administration.queries"
import type { components } from "~/generated/api"
import { apiClient } from "~/shared/api/client"
import { ApiProblemError } from "~/shared/api/problem"

export type SocialPostModerationCase =
  components["schemas"]["SocialPostModerationCase"]
export type SocialPostModerationCasePage =
  components["schemas"]["SocialPostModerationCasePage"]
export type CreateSocialPostModerationDecisionRequest =
  components["schemas"]["CreateSocialPostModerationDecisionRequest"]
export type SocialPostModerationDecisionResult =
  components["schemas"]["SocialPostModerationDecisionResult"]

export const postModerationKeys = {
  all: [...socialAdministrationKeys.all, "post-moderation"] as const,
  list: (limit: number, offset: number) =>
    [...postModerationKeys.all, "list", { limit, offset }] as const,
}

export function postModerationCasesQueryOptions(limit: number, offset: number) {
  return queryOptions({
    queryKey: postModerationKeys.list(limit, offset),
    queryFn: async (): Promise<SocialPostModerationCasePage> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/admin/social/post-moderation-cases",
        { params: { query: { limit, offset } } }
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

export function useDecidePostModerationCase() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      caseId: string
      csrfToken: string
      idempotencyKey: string
      body: CreateSocialPostModerationDecisionRequest
    }): Promise<SocialPostModerationDecisionResult> => {
      const { data, error, response } = await apiClient.POST(
        "/api/v1/admin/social/post-moderation-cases/{case_id}/decisions",
        {
          params: {
            path: { case_id: input.caseId },
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
      await Promise.all([
        queryClient.invalidateQueries({
          queryKey: socialAdministrationKeys.all,
        }),
        queryClient.invalidateQueries({ queryKey: socialPostKeys.all }),
      ])
    },
    onError: async () => {
      await queryClient.invalidateQueries({ queryKey: postModerationKeys.all })
    },
  })
}

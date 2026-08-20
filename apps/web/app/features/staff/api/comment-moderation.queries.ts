import {
  queryOptions,
  useMutation,
  useQueryClient,
} from "@tanstack/react-query"

import {
  commentKeys,
  type CommentTarget,
} from "~/features/social/api/comments.queries"
import type { components } from "~/generated/api"
import { apiClient } from "~/shared/api/client"
import { ApiProblemError } from "~/shared/api/problem"

export type CommentModerationCase =
  components["schemas"]["CommentModerationCase"]
export type CommentModerationCasePage =
  components["schemas"]["CommentModerationCasePage"]
export type CommentModerationDecision =
  components["schemas"]["CommentModerationDecision"]
export type CommentModerationDecisionReasonCode =
  components["schemas"]["CommentModerationDecisionReasonCode"]
export type CreateCommentModerationDecisionRequest =
  components["schemas"]["CreateCommentModerationDecisionRequest"]
export type CommentModerationDecisionResult =
  components["schemas"]["CommentModerationDecisionResult"]

export const commentModerationKeys = {
  all: ["staff", "social", "comment-moderation"] as const,
  list: (limit: number, offset: number) =>
    [...commentModerationKeys.all, "list", { limit, offset }] as const,
}

export function commentModerationCasesQueryOptions(
  limit: number,
  offset: number
) {
  return queryOptions({
    queryKey: commentModerationKeys.list(limit, offset),
    queryFn: async (): Promise<CommentModerationCasePage> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/admin/social/comment-moderation-cases",
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

export function useDecideCommentModerationCase() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      caseId: string
      target: CommentTarget
      csrfToken: string
      idempotencyKey: string
      body: CreateCommentModerationDecisionRequest
    }): Promise<CommentModerationDecisionResult> => {
      const { data, error, response } = await apiClient.POST(
        "/api/v1/admin/social/comment-moderation-cases/{case_id}/decisions",
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
    onSuccess: async (_, input) => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: commentModerationKeys.all }),
        queryClient.invalidateQueries({
          queryKey: commentKeys.all(input.target),
        }),
      ])
    },
    onError: async () => {
      await queryClient.invalidateQueries({
        queryKey: commentModerationKeys.all,
      })
    },
  })
}

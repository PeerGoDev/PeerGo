import {
  queryOptions,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query"

import type { components } from "~/generated/api"
import { apiClient } from "~/shared/api/client"
import { ApiProblemError } from "~/shared/api/problem"

export type Comment = components["schemas"]["Comment"]
export type CommentSort = components["schemas"]["SocialCommentSort"]
export type CommentPage = Pick<
  components["schemas"]["TorrentCommentPage"],
  "items" | "total" | "limit" | "offset"
> & { thread_total?: number }
export type CommentReportReasonCode =
  components["schemas"]["CommentReportReasonCode"]
export type CommentReportReceipt = components["schemas"]["CommentReportReceipt"]

export type CommentTarget =
  | { kind: "torrent"; id: number }
  | { kind: "announcement"; id: string }
  | { kind: "post"; id: string }

export function torrentCommentTarget(id: number): CommentTarget {
  return { kind: "torrent", id }
}

export function announcementCommentTarget(id: string): CommentTarget {
  return { kind: "announcement", id }
}

export function postCommentTarget(id: string): CommentTarget {
  return { kind: "post", id }
}

export const commentKeys = {
  all: (target: CommentTarget) =>
    ["social", "comments", target.kind, target.id] as const,
  page: (
    target: CommentTarget,
    limit: number,
    offset: number,
    sort?: CommentSort
  ) => [...commentKeys.all(target), { limit, offset, sort }] as const,
}

export function commentsQueryOptions(
  target: CommentTarget,
  limit: number,
  offset: number,
  sort?: CommentSort
) {
  return queryOptions({
    queryKey: commentKeys.page(target, limit, offset, sort),
    queryFn: async (): Promise<CommentPage> => {
      if (target.kind === "torrent") {
        const { data, error, response } = await apiClient.GET(
          "/api/v1/torrents/{torrent_id}/comments",
          {
            params: {
              path: { torrent_id: target.id },
              query: { limit, offset },
            },
          }
        )
        if (!response.ok || !data) {
          throw new ApiProblemError(response.status, error)
        }
        return data
      }

      if (target.kind === "post") {
        const { data, error, response } = await apiClient.GET(
          "/api/v1/social/posts/{post_id}/comments",
          {
            params: {
              path: { post_id: target.id },
              query: { limit, offset, sort: sort ?? "newest" },
            },
          }
        )
        if (!response.ok || !data) {
          throw new ApiProblemError(response.status, error)
        }
        return data
      }

      const { data, error, response } = await apiClient.GET(
        "/api/v1/announcements/{announcement_id}/comments",
        {
          params: {
            path: { announcement_id: target.id },
            query: { limit, offset },
          },
        }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    staleTime: 15_000,
    retry: false,
  })
}

export function useComments(
  target: CommentTarget,
  limit: number,
  offset: number,
  sort?: CommentSort
) {
  return useQuery(commentsQueryOptions(target, limit, offset, sort))
}

export function useCreateComment(target: CommentTarget) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      csrfToken: string
      idempotencyKey: string
      body: string
      parentCommentId?: string
    }): Promise<Comment> => {
      const request = {
        params: {
          path:
            target.kind === "torrent"
              ? { torrent_id: target.id }
              : target.kind === "post"
                ? { post_id: target.id }
                : { announcement_id: target.id },
          header: {
            "X-CSRF-Token": input.csrfToken,
            "Idempotency-Key": input.idempotencyKey,
          },
        },
        body: {
          body: input.body,
          ...(input.parentCommentId
            ? { parent_comment_id: input.parentCommentId }
            : {}),
        },
      }

      if (target.kind === "torrent") {
        const { data, error, response } = await apiClient.POST(
          "/api/v1/torrents/{torrent_id}/comments",
          {
            ...request,
            params: {
              ...request.params,
              path: { torrent_id: target.id },
            },
          }
        )
        if (!response.ok || !data) {
          throw new ApiProblemError(response.status, error)
        }
        return data
      }

      if (target.kind === "post") {
        const { data, error, response } = await apiClient.POST(
          "/api/v1/social/posts/{post_id}/comments",
          {
            ...request,
            params: {
              ...request.params,
              path: { post_id: target.id },
            },
          }
        )
        if (!response.ok || !data) {
          throw new ApiProblemError(response.status, error)
        }
        return data
      }

      const { data, error, response } = await apiClient.POST(
        "/api/v1/announcements/{announcement_id}/comments",
        {
          ...request,
          params: {
            ...request.params,
            path: { announcement_id: target.id },
          },
        }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: commentKeys.all(target) })
    },
  })
}

export function useUpdateComment(target: CommentTarget) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      commentId: string
      csrfToken: string
      expectedVersion: number
      body: string
    }): Promise<Comment> => {
      const { data, error, response } = await apiClient.PATCH(
        "/api/v1/comments/{comment_id}",
        {
          params: {
            path: { comment_id: input.commentId },
            header: { "X-CSRF-Token": input.csrfToken },
          },
          body: {
            body: input.body,
            expected_version: input.expectedVersion,
          },
        }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: commentKeys.all(target) })
    },
  })
}

export function useDeleteComment(target: CommentTarget) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      commentId: string
      csrfToken: string
      expectedVersion: number
    }): Promise<void> => {
      const { error, response } = await apiClient.DELETE(
        "/api/v1/comments/{comment_id}",
        {
          params: {
            path: { comment_id: input.commentId },
            query: { expected_version: input.expectedVersion },
            header: { "X-CSRF-Token": input.csrfToken },
          },
        }
      )
      if (!response.ok) {
        throw new ApiProblemError(response.status, error)
      }
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: commentKeys.all(target) })
    },
  })
}

export function useCreateCommentReport() {
  return useMutation({
    mutationFn: async (input: {
      commentId: string
      csrfToken: string
      idempotencyKey: string
      reasonCode: CommentReportReasonCode
      details: string
    }): Promise<CommentReportReceipt> => {
      const details = input.details.trim()
      const { data, error, response } = await apiClient.POST(
        "/api/v1/comments/{comment_id}/reports",
        {
          params: {
            path: { comment_id: input.commentId },
            header: {
              "X-CSRF-Token": input.csrfToken,
              "Idempotency-Key": input.idempotencyKey,
            },
          },
          body: {
            reason_code: input.reasonCode,
            ...(details ? { details } : {}),
          },
        }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
  })
}

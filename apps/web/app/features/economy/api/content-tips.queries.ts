import {
  queryOptions,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query"

import { economyKeys } from "~/features/economy/api/economy.queries"
import type { components } from "~/generated/api"
import { apiClient } from "~/shared/api/client"
import { ApiProblemError } from "~/shared/api/problem"

export type ContentTipOverview = components["schemas"]["ContentTipOverview"]
export type ContentTipRecord = components["schemas"]["ContentTipRecord"]

export type ContentTipTarget =
  | { kind: "torrent"; torrentId: number; title: string }
  | { kind: "post"; postId: string; title: string }
  | { kind: "comment"; commentId: string; title: string }

export const contentTipKeys = {
  all: ["content-tips"] as const,
  current: (userId: string) => [...contentTipKeys.all, userId] as const,
}

export function contentTipOverviewQueryOptions(userId: string) {
  return queryOptions({
    queryKey: contentTipKeys.current(userId),
    queryFn: async (): Promise<ContentTipOverview> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/me/content-tips",
        { params: { query: { limit: 30 } } }
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

export function useContentTipOverview(
  userId: string | undefined,
  enabled = true
) {
  return useQuery({
    ...contentTipOverviewQueryOptions(userId ?? "anonymous"),
    enabled: Boolean(userId) && enabled,
  })
}

export function useCreateContentTip(userId: string | undefined) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      target: ContentTipTarget
      amount: string
      csrfToken: string
      idempotencyKey: string
    }): Promise<ContentTipRecord> => {
      const request = {
        params: {
          header: {
            "X-CSRF-Token": input.csrfToken,
            "Idempotency-Key": input.idempotencyKey,
          },
        },
        body: { amount: input.amount },
      } as const

      if (input.target.kind === "torrent") {
        const { data, error, response } = await apiClient.POST(
          "/api/v1/torrents/{torrent_id}/tips",
          {
            ...request,
            params: {
              ...request.params,
              path: { torrent_id: input.target.torrentId },
            },
          }
        )
        if (!response.ok || !data)
          throw new ApiProblemError(response.status, error)
        return data
      }

      if (input.target.kind === "post") {
        const { data, error, response } = await apiClient.POST(
          "/api/v1/social/posts/{post_id}/tips",
          {
            ...request,
            params: {
              ...request.params,
              path: { post_id: input.target.postId },
            },
          }
        )
        if (!response.ok || !data)
          throw new ApiProblemError(response.status, error)
        return data
      }

      const { data, error, response } = await apiClient.POST(
        "/api/v1/comments/{comment_id}/tips",
        {
          ...request,
          params: {
            ...request.params,
            path: { comment_id: input.target.commentId },
          },
        }
      )
      if (!response.ok || !data)
        throw new ApiProblemError(response.status, error)
      return data
    },
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: contentTipKeys.all }),
        queryClient.invalidateQueries({ queryKey: economyKeys.all }),
      ])
    },
    meta: { userId },
  })
}

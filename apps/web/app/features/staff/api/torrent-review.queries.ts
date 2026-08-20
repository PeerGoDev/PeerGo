import {
  queryOptions,
  useMutation,
  useQueryClient,
} from "@tanstack/react-query"

import type { components } from "~/generated/api"
import { apiClient } from "~/shared/api/client"
import { ApiProblemError } from "~/shared/api/problem"

export type PendingTorrentReview = components["schemas"]["PendingTorrentReview"]
export type PendingTorrentReviewPage =
  components["schemas"]["PendingTorrentReviewPage"]
export type TorrentReviewDecisionRequest =
  components["schemas"]["TorrentReviewDecisionRequest"]
export type TorrentReviewDecisionResult =
  components["schemas"]["TorrentReviewDecisionResult"]

export const torrentReviewKeys = {
  all: ["staff", "torrent-reviews"] as const,
  pending: (limit: number) =>
    [...torrentReviewKeys.all, "pending", { limit }] as const,
}

export function pendingTorrentReviewsQueryOptions(limit: number) {
  return queryOptions({
    queryKey: torrentReviewKeys.pending(limit),
    queryFn: async (): Promise<PendingTorrentReviewPage> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/admin/torrent-reviews",
        { params: { query: { limit } } }
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

export function useDecideTorrentReview() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      torrentId: number
      csrfToken: string
      idempotencyKey: string
      body: TorrentReviewDecisionRequest
    }): Promise<TorrentReviewDecisionResult> => {
      const { data, error, response } = await apiClient.POST(
        "/api/v1/admin/torrents/{torrent_id}/review-decisions",
        {
          params: {
            path: { torrent_id: input.torrentId },
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
      await queryClient.invalidateQueries({ queryKey: torrentReviewKeys.all })
    },
    onError: async () => {
      // A conflicting decision changes the queue even when this request loses
      // the race, so always reload before the reviewer retries.
      await queryClient.invalidateQueries({ queryKey: torrentReviewKeys.all })
    },
  })
}

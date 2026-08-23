import {
  queryOptions,
  useMutation,
  useQueryClient,
} from "@tanstack/react-query"

import type { components } from "~/generated/api"
import { apiClient } from "~/shared/api/client"
import { ApiProblemError } from "~/shared/api/problem"

export type MyTorrentReviewAssignment =
  components["schemas"]["MyTorrentReviewAssignment"]
export type MyTorrentReviewAssignmentPage =
  components["schemas"]["MyTorrentReviewAssignmentPage"]
export type MyTorrentReviewDetail =
  components["schemas"]["MyTorrentReviewDetail"]
export type ReviewedTorrentReview =
  components["schemas"]["ReviewedTorrentReview"]
export type ReviewedTorrentReviewPage =
  components["schemas"]["ReviewedTorrentReviewPage"]
export type TorrentReviewFilePage = components["schemas"]["TorrentFilePage"]
export type TorrentReviewDecisionRequest =
  components["schemas"]["TorrentReviewDecisionRequest"]
export type TorrentReviewVoteResult =
  components["schemas"]["TorrentReviewVoteResult"]

export const torrentReviewVotingKeys = {
  all: ["torrent-review-voting"] as const,
  assignments: (limit: number) =>
    [...torrentReviewVotingKeys.all, "assignments", { limit }] as const,
  detail: (torrentId: number) =>
    [...torrentReviewVotingKeys.all, "detail", torrentId] as const,
  files: (torrentId: number, limit: number, offset: number) =>
    [
      ...torrentReviewVotingKeys.detail(torrentId),
      "files",
      { limit, offset },
    ] as const,
  history: (limit: number) =>
    [...torrentReviewVotingKeys.all, "history", { limit }] as const,
}

export function myTorrentReviewAssignmentsQueryOptions(limit: number) {
  return queryOptions({
    queryKey: torrentReviewVotingKeys.assignments(limit),
    queryFn: async (): Promise<MyTorrentReviewAssignmentPage> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/me/torrent-reviews",
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

export function myTorrentReviewDetailQueryOptions(torrentId: number) {
  return queryOptions({
    queryKey: torrentReviewVotingKeys.detail(torrentId),
    queryFn: async (): Promise<MyTorrentReviewDetail> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/me/torrent-reviews/{torrent_id}",
        { params: { path: { torrent_id: torrentId } } }
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

export function myTorrentReviewFilesQueryOptions(
  torrentId: number,
  limit: number,
  offset: number
) {
  return queryOptions({
    queryKey: torrentReviewVotingKeys.files(torrentId, limit, offset),
    queryFn: async (): Promise<TorrentReviewFilePage> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/me/torrent-reviews/{torrent_id}/files",
        {
          params: {
            path: { torrent_id: torrentId },
            query: { limit, offset },
          },
        }
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

export function myReviewedTorrentReviewsQueryOptions(limit: number) {
  return queryOptions({
    queryKey: torrentReviewVotingKeys.history(limit),
    queryFn: async (): Promise<ReviewedTorrentReviewPage> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/me/torrent-reviews/history",
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

export function useCreateTorrentReviewVote() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      torrentId: number
      csrfToken: string
      idempotencyKey: string
      body: TorrentReviewDecisionRequest
    }): Promise<TorrentReviewVoteResult> => {
      const { data, error, response } = await apiClient.POST(
        "/api/v1/me/torrent-reviews/{torrent_id}/votes",
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
    onSettled: async () => {
      // The decisive vote may remove the item for every reviewer; a conflict
      // can do the same, so both success and failure refresh the private queue.
      await queryClient.invalidateQueries({
        queryKey: torrentReviewVotingKeys.all,
      })
    },
  })
}

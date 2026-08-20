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
export type TorrentReviewDecisionRequest =
  components["schemas"]["TorrentReviewDecisionRequest"]
export type TorrentReviewVoteResult =
  components["schemas"]["TorrentReviewVoteResult"]

export const torrentReviewVotingKeys = {
  all: ["torrent-review-voting"] as const,
  assignments: (limit: number) =>
    [...torrentReviewVotingKeys.all, "assignments", { limit }] as const,
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

import { useMutation, useQueryClient } from "@tanstack/react-query"

import { torrentKeys } from "~/features/torrent/api/torrent.queries"
import type { components } from "~/generated/api"
import { apiClient } from "~/shared/api/client"
import { ApiProblemError } from "~/shared/api/problem"

export type UpdatePublishedTorrentMetadataRequest =
  components["schemas"]["UpdatePublishedTorrentMetadataRequest"]
export type PublishedTorrentMetadataRevision =
  components["schemas"]["PublishedTorrentMetadataRevision"]

export function useUpdatePublishedTorrentMetadata() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      torrentId: number
      csrfToken: string
      idempotencyKey: string
      body: UpdatePublishedTorrentMetadataRequest
    }): Promise<PublishedTorrentMetadataRevision> => {
      const { data, error, response } = await apiClient.PUT(
        "/api/v1/me/torrent-submissions/{torrent_id}/metadata",
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
      // Detail, catalog and the uploader's submission list all share this
      // canonical key root; one invalidation prevents partially stale titles.
      await queryClient.invalidateQueries({ queryKey: torrentKeys.all })
    },
  })
}

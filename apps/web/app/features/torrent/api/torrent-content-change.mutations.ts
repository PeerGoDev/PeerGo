import { useMutation, useQueryClient } from "@tanstack/react-query"

import { torrentKeys } from "~/features/torrent/api/torrent.queries"
import type { components } from "~/generated/api"
import { apiClient } from "~/shared/api/client"
import { ApiProblemError } from "~/shared/api/problem"

export type SubmitPublishedTorrentContentChangeRequest =
  components["schemas"]["SubmitPublishedTorrentContentChangeRequest"]
export type PublishedTorrentContentChange =
  components["schemas"]["PublishedTorrentContentChange"]

export function useSubmitPublishedTorrentContentChange(userId: string) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      torrentId: number
      csrfToken: string
      idempotencyKey: string
      body: SubmitPublishedTorrentContentChangeRequest
    }): Promise<PublishedTorrentContentChange> => {
      const { data, error, response } = await apiClient.POST(
        "/api/v1/me/torrent-submissions/{torrent_id}/content-change",
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
      await Promise.all([
        queryClient.invalidateQueries({
          queryKey: torrentKeys.mySubmissions(userId, 20),
        }),
        queryClient.invalidateQueries({
          queryKey: ["staff", "torrents", "administration", "content-changes"],
        }),
      ])
    },
  })
}

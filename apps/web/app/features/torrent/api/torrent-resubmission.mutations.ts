import { useMutation, useQueryClient } from "@tanstack/react-query"

import { torrentKeys } from "~/features/torrent/api/torrent.queries"
import type { components } from "~/generated/api"
import { apiClient } from "~/shared/api/client"
import { ApiProblemError } from "~/shared/api/problem"

export type TorrentResubmissionRequest =
  components["schemas"]["TorrentResubmissionRequest"]
export type TorrentResubmission = components["schemas"]["TorrentResubmission"]
export type TorrentResubmissionInput = {
  torrentId: number
  csrfToken: string
  idempotencyKey: string
  body: TorrentResubmissionRequest
}

export async function resubmitTorrentSubmission(
  input: TorrentResubmissionInput
): Promise<TorrentResubmission> {
  const { data, error, response } = await apiClient.PUT(
    "/api/v1/me/torrent-submissions/{torrent_id}/resubmit",
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
}

export function useResubmitTorrentSubmission(userId: string) {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: resubmitTorrentSubmission,
    onSettled: async () => {
      await queryClient.invalidateQueries({
        queryKey: torrentKeys.mySubmissions(userId, 20),
        exact: true,
      })
    },
  })
}

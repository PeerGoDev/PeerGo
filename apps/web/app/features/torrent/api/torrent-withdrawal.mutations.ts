import { useMutation, useQueryClient } from "@tanstack/react-query"

import type { components } from "~/generated/api"
import { torrentKeys } from "~/features/torrent/api/torrent.queries"
import { apiClient } from "~/shared/api/client"
import { ApiProblemError } from "~/shared/api/problem"

export type SubmitTorrentWithdrawalRequest =
  components["schemas"]["SubmitTorrentWithdrawalRequest"]
export type TorrentWithdrawalRequest =
  components["schemas"]["TorrentWithdrawalRequest"]

export function useSubmitTorrentWithdrawal(userId: string) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      torrentId: number
      csrfToken: string
      idempotencyKey: string
      body: SubmitTorrentWithdrawalRequest
    }): Promise<TorrentWithdrawalRequest> => {
      const { data, error, response } = await apiClient.POST(
        "/api/v1/me/torrent-submissions/{torrent_id}/withdrawal",
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
          queryKey: ["staff", "torrents", "administration"],
        }),
        queryClient.invalidateQueries({ queryKey: torrentKeys.all }),
      ])
    },
  })
}

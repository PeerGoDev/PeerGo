import { useMutation } from "@tanstack/react-query"

import type { components } from "~/generated/api"
import { apiClient } from "~/shared/api/client"
import { ApiProblemError } from "~/shared/api/problem"

export type TorrentReportReasonCode =
  components["schemas"]["TorrentReportReasonCode"]
export type CreateTorrentReportRequest =
  components["schemas"]["CreateTorrentReportRequest"]
export type TorrentReportReceipt = components["schemas"]["TorrentReportReceipt"]

export function useCreateTorrentReport() {
  return useMutation({
    mutationFn: async (input: {
      torrentId: number
      csrfToken: string
      idempotencyKey: string
      body: CreateTorrentReportRequest
    }): Promise<TorrentReportReceipt> => {
      const { data, error, response } = await apiClient.POST(
        "/api/v1/torrents/{torrent_id}/reports",
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
  })
}

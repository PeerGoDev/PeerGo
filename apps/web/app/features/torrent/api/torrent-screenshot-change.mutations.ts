import { useMutation, useQueryClient } from "@tanstack/react-query"

import type { components } from "~/generated/api"
import { torrentKeys } from "~/features/torrent/api/torrent.queries"
import { resolveApiUrl } from "~/shared/api/client"
import { ApiProblemError } from "~/shared/api/problem"

export type PublishedTorrentScreenshotChange =
  components["schemas"]["PublishedTorrentScreenshotChange"]
export type ScreenshotManifestItem =
  components["schemas"]["ScreenshotManifestItem"]

type SubmitInput = {
  torrentId: number
  expectedVersion: number
  manifest: ScreenshotManifestItem[]
  uploads: File[]
  reason: string
  csrfToken: string
  idempotencyKey: string
}

export function useSubmitPublishedTorrentScreenshotChange(userId: string) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: submitPublishedTorrentScreenshotChange,
    onSuccess: async (_, input) => {
      await Promise.all([
        queryClient.invalidateQueries({
          queryKey: torrentKeys.mySubmissions(userId, 20),
        }),
        queryClient.invalidateQueries({
          queryKey: torrentKeys.detail(input.torrentId),
        }),
        queryClient.invalidateQueries({
          queryKey: [
            "staff",
            "torrents",
            "administration",
            "screenshot-changes",
          ],
        }),
      ])
    },
  })
}

async function submitPublishedTorrentScreenshotChange(
  input: SubmitInput
): Promise<PublishedTorrentScreenshotChange> {
  const body = new FormData()
  body.set("expected_version", String(input.expectedVersion))
  body.set("reason", input.reason)
  input.manifest.forEach((item, index) => {
    body.set(`manifest[${index}][source]`, item.source)
    body.set(`manifest[${index}][index]`, String(item.index))
  })
  input.uploads.forEach((file) => body.append("uploads", file, file.name))

  const response = await fetch(
    resolveApiUrl(
      `/api/v1/me/torrent-submissions/${input.torrentId}/screenshot-change`
    ),
    {
      method: "POST",
      credentials: "include",
      headers: {
        Accept: "application/json, application/problem+json",
        "X-CSRF-Token": input.csrfToken,
        "Idempotency-Key": input.idempotencyKey,
      },
      body,
    }
  )
  const payload: unknown = await response.json().catch(() => undefined)
  if (!response.ok) throw new ApiProblemError(response.status, payload)
  return payload as PublishedTorrentScreenshotChange
}

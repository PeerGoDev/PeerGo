import { useMutation, useQueryClient } from "@tanstack/react-query"

import { Button } from "~/components/ui/button"
import { useCapabilities } from "~/features/authz/api/capabilities.queries"
import { hasCapability } from "~/features/staff/model/capability"
import { torrentKeys } from "~/features/torrent/api/torrent.queries"
import { apiClient } from "~/shared/api/client"
import { ApiProblemError, requestErrorDescription } from "~/shared/api/problem"

export function TorrentReseedButton({
  torrentId,
  userId,
  csrfToken,
}: {
  torrentId: number
  userId: string
  csrfToken: string
}) {
  const capabilities = useCapabilities(userId)
  const queryClient = useQueryClient()
  const request = useMutation({
    mutationFn: async () => {
      const { error, response } = await apiClient.POST(
        "/api/v1/torrents/{torrent_id}/reseed-requests",
        {
          params: {
            path: { torrent_id: torrentId },
            header: { "X-CSRF-Token": csrfToken },
          },
        }
      )
      if (!response.ok) throw new ApiProblemError(response.status, error)
    },
    onSuccess: () =>
      queryClient.invalidateQueries({ queryKey: torrentKeys.reseedRequests() }),
  })

  if (!hasCapability(capabilities.data, "torrent.reseed.request.self")) {
    return null
  }

  if (request.isSuccess) {
    return (
      <p className="text-xs text-success-foreground" role="status">
        已请求补种，恢复做种后会收到站内消息。
      </p>
    )
  }

  return (
    <>
      <Button
        type="button"
        size="sm"
        variant="outline"
        onClick={() => request.mutate()}
        disabled={request.isPending}
      >
        {request.isPending ? "正在请求…" : "请求补种"}
      </Button>
      {request.isError ? (
        <p className="text-xs text-destructive" role="alert">
          {requestErrorDescription(request.error, "请求补种失败，请稍后重试。")}
        </p>
      ) : null}
    </>
  )
}

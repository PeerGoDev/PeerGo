import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { afterEach, describe, expect, it, vi } from "vitest"

import { capabilityKeys } from "~/features/authz/api/capabilities.queries"
import { TorrentReseedButton } from "~/features/torrent/components/torrent-reseed-button"

describe("TorrentReseedButton", () => {
  afterEach(() => vi.unstubAllGlobals())

  it("subscribes to a reseed request using the member session CSRF token", async () => {
    const userId = "0198f20a-6da8-7e51-9c64-111111111111"
    const csrfToken = "c".repeat(43)
    const queryClient = new QueryClient()
    queryClient.setQueryData(capabilityKeys.current(userId), {
      policy_version: "test",
      items: [{ action: "torrent.reseed.request.self" }],
    })
    const fetchRequest = vi.fn(async (_request: Request) => {
      return new Response(null, { status: 204 })
    })
    vi.stubGlobal("fetch", fetchRequest)

    render(
      <QueryClientProvider client={queryClient}>
        <TorrentReseedButton
          torrentId={42}
          userId={userId}
          csrfToken={csrfToken}
        />
      </QueryClientProvider>
    )

    await userEvent.click(screen.getByRole("button", { name: "请求补种" }))

    expect(await screen.findByRole("status")).toHaveTextContent(
      "已请求补种，恢复做种后会收到站内消息。"
    )
    expect(fetchRequest).toHaveBeenCalledTimes(1)
    const [request] = fetchRequest.mock.calls[0]
    expect(new URL(request.url).pathname).toBe(
      "/api/v1/torrents/42/reseed-requests"
    )
    expect(request.method).toBe("POST")
    expect(request.headers.get("X-CSRF-Token")).toBe(csrfToken)
  })
})

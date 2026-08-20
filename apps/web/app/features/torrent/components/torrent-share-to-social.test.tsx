import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { fireEvent, render, screen } from "@testing-library/react"
import { MemoryRouter } from "react-router"
import { describe, expect, it } from "vitest"

import { sessionKeys } from "~/features/auth/api/session.mutations"
import { capabilityKeys } from "~/features/authz/api/capabilities.queries"
import { torrentKeys } from "~/features/torrent/api/torrent.queries"
import { TorrentShareToSocial } from "~/features/torrent/components/torrent-share-to-social"

const torrentId = 42
const userId = "0198f20a-6da8-7e51-9c64-111111111111"

describe("TorrentShareToSocial", () => {
  it("keeps the PtYes media slot when a torrent has no migrated image", () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false, staleTime: Infinity } },
    })
    queryClient.setQueryData(sessionKeys.current(), {
      user: {
        id: userId,
        username: "demo",
        display_name: "演示用户",
        email_verified: true,
      },
      expires_at: "2026-08-14T00:00:00Z",
      csrf_token: "c".repeat(43),
    })
    queryClient.setQueryData(capabilityKeys.current(userId), {
      policy_version: "2026-08-13",
      items: [{ action: "social.post.create.self" }],
    })
    queryClient.setQueryData(torrentKeys.swarm(torrentId), {
      torrent_id: torrentId,
      seeders: 0,
      leechers: 0,
      completed: 0,
      observed_at: "2026-08-13T00:00:00Z",
      stale: false,
      confidence: "fresh",
    })

    render(
      <MemoryRouter>
        <QueryClientProvider client={queryClient}>
          <TorrentShareToSocial
            torrentId={torrentId}
            title="Release Without Cover"
            subtitle="没有迁移图片"
            sizeBytes={1024}
            screenshotCount={0}
          />
        </QueryClientProvider>
      </MemoryRouter>
    )

    fireEvent.click(screen.getByRole("button", { name: "分享到动态圈" }))

    expect(screen.getByRole("dialog", { name: "分享到动态圈" })).toHaveClass(
      "border",
      "sm:max-w-md"
    )
    expect(document.querySelector("#torrent-share-form")).toHaveClass("min-w-0")
    const fallback = screen.getByRole("img", {
      name: "Release Without Cover暂无封面",
    })
    expect(fallback.parentElement).toHaveClass("h-20", "w-16", "shrink-0")
    expect(screen.getByText("0 做种")).toBeVisible()
    expect(screen.getByText("0 下载")).toBeVisible()
    expect(screen.getByLabelText("说点什么（可选）")).toHaveClass(
      "h-[78px]",
      "min-h-[78px]"
    )
    expect(screen.getByText("0/500")).toHaveClass(
      "mt-1!",
      "text-xs",
      "leading-4"
    )
  })
})

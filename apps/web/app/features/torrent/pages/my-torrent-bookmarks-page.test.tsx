import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import { MemoryRouter } from "react-router"
import { describe, expect, it } from "vitest"

import { TooltipProvider } from "~/components/ui/tooltip"
import { sessionKeys } from "~/features/auth/api/session.mutations"
import { capabilityKeys } from "~/features/authz/api/capabilities.queries"
import {
  torrentBookmarkKeys,
  type TorrentBookmarkPage,
} from "~/features/torrent/api/torrent-bookmarks.queries"
import { MyTorrentBookmarksPage } from "~/features/torrent/pages/my-torrent-bookmarks-page"

const userId = "0198f20a-6da8-7e51-9c64-111111111111"
const torrentId = 42

describe("MyTorrentBookmarksPage", () => {
  it("renders private saved torrents with bookmark time and active controls", () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    queryClient.setQueryData(sessionKeys.current(), {
      user: {
        id: userId,
        username: "demo",
        display_name: "PeerGo 演示用户",
        email_verified: true,
      },
      expires_at: "2026-08-10T00:00:00Z",
      csrf_token: "c".repeat(43),
    })
    queryClient.setQueryData(capabilityKeys.current(userId), {
      policy_version: "2026-08-09",
      items: [
        {
          action: "torrent.bookmark.read.self",
          description: "查看自己的收藏",
          scope: { type: "site", id: "peergo" },
          expires_at: "2026-08-10T00:00:00Z",
        },
        {
          action: "torrent.bookmark.write.self",
          description: "修改自己的收藏",
          scope: { type: "site", id: "peergo" },
          expires_at: "2026-08-10T00:00:00Z",
        },
      ],
    })
    const page: TorrentBookmarkPage = {
      total: 1,
      limit: 20,
      offset: 0,
      items: [
        {
          bookmarked_at: "2026-08-09T12:00:00Z",
          torrent: {
            id: torrentId,
            name: "Saved Release",
            subtitle: "A user-facing bookmark fixture",
            category: { id: "movies", name: "电影" },
            size_bytes: 4096,
            seeders: 8,
            leechers: 2,
            completed: 18,
            promotion: "free",
            sticky_until: null,
            uploaded_at: "2026-08-09T10:00:00Z",
            swarm_observed_at: "2026-08-09T11:59:00Z",
            swarm_stale: false,
          },
        },
      ],
    }
    queryClient.setQueryData(torrentBookmarkKeys.list(userId, 20, 0), page)
    queryClient.setQueryData(
      torrentBookmarkKeys.statuses(userId, [torrentId]),
      {
        bookmarked_ids: [torrentId],
      }
    )

    render(
      <MemoryRouter>
        <QueryClientProvider client={queryClient}>
          <TooltipProvider>
            <MyTorrentBookmarksPage />
          </TooltipProvider>
        </QueryClientProvider>
      </MemoryRouter>
    )

    expect(screen.getByRole("heading", { name: /我的收藏/ })).toBeVisible()
    const table = screen.getByRole("table")
    expect(table).toHaveClass("block", "min-w-0")
    expect(table).not.toHaveClass("min-w-[900px]")
    expect(table.querySelector("thead")).toHaveClass("block", "bg-muted")
    expect(screen.getAllByText("Saved Release")[0].closest("tr")).toHaveClass(
      "border-t!",
      "border-b-0!"
    )
    expect(screen.getByText("上传时间")).toBeInTheDocument()
    expect(screen.getAllByText("Saved Release").length).toBeGreaterThan(0)
    expect(screen.getAllByText("免费").length).toBeGreaterThan(0)
    for (const button of screen.getAllByRole("button", {
      name: "取消收藏“Saved Release”",
    })) {
      expect(button).toHaveAttribute("aria-pressed", "true")
    }
  })
})

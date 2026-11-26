import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter } from "react-router"
import { afterEach, describe, expect, it, vi } from "vitest"

import {
  sessionKeys,
  type WebSession,
} from "~/features/auth/api/session.mutations"
import { TorrentReseedPage } from "~/features/torrent/pages/torrent-reseed-page"

const session: WebSession = {
  user: {
    id: "0198f20a-6da8-7e51-9c64-111111111111",
    username: "helper",
    display_name: "补种会员",
    email_verified: true,
  },
  expires_at: "2026-11-27T00:00:00Z",
  csrf_token: "c".repeat(43),
}

describe("TorrentReseedPage", () => {
  afterEach(() => {
    vi.unstubAllGlobals()
    localStorage.removeItem("peergo-hide-nsfw")
  })

  it("shows demand and estimated rewards, pages the list, and shares the confirmed NSFW filter", async () => {
    const user = userEvent.setup()
    const queryClient = new QueryClient()
    queryClient.setQueryData(sessionKeys.current(), session)
    const requestedPages: Array<[string | null, string | null]> = []
    vi.stubGlobal("fetch", async (request: Request) => {
      const url = new URL(request.url)
      if (url.pathname !== "/api/v1/torrents/reseed-requests")
        return Response.json(session)
      const offset = url.searchParams.get("offset")
      const excludeNsfw = url.searchParams.get("exclude_nsfw")
      requestedPages.push([offset, excludeNsfw])
      return Response.json({
        limit: 20,
        offset: Number(offset),
        total: 21,
        items:
          offset === "20"
            ? [
                {
                  torrent_id: 50,
                  torrent_title:
                    excludeNsfw === "true"
                      ? "另一项补种请求"
                      : "成人分类补种请求",
                  requester_count: 1,
                },
              ]
            : [
                {
                  torrent_id: 42,
                  torrent_title: "需要补种的资源",
                  requester_count: 5,
                  reseed_reward: {
                    status: "upcoming",
                    required_hours: 24,
                    magic_amount: 1000,
                  },
                },
              ],
      })
    })

    render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={["/torrents/reseed"]}>
          <TorrentReseedPage />
        </MemoryRouter>
      </QueryClientProvider>
    )

    expect(screen.getByRole("heading", { name: "补种专区" })).toBeVisible()
    expect(
      await screen.findByRole("link", { name: "需要补种的资源" })
    ).toHaveAttribute("href", "/torrents/42")
    expect(screen.getByRole("table")).toHaveTextContent("5 人")
    expect(screen.getByRole("table")).toHaveTextContent("24 小时")
    expect(screen.getByRole("table")).toHaveTextContent("1,000 魔力值")

    await user.click(screen.getByRole("button", { name: "下一页" }))
    expect(
      await screen.findByRole("link", { name: "另一项补种请求" })
    ).toHaveAttribute("href", "/torrents/50")
    expect(screen.getByRole("table")).toHaveTextContent("暂无奖励")

    await user.click(screen.getByRole("switch", { name: "屏蔽 NSFW" }))
    await user.click(
      screen.getByRole("button", { name: "我已年满 18 周岁，确认显示" })
    )
    expect(
      await screen.findByRole("link", { name: "成人分类补种请求" })
    ).toBeVisible()
    expect(requestedPages).toEqual([
      ["0", "true"],
      ["20", "true"],
      ["20", "false"],
    ])
  })
})

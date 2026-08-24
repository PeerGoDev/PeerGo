import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter } from "react-router"
import { afterEach, describe, expect, it, vi } from "vitest"

import { sessionKeys } from "~/features/auth/api/session.mutations"
import { capabilityKeys } from "~/features/authz/api/capabilities.queries"
import { torrentKeys } from "~/features/torrent/api/torrent.queries"
import { TorrentShareToSocial } from "~/features/torrent/components/torrent-share-to-social"

const torrentId = 42
const userId = "0198f20a-6da8-7e51-9c64-111111111111"

describe("TorrentShareToSocial", () => {
  afterEach(() => vi.unstubAllGlobals())

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

  it("publishes a real torrent reference without synthetic title or link text", async () => {
    const user = userEvent.setup()
    const queryClient = preparedQueryClient()
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(
        JSON.stringify({
          id: "0198f20a-6da8-7e51-9c64-222222222222",
          author: {
            id: userId,
            username: "demo",
            display_name: "演示用户",
            followed_by_me: false,
            online: true,
            vip: false,
            administrator: false,
            medals: [],
          },
          board: {
            id: "resources",
            name: "资源交流",
            description: "",
            icon: "folder-open",
            tone: "green",
            display_order: 20,
            enabled: true,
            allow_member_posts: true,
            post_count: 1,
            version: 1,
          },
          content: "值得收藏",
          version: 1,
          comment_count: 0,
          like_count: 0,
          repost_count: 0,
          liked_by_me: false,
          reposted_by_me: false,
          pinned: false,
          featured: false,
          topics: [],
          media: [],
          torrent: {
            id: torrentId,
            available: true,
            title: "Release Without Cover",
            subtitle: "没有迁移图片",
            size_bytes: 1024,
            cover_available: false,
          },
          created_at: "2026-08-24T08:00:00Z",
          updated_at: "2026-08-24T08:00:00Z",
        }),
        { status: 201, headers: { "Content-Type": "application/json" } }
      )
    )
    vi.stubGlobal("fetch", fetchMock)

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

    await user.click(screen.getByRole("button", { name: "分享到动态圈" }))
    await user.type(screen.getByLabelText("说点什么（可选）"), " 值得收藏 ")
    await user.click(screen.getByRole("button", { name: "分享" }))

    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1))
    const request = fetchMock.mock.calls[0]?.[0] as Request
    await expect(request.clone().json()).resolves.toMatchObject({
      content: "值得收藏",
      board_id: "resources",
      torrent_id: torrentId,
    })
    const rawBody = await request.text()
    expect(rawBody).not.toContain("分享种子：")
    expect(rawBody).not.toContain(`/torrents/${torrentId}`)
  })
})

function preparedQueryClient() {
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
  return queryClient
}

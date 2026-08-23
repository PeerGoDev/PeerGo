import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter } from "react-router"
import { describe, expect, it } from "vitest"

import { SocialPostCard } from "~/features/social/components/social-post-card"

describe("SocialPostCard", () => {
  it("collapses long Unicode content at the PtYes-compatible boundary", async () => {
    const user = userEvent.setup()
    const content = `${"中".repeat(299)}😀结尾`

    render(
      <MemoryRouter>
        <QueryClientProvider client={new QueryClient()}>
          <SocialPostCard
            post={{
              id: "0198f20a-6da8-7e51-9c64-222222222222",
              author: {
                id: "0198f20a-6da8-7e51-9c64-111111111111",
                username: "demo",
                display_name: "演示用户",
                followed_by_me: false,
              },
              board: {
                id: "general",
                name: "生活茶馆",
                description: "",
                icon: "coffee",
                tone: "coral",
                display_order: 10,
                enabled: true,
                allow_member_posts: true,
                post_count: 1,
                version: 1,
              },
              content,
              version: 1,
              comment_count: 0,
              like_count: 0,
              repost_count: 0,
              liked_by_me: false,
              reposted_by_me: false,
              pinned: false,
              featured: false,
              hidden: false,
              topics: [],
              media: [],
              created_at: "2026-08-13T06:00:00Z",
              updated_at: "2026-08-13T06:00:00Z",
            }}
          />
        </QueryClientProvider>
      </MemoryRouter>
    )

    const paragraph = screen
      .getByRole("button", { name: "展开全文" })
      .closest("p")
    expect(paragraph).toHaveTextContent(`${"中".repeat(299)}😀...展开全文`)
    expect(paragraph).not.toHaveTextContent("结尾")

    await user.click(screen.getByRole("button", { name: "展开全文" }))

    expect(paragraph).toHaveTextContent(`${"中".repeat(299)}😀结尾收起`)
    expect(screen.getByRole("button", { name: "收起" })).toBeVisible()
  })

  it("links a shared torrent path back to its PeerGo detail page", () => {
    const torrentId = 42

    render(
      <MemoryRouter>
        <QueryClientProvider client={new QueryClient()}>
          <SocialPostCard
            post={{
              id: "0198f20a-6da8-7e51-9c64-222222222222",
              author: {
                id: "0198f20a-6da8-7e51-9c64-111111111111",
                username: "demo",
                display_name: "演示用户",
                followed_by_me: false,
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
              content: `分享种子：演示资源\n\n/torrents/${torrentId}`,
              version: 1,
              comment_count: 0,
              like_count: 0,
              repost_count: 0,
              liked_by_me: false,
              reposted_by_me: false,
              pinned: false,
              featured: false,
              hidden: false,
              topics: [],
              media: [],
              created_at: "2026-08-13T06:00:00Z",
              updated_at: "2026-08-13T06:00:00Z",
            }}
          />
        </QueryClientProvider>
      </MemoryRouter>
    )

    expect(
      screen.getByRole("link", { name: `/torrents/${torrentId}` })
    ).toHaveAttribute("href", `/torrents/${torrentId}`)
    expect(screen.getByRole("button", { name: "点赞" })).toHaveClass("border-0")
    expect(screen.getByRole("button", { name: "评论" })).toHaveClass("border-0")
  })
})

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter } from "react-router"
import { describe, expect, it } from "vitest"

import { SocialPostCard } from "~/features/social/components/social-post-card"
import { staffSessionKeys } from "~/features/staff/api/staff-session.mutations"

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
                online: true,
                vip: true,
                administrator: true,
                medals: [
                  {
                    id: 7,
                    name: "首批成员",
                    image_path: "/uploads/medals/founding.webp",
                  },
                ],
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

    const metadata = screen
      .getByText("生活茶馆")
      .closest('[data-slot="social-post-metadata"]')
    expect(metadata).toContainElement(
      screen.getByRole("link", { name: "演示用户" })
    )
    expect(screen.getByRole("link", { name: "演示用户" })).toHaveClass(
      "bg-gradient-to-r"
    )
    expect(screen.getByText("管理员")).toBeVisible()
    expect(screen.getByText("VIP")).toBeVisible()
    expect(screen.getByLabelText("勋章：首批成员")).toBeVisible()
    expect(screen.getByLabelText("演示用户在线")).toBeVisible()
    expect(metadata?.querySelector("[title]")).toBeInTheDocument()
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
                online: false,
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

  it("lets the red packet sender claim one share like PtYes", () => {
    const authorId = "0198f20a-6da8-7e51-9c64-111111111111"
    const queryClient = new QueryClient()
    queryClient.setQueryData(staffSessionKeys.current(), null)

    render(
      <MemoryRouter>
        <QueryClientProvider client={queryClient}>
          <SocialPostCard
            currentUserId={authorId}
            csrfToken="csrf-token"
            post={{
              id: "0198f20a-6da8-7e51-9c64-222222222222",
              author: {
                id: authorId,
                username: "sender",
                display_name: "红包发送者",
                followed_by_me: false,
                online: true,
                vip: false,
                administrator: false,
                medals: [],
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
              content: "大家来领红包",
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
              red_packet: {
                total_amount: 100,
                claim_count: 5,
                remaining_amount: 100,
                remaining_claims: 5,
                claimed_by_me: false,
              },
              created_at: "2026-08-23T06:00:00Z",
              updated_at: "2026-08-23T06:00:00Z",
            }}
          />
        </QueryClientProvider>
      </MemoryRouter>
    )

    expect(screen.getByRole("button", { name: "领取" })).toBeEnabled()
  })

  it("shows quick pin and delete actions to a social moderator", async () => {
    const user = userEvent.setup()
    const administratorId = "0198f20a-6da8-7e51-9c64-333333333333"
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false, staleTime: Infinity } },
    })
    const administrator = {
      id: administratorId,
      username: "admin",
      display_name: "管理员",
      email_verified: true,
    }
    queryClient.setQueryData(staffSessionKeys.current(), {
      user: administrator,
      expires_at: "2026-08-24T00:00:00Z",
      authentication_method: "account_session",
      authenticated_at: "2026-08-23T06:00:00Z",
      csrf_token: "s".repeat(43),
    })
    queryClient.setQueryData(staffSessionKeys.capabilities(administratorId), {
      policy_version: "policy-2026-08-23",
      items: [
        {
          action: "social.post.moderate",
          description: "管理动态",
          scope: { type: "site", id: "peergo" },
          expires_at: "2026-08-24T00:00:00Z",
        },
      ],
    })

    render(
      <MemoryRouter>
        <QueryClientProvider client={queryClient}>
          <SocialPostCard
            currentUserId={administratorId}
            csrfToken={"w".repeat(43)}
            post={{
              id: "0198f20a-6da8-7e51-9c64-222222222222",
              author: {
                id: "0198f20a-6da8-7e51-9c64-111111111111",
                username: "member",
                display_name: "普通成员",
                followed_by_me: false,
                online: false,
                vip: false,
                administrator: false,
                medals: [],
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
              content: "需要管理员处理的动态",
              version: 3,
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
              created_at: "2026-08-23T06:00:00Z",
              updated_at: "2026-08-23T06:00:00Z",
            }}
          />
        </QueryClientProvider>
      </MemoryRouter>
    )

    await user.click(screen.getByRole("button", { name: "动态操作" }))

    expect(
      await screen.findByRole("menuitem", { name: "置顶动态" })
    ).toBeVisible()
    const deleteAction = screen.getByRole("menuitem", { name: "删除动态" })
    expect(deleteAction).toBeVisible()

    await user.click(deleteAction)

    expect(screen.getByRole("alertdialog")).toHaveTextContent(
      "删除后动态将立即从前台隐藏"
    )
  })
})

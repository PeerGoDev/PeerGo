import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter } from "react-router"
import { describe, expect, it } from "vitest"

import { sessionKeys } from "~/features/auth/api/session.mutations"
import { capabilityKeys } from "~/features/authz/api/capabilities.queries"
import { socialPostKeys } from "~/features/social/api/posts.queries"
import { SocialFeedPage } from "~/features/social/pages/social-feed-page"

const userId = "0198f20a-6da8-7e51-9c64-111111111111"

describe("SocialFeedPage", () => {
  it("renders the PtYes-compatible feed frame and lets administrators select restricted boards", async () => {
    const user = userEvent.setup()
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
      expires_at: "2026-08-14T08:00:00Z",
      csrf_token: "a".repeat(43),
    })
    queryClient.setQueryData(capabilityKeys.current(userId), {
      policy_version: "test",
      items: [
        {
          action: "social.post.create.restricted.self",
          description: "发布管理团队动态",
          scope: { type: "site", id: "site" },
          expires_at: "2026-08-14T08:00:00Z",
        },
        {
          action: "social.post.create.self",
          description: "发布动态",
          scope: { type: "site", id: "site" },
          expires_at: "2026-08-14T08:00:00Z",
        },
      ],
    })
    queryClient.setQueryData(socialPostKeys.overview(), {
      boards: [
        {
          id: "general",
          name: "生活茶馆",
          description: "日常交流",
          icon: "coffee",
          tone: "coral",
          display_order: 10,
          enabled: true,
          allow_member_posts: true,
          post_count: 1,
          version: 1,
        },
        {
          id: "staff",
          name: "站务公告",
          description: "社区规则与站务通知",
          icon: "megaphone",
          tone: "violet",
          display_order: 40,
          enabled: true,
          allow_member_posts: false,
          post_count: 0,
          version: 1,
        },
      ],
      hot_topics: [],
    })
    queryClient.setQueryData(socialPostKeys.page("newest", 20, 0), {
      items: [
        {
          id: "0198f20a-6da8-7e51-9c64-222222222222",
          author: {
            id: userId,
            username: "demo",
            display_name: "演示用户",
          },
          content: "欢迎来到 #PeerGo 动态圈",
          version: 1,
          comment_count: 2,
          created_at: "2026-08-13T06:00:00Z",
          updated_at: "2026-08-13T06:00:00Z",
        },
      ],
      total: 1,
      limit: 20,
      offset: 0,
      sort: "newest",
    })

    render(
      <MemoryRouter initialEntries={["/social"]}>
        <QueryClientProvider client={queryClient}>
          <SocialFeedPage />
        </QueryClientProvider>
      </MemoryRouter>
    )

    expect(screen.getByRole("heading", { name: "动态圈" })).toBeVisible()
    await user.click(screen.getByRole("combobox", { name: "发布板块" }))
    expect(
      await screen.findByRole("option", { name: "站务公告（管理团队）" })
    ).toBeVisible()
    expect(screen.getByLabelText("动态正文")).toHaveAttribute(
      "placeholder",
      "分享你的想法..."
    )
    expect(screen.getByLabelText("动态正文")).toHaveClass("rounded-none")
    expect(
      screen.getByRole("button", { name: "添加图片 (0/9)" })
    ).not.toHaveAttribute("aria-disabled")
    expect(
      screen.getByRole("button", { name: "添加图片 (0/9)" })
    ).not.toBeDisabled()
    expect(screen.getByText("0/2000")).toHaveClass("hidden", "sm:inline")
    expect(screen.getByText("公开")).toHaveClass("hidden", "sm:flex")
    expect(screen.getByRole("button", { name: "发布" })).toHaveClass("px-3")
    expect(screen.getByRole("tablist", { name: "动态流筛选" })).toBeVisible()
    expect(screen.getByRole("tab", { name: "关注" })).not.toHaveAttribute(
      "aria-disabled"
    )
    expect(screen.getByRole("tab", { name: "关注" })).not.toBeDisabled()
    expect(screen.getByRole("tab", { name: "发现" })).toHaveAttribute(
      "aria-selected",
      "true"
    )
    expect(screen.getByRole("tab", { name: "热门" })).not.toHaveAttribute(
      "aria-disabled"
    )
    expect(screen.getByRole("tab", { name: "热门" })).not.toBeDisabled()
    expect(screen.getByRole("tabpanel", { name: "发现" })).toBeVisible()
    expect(screen.getByText(/欢迎来到.*动态圈/)).toBeVisible()
    expect(screen.getByText("#PeerGo")).toBeVisible()
    expect(screen.getByRole("button", { name: "点赞" })).not.toHaveAttribute(
      "aria-disabled"
    )
    expect(screen.getByRole("button", { name: "点赞" })).not.toBeDisabled()
    expect(screen.getByText("2").closest("a")).toHaveAttribute(
      "href",
      "/social/post/0198f20a-6da8-7e51-9c64-222222222222"
    )
    expect(screen.getByText("暂无热门话题")).toBeVisible()
  })
})

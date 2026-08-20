import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen, within } from "@testing-library/react"
import { MemoryRouter, Route, Routes } from "react-router"
import { describe, expect, it } from "vitest"

import { sessionKeys } from "~/features/auth/api/session.mutations"
import { capabilityKeys } from "~/features/authz/api/capabilities.queries"
import { AnnouncementDetailPage } from "~/features/announcement/pages/announcement-detail-page"
import {
  announcementCommentTarget,
  commentKeys,
  type CommentPage,
} from "~/features/social/api/comments.queries"
import { siteKeys } from "~/features/site/api/site.queries"

const announcementId = "welcome-to-peergo"
const userId = "0198f20a-6da8-7e51-9c64-111111111111"

describe("AnnouncementDetailPage", () => {
  it("uses the compact reference-style card for an unavailable announcement", () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })

    render(
      <MemoryRouter initialEntries={["/announcements/invalid!"]}>
        <QueryClientProvider client={queryClient}>
          <Routes>
            <Route
              path="/announcements/:announcementId"
              element={<AnnouncementDetailPage />}
            />
          </Routes>
        </QueryClientProvider>
      </MemoryRouter>
    )

    const heading = screen.getByRole("heading", { name: "公告不存在" })
    expect(heading.closest('[data-slot="card"]')).toHaveClass(
      "w-full",
      "max-w-md",
      "py-0"
    )
    expect(screen.getByText("公告不存在或已被删除")).toBeVisible()
    expect(
      screen.getByRole("button", { name: "返回公告列表" })
    ).toHaveAttribute("href", "/announcements")
  })

  it("renders the published body and reuses the authorized comment thread", () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    queryClient.setQueryData(siteKeys.announcement(announcementId), {
      id: announcementId,
      title: "欢迎来到 PeerGo",
      summary: "首版公开公告摘要。",
      body: "这里是公告正文。\n\n评论区用于讨论公告内容。",
      body_format: "plain_text",
      version: 1,
      published_at: "2026-08-10T08:00:00Z",
      updated_at: "2026-08-10T08:00:00Z",
    })
    queryClient.setQueryData(siteKeys.info(), {
      name: "PeerGo",
      description: "测试站点",
      registration_mode: "open",
      registration_username_min_characters: 3,
      registration_username_max_characters: 20,
      registration_email_domain_mode: "any",
      human_verification: disabledHumanVerification,
      online_users: 8,
      default_torrent_view: "list",
      show_latest_announcement: true,
    })
    queryClient.setQueryData(sessionKeys.current(), {
      user: {
        id: userId,
        username: "north-shore",
        display_name: "北岸",
        email_verified: true,
      },
      expires_at: "2026-08-11T08:00:00Z",
      csrf_token: "a".repeat(43),
    })
    queryClient.setQueryData(capabilityKeys.current(userId), {
      policy_version: "test",
      items: [
        {
          action: "announcement.comment.create.self",
          description: "发表评论",
          scope: { type: "self", id: userId },
          expires_at: "2026-08-11T08:00:00Z",
        },
      ],
    })
    queryClient.setQueryData(
      commentKeys.page(announcementCommentTarget(announcementId), 20, 0),
      { items: [], total: 0, limit: 20, offset: 0 } satisfies CommentPage
    )

    render(
      <MemoryRouter initialEntries={[`/announcements/${announcementId}`]}>
        <QueryClientProvider client={queryClient}>
          <Routes>
            <Route
              path="/announcements/:announcementId"
              element={<AnnouncementDetailPage />}
            />
          </Routes>
        </QueryClientProvider>
      </MemoryRouter>
    )

    expect(
      screen.getByRole("heading", { level: 1, name: "欢迎来到 PeerGo" })
    ).toBeVisible()
    expect(screen.getByText(/这里是公告正文/)).toBeVisible()
    expect(screen.getByText(/由 PeerGo 站务 发布于/)).toBeVisible()
    expect(screen.getByLabelText("发表评论")).toHaveAttribute(
      "placeholder",
      "发表评论..."
    )
    expect(screen.getByLabelText("发表评论")).toHaveClass(
      "min-h-[100px]",
      "resize-none"
    )
    expect(screen.getByText("0/2000")).toBeVisible()
    expect(screen.getByText("暂无评论，来发表第一条评论吧")).toBeVisible()
    const article = screen.getByRole("article")
    expect(within(article).getByRole("heading", { name: "评论" })).toBeVisible()
  })

  it("uses the compact PtYes-style login prompt for anonymous readers", () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    queryClient.setQueryData(siteKeys.announcement(announcementId), {
      id: announcementId,
      title: "欢迎来到 PeerGo",
      summary: "首版公开公告摘要。",
      body: "这里是公告正文。",
      body_format: "plain_text",
      version: 1,
      published_at: "2026-08-10T08:00:00Z",
      updated_at: "2026-08-10T08:00:00Z",
    })
    queryClient.setQueryData(siteKeys.info(), {
      name: "PeerGo",
      description: "测试站点",
      registration_mode: "open",
      registration_username_min_characters: 3,
      registration_username_max_characters: 20,
      registration_email_domain_mode: "any",
      human_verification: disabledHumanVerification,
      online_users: 8,
      default_torrent_view: "list",
      show_latest_announcement: true,
    })
    queryClient.setQueryData(sessionKeys.current(), null)
    queryClient.setQueryData(
      commentKeys.page(announcementCommentTarget(announcementId), 20, 0),
      { items: [], total: 0, limit: 20, offset: 0 } satisfies CommentPage
    )

    render(
      <MemoryRouter initialEntries={[`/announcements/${announcementId}`]}>
        <QueryClientProvider client={queryClient}>
          <Routes>
            <Route
              path="/announcements/:announcementId"
              element={<AnnouncementDetailPage />}
            />
          </Routes>
        </QueryClientProvider>
      </MemoryRouter>
    )

    expect(screen.getByText("后才能发表评论")).toBeVisible()
    expect(screen.getByRole("link", { name: "登录" })).toHaveAttribute(
      "href",
      "/login"
    )
    expect(screen.queryByText("登录后参与评论与回复。")).not.toBeInTheDocument()
  })
})

const disabledHumanVerification = {
  provider: "disabled" as const,
  site_key: "",
  registration_enabled: false,
  login_enabled: false,
  password_recovery_enabled: false,
}

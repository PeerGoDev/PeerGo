import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter } from "react-router"
import { describe, expect, it } from "vitest"

import { sessionKeys } from "~/features/auth/api/session.mutations"
import { capabilityKeys } from "~/features/authz/api/capabilities.queries"
import { commentModerationKeys } from "~/features/staff/api/comment-moderation.queries"
import { staffSessionKeys } from "~/features/staff/api/staff-session.mutations"
import { StaffCommentModerationPage } from "~/features/staff/pages/staff-comment-moderation-page"

const userId = "0198f20a-6da8-7e51-9c64-777777777777"

describe("StaffCommentModerationPage", () => {
  it("matches the compact Rousi report layout without exposing reporter identity", async () => {
    const user = userEvent.setup()
    const queryClient = createQueryClient()

    render(
      <MemoryRouter initialEntries={["/staff/content/comments"]}>
        <QueryClientProvider client={queryClient}>
          <StaffCommentModerationPage />
        </QueryClientProvider>
      </MemoryRouter>
    )

    expect(
      await screen.findByRole("heading", { name: "评论审核" })
    ).toHaveClass("text-2xl", "font-bold")
    expect(screen.getByRole("button", { name: "刷新" })).toBeVisible()
    expect(screen.getByText("评论举报案件")).toBeVisible()
    expect(screen.getByRole("columnheader", { name: "目标" })).toBeVisible()
    expect(screen.getByRole("columnheader", { name: "举报原因" })).toBeVisible()
    expect(screen.getByRole("columnheader", { name: "评论内容" })).toBeVisible()
    expect(screen.getByRole("cell", { name: "待处理" })).toBeVisible()
    expect(screen.getByText("举报人匿名")).toBeVisible()
    expect(screen.getByText("同评论聚合")).toBeVisible()
    expect(screen.getByText("保存前复核")).toBeVisible()
    expect(screen.getAllByText("广告或刷屏")[0]).toBeVisible()
    expect(screen.getAllByText("种子评论测试内容")[0]).toBeVisible()
    expect(screen.getByText("共 3 份匿名举报")).toBeVisible()
    expect(screen.queryByText("reporter@example.com")).not.toBeInTheDocument()

    await user.click(
      screen.getByRole("button", {
        name: "处理评论举报 PeerGo 2026 测试资源",
      })
    )
    expect(screen.getByRole("dialog", { name: "处置评论举报" })).toBeVisible()
    expect(
      screen.getByText(/保存时会再次确认案件和评论是否已被其他管理员处理/)
    ).toBeVisible()
  })
})

function createQueryClient() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  })
  queryClient.setQueryData(sessionKeys.current(), {
    user: {
      id: userId,
      username: "admin",
      display_name: "管理员",
      email_verified: true,
    },
    expires_at: "2026-08-15T00:00:00Z",
    csrf_token: "c".repeat(43),
  })
  queryClient.setQueryData(capabilityKeys.current(userId), {
    policy_version: "2026-08-14",
    items: [],
  })
  queryClient.setQueryData(staffSessionKeys.current(), {
    user: {
      id: userId,
      username: "admin",
      display_name: "管理员",
      email_verified: true,
    },
    expires_at: "2026-08-15T00:00:00Z",
    webauthn_authenticated_at: "2026-08-14T08:00:00Z",
    csrf_token: "s".repeat(43),
  })
  queryClient.setQueryData(staffSessionKeys.capabilities(userId), {
    policy_version: "2026-08-14",
    items: [
      {
        action: "social.report.read",
        description: "读取评论举报",
        scope: { type: "site", id: "peergo" },
        expires_at: "2026-08-15T00:00:00Z",
      },
      {
        action: "social.report.resolve",
        description: "处置评论举报",
        scope: { type: "site", id: "peergo" },
        expires_at: "2026-08-15T00:00:00Z",
      },
    ],
  })
  queryClient.setQueryData(commentModerationKeys.list(20, 0), {
    items: [
      {
        id: "0198f20a-6da8-7e51-9c64-888888888888",
        state: "open",
        version: 2,
        target: {
          kind: "torrent",
          title: "PeerGo 2026 测试资源",
          torrent_id: 42,
        },
        comment: {
          id: "0198f20a-6da8-7e51-9c64-aaaaaaaaaaaa",
          author: {
            id: "0198f20a-6da8-7e51-9c64-bbbbbbbbbbbb",
            display_name: "评论用户",
          },
          body: "种子评论测试内容",
          body_format: "plain_text",
          state: "visible",
          version: 4,
          created_at: "2026-08-14T08:00:00Z",
          updated_at: "2026-08-14T08:00:00Z",
        },
        report_count: 3,
        reports: [
          {
            reason_code: "spam",
            details: "连续发布重复推广信息。",
            created_at: "2026-08-14T08:10:00Z",
          },
        ],
        opened_at: "2026-08-14T08:10:00Z",
        latest_reported_at: "2026-08-14T08:20:00Z",
      },
    ],
    total: 1,
    limit: 20,
    offset: 0,
  })
  return queryClient
}

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { fireEvent, render, screen } from "@testing-library/react"
import { MemoryRouter, Route, Routes } from "react-router"
import { describe, expect, it } from "vitest"

import { TooltipProvider } from "~/components/ui/tooltip"
import { sessionKeys } from "~/features/auth/api/session.mutations"
import { capabilityKeys } from "~/features/authz/api/capabilities.queries"
import {
  notificationKeys,
  type MyNotificationPage,
} from "~/features/notification/api/notifications.queries"
import { NotificationPage } from "~/features/notification/pages/notification-page"

const userId = "0198f20a-6da8-7e51-9c64-111111111111"
const approvedTorrentId = 42
const rejectedTorrentId = 43

describe("NotificationPage", () => {
  it("renders private review outcomes with clear user actions", async () => {
    const queryClient = notificationQueryClient()
    const page: MyNotificationPage = {
      total: 7,
      unread_count: 1,
      limit: 20,
      offset: 0,
      items: [
        {
          id: "0198f20a-6da8-7e51-9c64-444444444444",
          kind: "torrent_review",
          torrent_id: rejectedTorrentId,
          torrent_title: "Release Needs Details",
          outcome: "rejected",
          reason_code: "metadata_incomplete",
          reason: "请补充完整的发布说明后重新提交。",
          created_at: "2026-08-10T12:00:00Z",
          read_at: null,
        },
        {
          id: "0198f20a-6da8-7e51-9c64-555555555555",
          kind: "torrent_review",
          torrent_id: approvedTorrentId,
          torrent_title: "Approved Release",
          outcome: "published",
          reason_code: "meets_requirements",
          reason: "内容与元数据已经核对完成，可以正式发布。",
          created_at: "2026-08-10T11:00:00Z",
          read_at: "2026-08-10T11:30:00Z",
        },
        {
          id: "0198f20a-6da8-7e51-9c64-777777777777",
          kind: "ratio_watch",
          ratio_watch_status: "download_restricted",
          ratio_basis_points: 2500,
          minimum_ratio_basis_points: 4000,
          restriction_ratio_basis_points: 3000,
          deadline_at: "2026-08-10T10:00:00Z",
          created_at: "2026-08-10T10:00:00Z",
          read_at: "2026-08-10T10:30:00Z",
        },
        {
          id: "0198f20a-6da8-7e51-9c64-888888888888",
          kind: "hnr",
          torrent_id: 44,
          torrent_title: "H&R Needs Seeding",
          hnr_status: "download_restricted",
          hnr_grace_ends_at: "2026-08-10T09:00:00Z",
          created_at: "2026-08-10T09:00:00Z",
          read_at: "2026-08-10T09:30:00Z",
        },
        {
          id: "0198f20a-6da8-7e51-9c64-999999999999",
          kind: "hnr_appeal",
          torrent_id: 44,
          torrent_title: "H&R Needs Seeding",
          hnr_status: "appeal_approved",
          hnr_grace_ends_at: "2026-08-10T09:00:00Z",
          hnr_appeal_response: "已核对客户端异常记录，批准本条 H&R 义务豁免。",
          created_at: "2026-08-10T08:30:00Z",
          read_at: "2026-08-10T09:00:00Z",
        },
        {
          id: "0198f20a-6da8-7e51-9c64-aaaaaaaaaaaa",
          kind: "workgroup_contribution",
          workgroup_kind: "retention",
          workgroup_metric: "seeding_active_seconds",
          workgroup_policy_revision: 1,
          workgroup_period_starts_at: "2026-08-01T00:00:00Z",
          workgroup_period_ends_at: "2026-09-01T00:00:00Z",
          workgroup_observed_at: "2026-08-10T08:00:00Z",
          workgroup_evidence_state: "collecting",
          workgroup_current_value: 86400,
          workgroup_target_value: 604800,
          workgroup_assessment_state: "in_progress",
          workgroup_explanation_code: "period_in_progress",
          workgroup_reason: "请继续保持本月做种，并关注剩余贡献时长。",
          created_at: "2026-08-10T08:00:00Z",
          read_at: "2026-08-10T08:30:00Z",
        },
        {
          id: "0198f20a-6da8-7e51-9c64-bbbbbbbbbbbb",
          kind: "member_gift",
          member_gift_sender_numeric_id: "42",
          member_gift_sender_username: "member42",
          member_gift_sender_display_name: "四十二号成员",
          member_gift_net_amount: "9007199254740992",
          member_gift_message: "感谢你长期保种。",
          created_at: "2026-08-10T07:30:00Z",
          read_at: "2026-08-10T08:00:00Z",
        },
      ],
    }
    queryClient.setQueryData(notificationKeys.page(userId, 20, 0, false), page)
    queryClient.setQueryData(notificationKeys.page(userId, 20, 0, true), {
      ...page,
      total: 1,
      items: page.items.filter((notification) => notification.read_at === null),
    } satisfies MyNotificationPage)

    renderNotificationPage(queryClient, "/notifications")

    expect(
      screen.getByRole("heading", { level: 1, name: "站内消息" })
    ).toBeVisible()
    expect(screen.getByRole("main")).toHaveClass(
      "px-8!",
      "pt-12!",
      "gap-6",
      "lg:px-10!",
      "lg:pt-14!",
      "md:max-w-3xl"
    )
    expect(
      screen.getByRole("heading", { level: 2, name: "消息列表" })
    ).toBeVisible()
    const messageCards = screen
      .getByRole("region", { name: "站内消息" })
      .querySelectorAll("[data-slot='card']")
    expect(messageCards).toHaveLength(2)
    expect(messageCards[0]).toHaveClass(
      "gap-0",
      "py-0",
      "rounded-lg",
      "text-base",
      "shadow",
      "md:min-h-[640px]"
    )
    expect(messageCards[1]).toHaveClass(
      "md:col-span-2",
      "text-base",
      "shadow",
      "md:min-h-[640px]"
    )
    expect(screen.getByLabelText("未读")).toHaveClass("bg-primary")
    expect(screen.getByRole("button", { name: "联系管理员" })).toHaveClass(
      "border-0",
      "min-w-0",
      "whitespace-nowrap",
      "md:shrink-0"
    )
    expect(screen.getByRole("button", { name: "仅未读" })).toHaveClass(
      "border-0",
      "text-foreground"
    )
    expect(screen.getByText(/Release Needs Details/)).toBeVisible()
    expect(screen.getByText(/审核通过：Approved Release/)).toBeVisible()
    expect(screen.getByText("下载权限已受限")).toBeVisible()
    expect(screen.getByText(/H&R 待补做，下载受限/)).toBeVisible()
    expect(screen.getByText(/H&R 申诉已批准/)).toBeVisible()
    expect(screen.getByText("保种组贡献进度提醒")).toBeVisible()
    expect(
      screen.getByText("收到 四十二号成员 赠送的 9,007,199,254,740,992 魔力值")
    ).toBeVisible()
    expect(screen.getByText("请选择一条消息查看详情")).toBeVisible()

    fireEvent.click(
      screen.getByRole("button", { name: /Release Needs Details/ })
    )
    expect(screen.getByRole("link", { name: "查看反馈" })).toHaveAttribute(
      "href",
      "/account/submissions"
    )

    fireEvent.click(screen.getByRole("button", { name: /Approved Release/ }))
    expect(screen.getByRole("link", { name: "查看种子" })).toHaveAttribute(
      "href",
      `/torrents/${approvedTorrentId}`
    )

    fireEvent.click(screen.getByRole("button", { name: /下载权限已受限/ }))
    expect(
      screen.getByRole("link", { name: "查看分享率考核" })
    ).toHaveAttribute("href", "/account/ratio")
    expect(screen.getByText("0.25")).toBeVisible()
    expect(screen.getByText("0.40")).toBeVisible()

    fireEvent.click(
      screen.getByRole("button", { name: /H&R 待补做，下载受限/ })
    )
    expect(screen.getByRole("link", { name: "查看 H&R" })).toHaveAttribute(
      "href",
      "/account/hnr"
    )

    fireEvent.click(screen.getByRole("button", { name: /H&R 申诉已批准/ }))
    expect(
      screen.getByText("已核对客户端异常记录，批准本条 H&R 义务豁免。")
    ).toBeVisible()

    fireEvent.click(screen.getByRole("button", { name: /保种组贡献进度提醒/ }))
    expect(
      screen.getByText("请继续保持本月做种，并关注剩余贡献时长。")
    ).toBeVisible()
    expect(screen.getByText("1 天 / 7 天")).toBeVisible()
    expect(
      screen.getByRole("link", { name: "查看工作组贡献" })
    ).toHaveAttribute("href", "/workgroups")

    fireEvent.click(
      screen.getByRole("button", {
        name: /收到 四十二号成员 赠送的 9,007,199,254,740,992 魔力值/,
      })
    )
    expect(
      screen.getByRole("heading", { level: 2, name: "收到成员赠送" })
    ).toBeVisible()
    expect(screen.getByText("9,007,199,254,740,992")).toBeVisible()
    expect(screen.getByText("四十二号成员 #42 · @member42")).toBeVisible()
    expect(screen.getByText("感谢你长期保种。")).toBeVisible()
    expect(
      screen.getByRole("link", { name: "查看魔力值账本" })
    ).toHaveAttribute("href", "/account/economy")

    expect(
      screen.queryByRole("button", { name: "标为已读" })
    ).not.toBeInTheDocument()
    expect(screen.getByRole("button", { name: "全部标记为已读" })).toBeEnabled()

    fireEvent.click(screen.getByRole("button", { name: "联系管理员" }))
    expect(screen.getByRole("heading", { name: "联系管理员" })).toBeVisible()
    expect(
      screen.getByRole("heading", { name: "联系管理员" }).querySelector("svg")
    ).not.toBeNull()
    expect(document.querySelector("[data-slot='dialog-overlay']")).toHaveClass(
      "bg-black/80",
      "supports-backdrop-filter:backdrop-blur-none"
    )
    expect(screen.getByLabelText("标题")).toHaveAttribute("maxlength", "100")
    expect(screen.getByLabelText("内容")).toHaveAttribute("maxlength", "2000")
    expect(screen.getByLabelText("内容")).toHaveClass("min-h-[162px]")
    expect(screen.getByRole("button", { name: "发送" })).toBeDisabled()
    fireEvent.click(screen.getByRole("button", { name: "取消" }))

    fireEvent.click(screen.getByRole("button", { name: "删除全部" }))
    expect(
      screen.getByRole("heading", { name: "清空全部消息？" })
    ).toBeVisible()
    expect(screen.getByRole("button", { name: "确认清空" })).toBeEnabled()
    fireEvent.click(screen.getByRole("button", { name: "取消" }))

    fireEvent.click(screen.getByRole("button", { name: "仅未读" }))
    expect(screen.getByRole("button", { name: "显示全部" })).toBeVisible()
    expect(
      screen.queryByText(/审核通过：Approved Release/)
    ).not.toBeInTheDocument()
    expect(document.body.textContent).not.toMatch(
      /decision|capability|reviewer/i
    )
  })

  it("uses the stable page query from the URL", () => {
    const queryClient = notificationQueryClient()
    queryClient.setQueryData(notificationKeys.page(userId, 20, 20, false), {
      total: 21,
      unread_count: 0,
      limit: 20,
      offset: 20,
      items: [
        {
          id: "0198f20a-6da8-7e51-9c64-666666666666",
          kind: "torrent_review",
          torrent_id: approvedTorrentId,
          torrent_title: "Older Review Result",
          outcome: "published",
          reason_code: "meets_requirements",
          reason: "内容与元数据已经核对完成，可以正式发布。",
          created_at: "2026-07-10T11:00:00Z",
          read_at: "2026-07-10T12:00:00Z",
        },
      ],
    } satisfies MyNotificationPage)

    renderNotificationPage(queryClient, "/notifications?page=2")

    expect(screen.getByText(/Older Review Result/)).toBeVisible()
    expect(screen.getByText("第 2 / 2 页")).toBeVisible()
  })

  it("keeps the PtYes action rhythm when there are no unread messages", async () => {
    const queryClient = notificationQueryClient()
    queryClient.setQueryData(notificationKeys.page(userId, 20, 0, false), {
      total: 0,
      unread_count: 0,
      limit: 20,
      offset: 0,
      items: [],
    } satisfies MyNotificationPage)

    renderNotificationPage(queryClient, "/notifications")

    expect(
      screen.getByRole("button", { name: "全部标记为已读" })
    ).toBeDisabled()
    expect(screen.getByText("暂无消息")).toBeVisible()
    expect(screen.getByRole("button", { name: "仅未读" })).toBeVisible()
    expect(screen.getByRole("button", { name: "删除全部" })).toBeVisible()
  })
})

function notificationQueryClient() {
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
    expires_at: "2026-08-11T00:00:00Z",
    csrf_token: "c".repeat(43),
  })
  queryClient.setQueryData(capabilityKeys.current(userId), {
    policy_version: "2026-08-10",
    items: [
      {
        action: "notification.read.self",
        description: "查看自己的站内通知",
        scope: { type: "site", id: "peergo" },
        expires_at: "2026-08-11T00:00:00Z",
      },
      {
        action: "notification.read.state.write.self",
        description: "更新自己的通知已读状态",
        scope: { type: "site", id: "peergo" },
        expires_at: "2026-08-11T00:00:00Z",
      },
      {
        action: "notification.archive.self",
        description: "归档自己的站内通知",
        scope: { type: "site", id: "peergo" },
        expires_at: "2026-08-11T00:00:00Z",
      },
      {
        action: "notification.feedback.create.self",
        description: "向站点管理员提交反馈",
        scope: { type: "site", id: "peergo" },
        expires_at: "2026-08-11T00:00:00Z",
      },
    ],
  })
  return queryClient
}

function renderNotificationPage(
  queryClient: QueryClient,
  initialEntry: string
) {
  render(
    <MemoryRouter initialEntries={[initialEntry]}>
      <QueryClientProvider client={queryClient}>
        <TooltipProvider>
          <Routes>
            <Route path="/notifications" element={<NotificationPage />} />
          </Routes>
        </TooltipProvider>
      </QueryClientProvider>
    </MemoryRouter>
  )
}

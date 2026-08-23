import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import { MemoryRouter } from "react-router"
import { describe, expect, it } from "vitest"

import { sessionKeys } from "~/features/auth/api/session.mutations"
import { capabilityKeys } from "~/features/authz/api/capabilities.queries"
import {
  socialNotificationKeys,
  type SocialNotificationPage,
} from "~/features/social/api/social-notifications.queries"
import { SocialNotificationsPage } from "~/features/social/pages/social-notifications-page"

const userId = "0198f20a-6da8-7e51-9c64-111111111111"

describe("SocialNotificationsPage", () => {
  it("keeps social interactions separate and renders PtYes identities", () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false, staleTime: Infinity } },
    })
    queryClient.setQueryData(sessionKeys.current(), {
      user: {
        id: userId,
        username: "receiver",
        display_name: "接收者",
        email_verified: true,
      },
      expires_at: "2026-08-25T00:00:00Z",
      csrf_token: "n".repeat(43),
    })
    queryClient.setQueryData(capabilityKeys.current(userId), {
      policy_version: "test",
      items: [
        {
          action: "notification.read.self",
          description: "查看通知",
          scope: { type: "self", id: userId },
          expires_at: "2026-08-25T00:00:00Z",
        },
        {
          action: "notification.read.state.write.self",
          description: "更新通知",
          scope: { type: "self", id: userId },
          expires_at: "2026-08-25T00:00:00Z",
        },
      ],
    })
    queryClient.setQueryData(
      socialNotificationKeys.page(userId, "all", 20, 0),
      {
        items: [
          {
            id: "0198f20a-6da8-7e51-9c64-222222222222",
            kind: "comment_reply",
            actor: {
              id: "0198f20a-6da8-7e51-9c64-333333333333",
              username: "rainbow-admin",
              display_name: "炫彩管理员",
              followed_by_me: false,
              online: true,
              vip: true,
              administrator: true,
              medals: [
                {
                  id: 8,
                  name: "社区守护者",
                  image_path: "/uploads/medals/guardian.webp",
                },
              ],
            },
            post_id: "0198f20a-6da8-7e51-9c64-444444444444",
            comment_id: "0198f20a-6da8-7e51-9c64-555555555555",
            post_preview: "原动态正文",
            comment_preview: "这是楼层内回复",
            created_at: "2026-08-24T00:00:00Z",
            read_at: null,
          },
        ],
        total: 1,
        unread_count: 1,
        limit: 20,
        offset: 0,
      } satisfies SocialNotificationPage
    )

    render(
      <MemoryRouter initialEntries={["/social/notifications"]}>
        <QueryClientProvider client={queryClient}>
          <SocialNotificationsPage />
        </QueryClientProvider>
      </MemoryRouter>
    )

    expect(
      screen.getByRole("heading", { level: 1, name: "动态圈通知" })
    ).toBeVisible()
    expect(screen.getByText("互动提醒，与站内信分开")).toBeVisible()
    expect(screen.getByRole("button", { name: "评论与回复" })).toBeVisible()
    expect(screen.getByText("炫彩管理员")).toHaveClass("bg-gradient-to-r")
    expect(screen.getByText("管理员")).toBeVisible()
    expect(screen.getByText("VIP")).toBeVisible()
    expect(screen.getByTitle("社区守护者")).toBeVisible()
    expect(screen.getByLabelText("炫彩管理员在线")).toBeVisible()
    expect(screen.getByText("回复了你的评论")).toBeVisible()
    expect(screen.getByText("“这是楼层内回复”")).toBeVisible()
    expect(screen.getByText("原动态：原动态正文")).toBeVisible()
    expect(screen.getByLabelText("未读")).toBeVisible()
  })
})

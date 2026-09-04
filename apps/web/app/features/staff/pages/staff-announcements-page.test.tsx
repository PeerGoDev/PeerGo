import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter } from "react-router"
import { describe, expect, it } from "vitest"

import { TooltipProvider } from "~/components/ui/tooltip"
import { sessionKeys } from "~/features/auth/api/session.mutations"
import { announcementAdministrationKeys } from "~/features/staff/api/announcement-administration.queries"
import { staffSessionKeys } from "~/features/staff/api/staff-session.mutations"
import { StaffAnnouncementsPage } from "~/features/staff/pages/staff-announcements-page"
import type { components } from "~/generated/api"

type CapabilityAction = components["schemas"]["CapabilityAction"]

const userId = "0198f20a-6da8-7e51-9c64-444444444444"

describe("StaffAnnouncementsPage", () => {
  it("keeps the PtYes-style browse controls connected to PeerGo publication data", async () => {
    const user = userEvent.setup()
    const queryClient = createQueryClient([
      "announcement.manage.read",
      "announcement.create",
      "announcement.update",
      "announcement.publish",
    ])

    render(
      <MemoryRouter initialEntries={["/staff/content/announcements"]}>
        <QueryClientProvider client={queryClient}>
          <TooltipProvider>
            <StaffAnnouncementsPage />
          </TooltipProvider>
        </QueryClientProvider>
      </MemoryRouter>
    )

    const heading = await screen.findByRole("heading", { name: "公告管理" })
    expect(heading).toBeVisible()
    expect(heading).toHaveClass("text-xl", "font-bold")
    expect(screen.getByRole("button", { name: "创建新公告" })).toHaveClass(
      "w-35"
    )
    expect(screen.getByLabelText("搜索公告").parentElement).toHaveClass(
      "h-[42px]"
    )
    expect(screen.getByText("显示 2 / 2 条公告")).toBeVisible()
    expect(
      screen.getByRole("columnheader", { name: "标识 / 版本" })
    ).toBeVisible()
    expect(screen.getByText("welcome-to-peergo")).toBeVisible()
    expect(screen.getAllByRole("row")[1]).toHaveClass("h-[70px]")
    expect(
      screen.getAllByRole("button", { name: "编辑公告 欢迎使用 PeerGo" })[0]
    ).toHaveClass("size-8")
    expect(
      screen.getAllByRole("link", {
        name: "查看公开公告 欢迎使用 PeerGo",
      })[0]
    ).toHaveClass("size-8")

    await user.type(screen.getByLabelText("搜索公告"), "announce")
    expect(screen.getAllByText("Tracker 维护预告")).not.toHaveLength(0)
    expect(screen.queryAllByText("欢迎使用 PeerGo")).toHaveLength(0)
    expect(screen.getByText("显示 1 / 2 条公告")).toBeVisible()

    await user.click(screen.getByRole("button", { name: "清空公告搜索" }))
    await user.click(screen.getByRole("combobox", { name: "发布状态" }))
    await user.click(await screen.findByRole("option", { name: "已发布" }))
    expect(screen.getAllByText("欢迎使用 PeerGo")).not.toHaveLength(0)
    expect(screen.queryAllByText("Tracker 维护预告")).toHaveLength(0)

    await user.click(screen.getByRole("combobox", { name: "修订状态" }))
    await user.click(
      await screen.findByRole("option", { name: "有未发布修订" })
    )
    expect(screen.getByText("没有匹配公告")).toBeVisible()
  })
})

function createQueryClient(actions: CapabilityAction[]) {
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
    expires_at: "2026-08-13T00:00:00Z",
    csrf_token: "c".repeat(43),
  })
  queryClient.setQueryData(staffSessionKeys.current(), {
    user: {
      id: userId,
      username: "admin",
      display_name: "管理员",
      email_verified: true,
    },
    expires_at: "2026-08-13T00:00:00Z",
    webauthn_authenticated_at: "2026-08-12T12:00:00Z",
    csrf_token: "s".repeat(43),
  })
  queryClient.setQueryData(staffSessionKeys.capabilities(userId), {
    policy_version: "2026-08-12",
    items: actions.map((action) => ({
      action,
      description: action,
      scope: { type: "site", id: "site" },
      expires_at: "2026-08-13T00:00:00Z",
    })),
  })
  queryClient.setQueryData(announcementAdministrationKeys.list(50, 0), {
    items: [
      {
        id: "welcome-to-peergo",
        title: "欢迎使用 PeerGo",
        summary: "站点首份公开说明",
        status: "published",
        version: 2,
        revision_number: 1,
        has_unpublished_changes: false,
        published_at: "2026-08-01T00:00:00Z",
        created_at: "2026-08-01T00:00:00Z",
        updated_at: "2026-08-01T00:00:00Z",
      },
      {
        id: "tracker-maintenance",
        title: "Tracker 维护预告",
        summary: "维护窗口与 announce 影响范围",
        status: "scheduled",
        version: 4,
        revision_number: 3,
        has_unpublished_changes: true,
        scheduled_for: "2026-08-15T00:00:00Z",
        created_at: "2026-08-10T00:00:00Z",
        updated_at: "2026-08-12T00:00:00Z",
      },
    ],
    total: 2,
    limit: 50,
    offset: 0,
  })
  return queryClient
}

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { createMemoryRouter, RouterProvider } from "react-router"
import { describe, expect, it } from "vitest"

import { sessionKeys } from "~/features/auth/api/session.mutations"
import { capabilityKeys } from "~/features/authz/api/capabilities.queries"
import {
  siteDisplaySettingsKeys,
  type SiteDisplaySettings,
} from "~/features/staff/api/site-display-settings.queries"
import { staffSessionKeys } from "~/features/staff/api/staff-session.mutations"
import { StaffSiteSettingsPage } from "~/features/staff/pages/staff-site-settings-page"

const userId = "0198f20a-6da8-7e51-9c64-555555555555"

describe("StaffSiteSettingsPage", () => {
  it("matches the compact Rousi settings frame without dropping review controls", async () => {
    const user = userEvent.setup()
    const queryClient = createQueryClient()
    const router = createMemoryRouter(
      [
        {
          path: "/staff/settings/site",
          element: <StaffSiteSettingsPage />,
        },
      ],
      { initialEntries: ["/staff/settings/site"] }
    )

    render(
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    )

    const heading = await screen.findByRole("heading", { name: "站点设置" })
    expect(heading).toHaveClass("w-fit")
    expect(heading.closest('[data-slot="card"]')).toHaveClass("gap-0", "py-0")

    const save = screen.getByRole("button", { name: "保存修改" })
    expect(save).toHaveClass("w-28")
    expect(save).toBeDisabled()
    expect(screen.getByLabelText("站点说明").tagName).toBe("INPUT")
    expect(screen.getByLabelText("种子文件名前缀")).toHaveValue("[ROUSI]")
    expect(screen.getByRole("tablist", { name: "站点设置分区" })).toBeVisible()
    await user.click(screen.getByRole("tab", { name: "首页展示" }))
    expect(screen.getByRole("heading", { name: "首页展示" })).toBeVisible()
    await user.click(screen.getByRole("tab", { name: "侧栏菜单" }))
    expect(
      screen.getByRole("heading", { name: "自定义左侧菜单" })
    ).toBeVisible()
    await user.click(screen.getByRole("tab", { name: "变更与审计" }))
    expect(screen.getByRole("heading", { name: "变更与审计" })).toBeVisible()
    expect(screen.getByText(/最近生效于/)).toBeVisible()

    await user.click(screen.getByRole("tab", { name: "基本信息" }))
    await user.clear(screen.getByLabelText("站点名称"))
    await user.type(screen.getByLabelText("站点名称"), "PeerGo Next")
    await user.click(screen.getByRole("tab", { name: "变更与审计" }))
    await user.type(
      screen.getByLabelText("变更理由"),
      "统一站点名称并完成公开页面复核。"
    )
    expect(save).toBeEnabled()

    await user.click(save)
    expect(
      await screen.findByRole("heading", { name: "确认站点与展示变更" })
    ).toBeVisible()
    expect(screen.getByText("PeerGo Next")).toBeVisible()
  })

  it("adds and reviews a third-party Wiki sidebar link", async () => {
    const user = userEvent.setup()
    const queryClient = createQueryClient()
    const router = createMemoryRouter(
      [
        {
          path: "/staff/settings/site",
          element: <StaffSiteSettingsPage />,
        },
      ],
      { initialEntries: ["/staff/settings/site"] }
    )

    render(
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    )

    await user.click(await screen.findByRole("tab", { name: "侧栏菜单" }))
    await user.click(screen.getByRole("button", { name: "新增链接" }))
    await user.type(screen.getByLabelText("菜单名称"), "Wiki")
    await user.type(
      screen.getByLabelText("链接地址"),
      "https://wiki.example.com"
    )
    await user.click(screen.getByRole("button", { name: "保存修改" }))

    expect(
      await screen.findByRole("heading", { name: "确认站点与展示变更" })
    ).toBeVisible()
    expect(screen.getByText("1 项：Wiki")).toBeVisible()
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
        action: "site.display.manage.read",
        description: "读取站点展示设置",
        scope: { type: "site", id: "peergo" },
        expires_at: "2026-08-15T00:00:00Z",
      },
      {
        action: "site.display.update",
        description: "更新站点展示设置",
        scope: { type: "site", id: "peergo" },
        expires_at: "2026-08-15T00:00:00Z",
      },
    ],
  })
  const settings: SiteDisplaySettings = {
    name: "PeerGo",
    description: "一套从零设计的现代 Private Tracker 平台",
    torrent_filename_prefix: "[ROUSI]",
    default_torrent_view: "list",
    show_latest_announcement: true,
    custom_navigation_items: [],
    version: 7,
    effective_at: "2026-08-14T00:00:00Z",
    updated_at: "2026-08-14T00:00:00Z",
  }
  queryClient.setQueryData(siteDisplaySettingsKeys.detail(), settings)
  return queryClient
}

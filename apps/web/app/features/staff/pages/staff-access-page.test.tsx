import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter } from "react-router"
import { describe, expect, it } from "vitest"

import { sessionKeys } from "~/features/auth/api/session.mutations"
import { capabilityKeys } from "~/features/authz/api/capabilities.queries"
import { siteKeys } from "~/features/site/api/site.queries"
import { staffSessionKeys } from "~/features/staff/api/staff-session.mutations"
import { managedUserListQueryOptions } from "~/features/staff/api/user-administration.queries"
import { StaffAccessPage } from "~/features/staff/pages/staff-access-page"
import { torrentListQueryOptions } from "~/features/torrent/api/torrent.queries"
import type { components } from "~/generated/api"

type CapabilityAction = components["schemas"]["CapabilityAction"]

describe("StaffAccessPage", () => {
  it("renders a compact dashboard from real session, site, and capability data", async () => {
    const user = userEvent.setup()
    const queryClient = createStaffQueryClient([
      "user.account.read",
      "torrent.manage.read",
      "torrent.purchase.manage.read",
      "torrent.review",
      "announcement.manage.read",
      "authz.grant.read",
    ])

    render(
      <MemoryRouter initialEntries={["/staff"]}>
        <QueryClientProvider client={queryClient}>
          <StaffAccessPage />
        </QueryClientProvider>
      </MemoryRouter>
    )

    expect(screen.getByRole("heading", { name: "仪表盘" })).toBeVisible()
    expect(screen.getByText("12327")).toBeVisible()
    expect(screen.getByText("222")).toBeVisible()
    expect(screen.getByText("8999")).toBeVisible()
    expect(screen.getByText("6", { selector: "p" })).toBeVisible()
    expect(screen.getByText("@admin")).toBeVisible()
    expect(screen.getByText("已加载")).toBeVisible()
    expect(screen.getByText("当前站点在线用户")).toHaveClass("sr-only")
    expect(
      screen.getByText("在线人数").closest('[data-slot="card"]')
    ).toHaveClass("h-[114px]")
    expect(
      screen
        .getByText("快捷操作", { exact: true })
        .closest('[data-slot="card"]')
    ).toHaveClass("gap-(--card-spacing)", "py-(--card-spacing)")
    expect(screen.getByRole("link", { name: "用户管理" })).toHaveAttribute(
      "href",
      "/staff/users"
    )
    expect(screen.getByRole("link", { name: "种子管理" })).toHaveAttribute(
      "href",
      "/staff/content/torrents"
    )
    expect(screen.getByRole("link", { name: "购买记录" })).toHaveAttribute(
      "href",
      "/staff/content/torrent-purchases"
    )
    expect(screen.getByRole("link", { name: "种子审核" })).toHaveAttribute(
      "href",
      "/staff/content/torrent-reviews"
    )
    expect(screen.queryByRole("link", { name: "站点设置" })).toBeNull()
    expect(
      screen.getByRole("link", { name: "打开权限与任期" })
    ).toHaveAttribute("href", "/staff/governance")

    await user.click(screen.getByRole("button", { name: "刷新" }))
    expect(await screen.findByRole("button", { name: "刷新" })).toBeEnabled()
  })
})

function createStaffQueryClient(actions: CapabilityAction[]) {
  const userId = "0198f20a-6da8-7e51-9c64-333333333333"
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  })
  queryClient.setQueryData(siteKeys.info(), {
    name: "PeerGo",
    description: "测试站点",
    registration_mode: "open",
    registration_username_min_characters: 3,
    registration_username_max_characters: 20,
    registration_email_domain_mode: "any",
    human_verification: disabledHumanVerification,
    online_users: 222,
    default_torrent_view: "list",
    show_latest_announcement: true,
  })
  queryClient.setQueryData(
    managedUserListQueryOptions({
      query: "",
      status: "all",
      page: 1,
      pageSize: 20,
    }).queryKey,
    {
      items: [],
      total: 12_327,
      page: 1,
      page_size: 20,
      summary: {
        total: 12_327,
        active: 11_263,
        banned: 1_064,
        vip: 43,
        download_restricted: 11,
        unverified: 108,
      },
    }
  )
  queryClient.setQueryData(torrentListQueryOptions({ limit: 1 }).queryKey, {
    items: [],
    total: 8_999,
    limit: 1,
    offset: 0,
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
  queryClient.setQueryData(capabilityKeys.current(userId), {
    policy_version: "2026-08-12",
    items: [],
  })
  queryClient.setQueryData(staffSessionKeys.current(), {
    user: {
      id: userId,
      username: "admin",
      display_name: "管理员",
      email_verified: true,
    },
    expires_at: "2026-08-12T14:00:00Z",
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
  return queryClient
}

const disabledHumanVerification = {
  provider: "disabled" as const,
  site_key: "",
  registration_enabled: false,
  login_enabled: false,
  password_recovery_enabled: false,
}

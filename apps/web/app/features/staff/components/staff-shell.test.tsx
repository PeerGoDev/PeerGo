import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter } from "react-router"
import { describe, expect, it, vi } from "vitest"

import { TooltipProvider } from "~/components/ui/tooltip"
import { sessionKeys } from "~/features/auth/api/session.mutations"
import { capabilityKeys } from "~/features/authz/api/capabilities.queries"
import { siteKeys } from "~/features/site/api/site.queries"
import { StaffShell } from "~/features/staff/components/staff-shell"
import { staffSessionKeys } from "~/features/staff/api/staff-session.mutations"
import type { components } from "~/generated/api"

vi.mock("~/hooks/use-mobile", () => ({
  useIsMobile: () => true,
}))

type CapabilityAction = components["schemas"]["CapabilityAction"]

describe("StaffShell navigation", () => {
  it("keeps implemented staff areas discoverable before security verification", async () => {
    const user = userEvent.setup()
    const queryClient = createStaffQueryClient([], {
      staffSession: false,
      webActions: ["staff.session.create.self", "staff.credential.enroll.self"],
    })

    render(
      <MemoryRouter initialEntries={["/staff"]}>
        <QueryClientProvider client={queryClient}>
          <TooltipProvider>
            <StaffShell>
              <main>验证管理员身份</main>
            </StaffShell>
          </TooltipProvider>
        </QueryClientProvider>
      </MemoryRouter>
    )

    await user.click(screen.getByRole("button", { name: "切换后台侧栏" }))
    const navigation = await screen.findByRole("dialog")

    expect(navigation.querySelector('a[href="/staff"]')).toHaveClass(
      "max-lg:w-[calc(100%-2.5rem)]",
      "max-lg:self-start"
    )
    expect(within(navigation).getByText("P")).toHaveClass(
      "group-data-[collapsible=icon]:inline"
    )

    expect(
      within(navigation).getByRole("link", { name: "公告管理" })
    ).toHaveAttribute("href", "/staff/content/announcements")
    expect(
      within(navigation).getByRole("link", { name: "用户管理" })
    ).toHaveAttribute("href", "/staff/users")
    expect(
      within(navigation).getByRole("link", { name: "评论审核" })
    ).toHaveAttribute("href", "/staff/content/comments")
    expect(
      within(navigation).getByRole("link", { name: "购买记录" })
    ).toHaveAttribute("href", "/staff/content/torrent-purchases")
    expect(
      within(navigation).getByRole("link", { name: "安全凭据登记" })
    ).toHaveAttribute("href", "/staff/enroll")
  })

  it("keeps the organized staff sections available from the compact navigation", async () => {
    const user = userEvent.setup()
    const queryClient = createStaffQueryClient([
      "user.account.read",
      "authz.grant.read",
      "torrent.manage.read",
      "torrent.purchase.manage.read",
      "torrent.review",
      "announcement.manage.read",
      "social.report.read",
      "category.manage.read",
      "site.display.manage.read",
      "site.registration.manage.read",
      "promotion.manage.read",
      "economy.seedingreward.policy.read",
      "hnr.policy.read",
      "operations.monitor.read",
      "tracker.policy.read",
    ])

    render(
      <MemoryRouter initialEntries={["/staff/content/announcements"]}>
        <QueryClientProvider client={queryClient}>
          <TooltipProvider>
            <StaffShell>
              <main>后台内容</main>
            </StaffShell>
          </TooltipProvider>
        </QueryClientProvider>
      </MemoryRouter>
    )

    await user.click(screen.getByRole("button", { name: "切换后台侧栏" }))
    const navigation = await screen.findByRole("dialog")

    expect(within(navigation).getByText("概览")).toBeVisible()
    expect(within(navigation).getByText("内容")).toBeVisible()
    expect(within(navigation).getByText("用户")).toBeVisible()
    expect(within(navigation).getByText("设置")).toBeVisible()
    expect(within(navigation).getByText("运营监控")).toBeVisible()
    expect(
      within(navigation).getByRole("link", { name: "仪表盘" })
    ).not.toHaveAttribute("data-active", "true")
    const activeAnnouncement = within(navigation).getByRole("link", {
      name: "公告管理",
    })
    expect(activeAnnouncement).toHaveAttribute(
      "href",
      "/staff/content/announcements"
    )
    expect(activeAnnouncement).toHaveAttribute("data-active")
    expect(activeAnnouncement).toHaveClass(
      "h-11",
      "justify-end",
      "rounded-none",
      "group-data-[collapsible=icon]:h-11!",
      "group-data-[collapsible=icon]:w-full!"
    )
    expect(activeAnnouncement).toHaveAttribute("data-active")
    expect(
      within(navigation).getByRole("link", { name: "种子管理" })
    ).toHaveAttribute("href", "/staff/content/torrents")
    expect(
      within(navigation).getByRole("link", { name: "购买记录" })
    ).toHaveAttribute("href", "/staff/content/torrent-purchases")
    expect(
      within(navigation).getByRole("link", { name: "种子审核" })
    ).toHaveAttribute("href", "/staff/content/torrent-reviews")
    expect(
      within(navigation).getByRole("link", { name: "评论审核" })
    ).toHaveAttribute("href", "/staff/content/comments")
    expect(
      within(navigation).getByRole("link", { name: "分类管理" })
    ).toHaveAttribute("href", "/staff/content/categories")
    expect(
      within(navigation).getByRole("link", { name: "用户管理" })
    ).toHaveAttribute("href", "/staff/users")
    expect(
      within(navigation).getByRole("link", { name: "权限与任期" })
    ).toHaveAttribute("href", "/staff/governance")
    await user.click(
      within(navigation).getByRole("button", { name: "站点设置" })
    )
    expect(
      within(navigation).getByRole("link", { name: "基础设置" })
    ).toHaveAttribute("href", "/staff/settings/site")
    expect(
      within(navigation).getByRole("link", { name: "注册与认证" })
    ).toHaveAttribute("href", "/staff/settings/registration")
    expect(
      within(navigation).getByRole("link", { name: "图片与存储" })
    ).toHaveAttribute("href", "/staff/settings/storage")
    expect(
      within(navigation).getByRole("link", { name: "邮件设置" })
    ).toHaveAttribute("href", "/staff/settings/email")
    expect(
      within(navigation).getByRole("link", { name: "VIP 与用户资料" })
    ).toHaveAttribute("href", "/staff/settings/vip-profile")
    await user.click(
      within(navigation).getByRole("button", { name: "种子与 Tracker" })
    )
    expect(
      within(navigation).getByRole("link", { name: "种子规则" })
    ).toHaveAttribute("href", "/staff/settings/torrents")
    expect(
      within(navigation).getByRole("link", { name: "Tracker 参数" })
    ).toHaveAttribute("href", "/staff/settings/tracker")
    expect(
      within(navigation).getByRole("link", { name: "优惠规则" })
    ).toHaveAttribute("href", "/staff/settings/promotions")
    expect(
      within(navigation).getByRole("link", { name: "盒子设置" })
    ).toHaveAttribute("href", "/staff/settings/seedbox")
    expect(
      within(navigation).getByRole("link", { name: "分享率与 H&R" })
    ).toHaveAttribute("href", "/staff/settings/ratio-hnr")
    await user.click(
      within(navigation).getByRole("button", { name: "等级与魔力" })
    )
    expect(
      within(navigation).getByRole("link", { name: "做种奖励" })
    ).toHaveAttribute("href", "/staff/settings/seeding-rewards")
    expect(
      within(navigation).getByRole("link", { name: "经验与等级" })
    ).toHaveAttribute("href", "/staff/settings/progression/levels")
    expect(
      within(navigation).getByRole("link", { name: "签到与活动奖励" })
    ).toHaveAttribute("href", "/staff/settings/activity-rewards")
    expect(
      within(navigation).getByRole("link", { name: "魔力值使用规则" })
    ).toHaveAttribute("href", "/staff/settings/magic-usage")
    expect(
      within(navigation).getByRole("link", { name: "Tracker 状态" })
    ).toHaveAttribute("href", "/staff/operations/tracker")
    expect(
      within(navigation).getByRole("link", { name: "Worker 状态" })
    ).toHaveAttribute("href", "/staff/operations/workers")
    expect(
      within(navigation).getByRole("link", { name: "任务异常与审计" })
    ).toHaveAttribute("href", "/staff/operations/incidents")
  })

  it("opens the settings parent and marks the implemented child active", async () => {
    const user = userEvent.setup()
    const queryClient = createStaffQueryClient(["site.display.manage.read"])

    render(
      <MemoryRouter initialEntries={["/staff/settings/site"]}>
        <QueryClientProvider client={queryClient}>
          <TooltipProvider>
            <StaffShell>
              <main>设置内容</main>
            </StaffShell>
          </TooltipProvider>
        </QueryClientProvider>
      </MemoryRouter>
    )

    await user.click(screen.getByRole("button", { name: "切换后台侧栏" }))
    const navigation = await screen.findByRole("dialog")
    const parent = within(navigation).getByRole("button", {
      name: "站点设置",
    })
    const child = within(navigation).getByRole("link", { name: "基础设置" })

    expect(parent).toHaveAttribute("aria-expanded", "true")
    expect(child).toHaveAttribute("href", "/staff/settings/site")
    expect(child).toHaveAttribute("data-active")
  })
})

function createStaffQueryClient(
  actions: CapabilityAction[],
  options: {
    staffSession?: boolean
    webActions?: CapabilityAction[]
  } = {}
) {
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
    items: (options.webActions ?? []).map((action) => ({
      action,
      description: action,
      scope: { type: "site", id: "site" },
      expires_at: "2026-08-13T00:00:00Z",
    })),
  })
  if (options.staffSession !== false) {
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
  } else {
    queryClient.setQueryData(staffSessionKeys.current(), null)
  }
  return queryClient
}

const disabledHumanVerification = {
  provider: "disabled" as const,
  site_key: "",
  registration_enabled: false,
  login_enabled: false,
  password_recovery_enabled: false,
}

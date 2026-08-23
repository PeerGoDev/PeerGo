import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen, waitFor, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter, useLocation } from "react-router"
import { describe, expect, it, vi } from "vitest"

import { TooltipProvider } from "~/components/ui/tooltip"
import { sessionKeys } from "~/features/auth/api/session.mutations"
import { capabilityKeys } from "~/features/authz/api/capabilities.queries"
import { notificationKeys } from "~/features/notification/api/notifications.queries"
import { AppShell } from "~/features/shell/components/app-shell"
import { siteKeys } from "~/features/site/api/site.queries"
import { trafficKeys } from "~/features/traffic/api/traffic.queries"

vi.mock("~/hooks/use-mobile", () => ({
  useIsMobile: () => true,
}))

describe("AppShell mobile navigation", () => {
  it("closes the navigation sheet after following a sidebar link", async () => {
    const user = userEvent.setup()
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
      online_users: 1,
      default_torrent_view: "list",
      show_latest_announcement: true,
    })
    queryClient.setQueryData(sessionKeys.current(), null)

    render(
      <MemoryRouter initialEntries={["/announcements"]}>
        <QueryClientProvider client={queryClient}>
          <TooltipProvider>
            <AppShell>
              <main>
                <LocationProbe />
              </main>
            </AppShell>
          </TooltipProvider>
        </QueryClientProvider>
      </MemoryRouter>
    )

    expect(screen.getAllByRole("main")).toHaveLength(1)

    expect(screen.getByRole("button", { name: "切换侧栏" })).toHaveClass(
      "-ml-2",
      "size-10",
      "rounded-lg",
      "[&_svg]:size-6"
    )
    expect(
      screen.queryByRole("button", { name: "切换到深色模式" })
    ).not.toBeInTheDocument()

    await user.click(screen.getByRole("button", { name: "切换侧栏" }))

    const navigation = await screen.findByRole("dialog")
    expect(navigation).toHaveStyle({
      "--sidebar-width": "15rem",
      width: "240px",
    })
    expect(document.querySelector('[data-slot="sheet-overlay"]')).toHaveClass(
      "bg-black/50",
      "supports-backdrop-filter:backdrop-blur-none"
    )
    expect(
      within(navigation).getByRole("button", { name: "收起导航" })
    ).toBeVisible()
    expect(
      within(navigation).queryByRole("button", { name: "关闭" })
    ).not.toBeInTheDocument()
    expect(navigation.querySelector('a[href="/"]')).not.toHaveClass(
      "max-lg:w-[calc(100%-2.5rem)]"
    )
    expect(navigation.querySelector('a[href="/"]')).not.toHaveFocus()
    expect(within(navigation).getByText("Powered by PeerGo")).toHaveClass(
      "hidden",
      "lg:block"
    )
    expect(
      navigation.querySelector('[data-slot="sidebar-separator"]')
    ).toHaveClass("mx-0")

    await user.click(within(navigation).getByRole("link", { name: "登录" }))

    expect(screen.getByTestId("current-path")).toHaveTextContent("/login")
    await waitFor(() => {
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument()
    })
  })

  it("keeps PeerGo account utilities visible in both primary and quick navigation", async () => {
    const user = userEvent.setup()
    const userId = "0198f20a-6da8-7e51-9c64-222222222222"
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
      online_users: 1,
      default_torrent_view: "list",
      show_latest_announcement: true,
    })
    queryClient.setQueryData(sessionKeys.current(), {
      user: {
        id: userId,
        username: "demo",
        display_name: "演示用户",
        email_verified: true,
      },
      expires_at: "2026-08-13T00:00:00Z",
      csrf_token: "c".repeat(43),
    })
    queryClient.setQueryData(capabilityKeys.current(userId), {
      policy_version: "2026-08-12",
      items: [
        { action: "traffic.read.self" },
        { action: "ratio.assessment.read.self" },
        { action: "hnr.read.self" },
        { action: "torrent.submission.read.self" },
        { action: "torrent.bookmark.read.self" },
        { action: "notification.read.self" },
        { action: "economy.medal.read.self" },
        { action: "staff.session.create.self" },
      ],
    })
    queryClient.setQueryData(notificationKeys.summary(userId), {
      unread_count: 7,
    })
    queryClient.setQueryData(trafficKeys.current(userId), {
      totals: {
        raw_uploaded_bytes: "0",
        raw_downloaded_bytes: "0",
        credited_uploaded_bytes: "0",
        charged_downloaded_bytes: "0",
        entry_count: "0",
        last_settled_at: null,
        projection_updated_at: null,
      },
      entries: [],
    })

    render(
      <MemoryRouter initialEntries={["/account/traffic"]}>
        <QueryClientProvider client={queryClient}>
          <TooltipProvider>
            <AppShell>
              <main>首页内容</main>
            </AppShell>
          </TooltipProvider>
        </QueryClientProvider>
      </MemoryRouter>
    )

    expect(screen.getByRole("button", { name: "切换到深色模式" })).toBeVisible()
    await user.click(screen.getByRole("button", { name: "切换侧栏" }))
    const navigation = await screen.findByRole("dialog")

    expect(
      within(navigation).getByRole("link", { name: "demo" })
    ).not.toHaveAttribute("data-active", "true")
    expect(
      within(navigation).getByRole("link", { name: /消息/ })
    ).toHaveAttribute("href", "/notifications")
    expect(within(navigation).getByText("7")).toBeVisible()
    expect(
      within(navigation).getByRole("link", { name: "流量统计" })
    ).toHaveAttribute("href", "/account/traffic")
    expect(
      within(navigation).getByRole("link", { name: "分享率考核" })
    ).toHaveAttribute("href", "/account/ratio")
    expect(within(navigation).getByRole("link", { name: "首页" })).toHaveClass(
      "group-data-[collapsible=icon]:h-10!",
      "group-data-[collapsible=icon]:w-full!"
    )
    expect(
      within(navigation).getByRole("link", { name: "H&R" })
    ).toHaveAttribute("href", "/account/hnr")
    expect(
      within(navigation).getByRole("link", { name: "勋章" })
    ).toHaveAttribute("href", "/medals")
    expect(
      within(navigation).queryByRole("button", { name: "账户与成长" })
    ).not.toBeInTheDocument()
    expect(
      within(navigation).queryByRole("link", { name: "我的上传" })
    ).not.toBeInTheDocument()
    expect(
      within(navigation).getByRole("link", { name: "我的收藏" })
    ).toHaveAttribute("href", "/account/bookmarks")
    expect(
      within(navigation).getByRole("link", { name: "账户设置" })
    ).toHaveAttribute("href", "/account")
    expect(
      within(navigation).getByRole("link", { name: "种子审核" })
    ).toHaveAttribute("href", "/review")
    expect(
      within(navigation).getByRole("link", { name: "管理后台" })
    ).toHaveAttribute("href", "/staff")
    const primaryGroup = within(navigation)
      .getByRole("link", { name: "搜索" })
      .closest('[data-slot="sidebar-group"]')
    const socialGroup = within(navigation)
      .getByRole("link", { name: "动态圈" })
      .closest('[data-slot="sidebar-group"]')
    expect(primaryGroup).not.toBe(socialGroup)
    expect(socialGroup).toHaveTextContent("社区")

    await user.click(
      within(navigation).getByRole("button", { name: "收起导航" })
    )
    await waitFor(() => {
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument()
    })

    await user.click(screen.getByRole("button", { name: "打开账户菜单" }))

    expect(
      await screen.findByRole("menuitem", { name: "流量统计" })
    ).toHaveAttribute("href", "/account/traffic")
    expect(
      screen.getByRole("menuitem", { name: "分享率考核" })
    ).toHaveAttribute("href", "/account/ratio")
    expect(screen.getByRole("menuitem", { name: /消息/ })).toHaveAttribute(
      "href",
      "/notifications"
    )
    expect(screen.getByRole("menuitem", { name: "个人主页" })).toHaveAttribute(
      "href",
      "/user/demo"
    )
    expect(screen.getByRole("menuitem", { name: "H&R" })).toHaveAttribute(
      "href",
      "/account/hnr"
    )
    expect(
      screen.queryByRole("menuitem", { name: "我的上传" })
    ).not.toBeInTheDocument()
    expect(screen.getByRole("menuitem", { name: "我的收藏" })).toHaveAttribute(
      "href",
      "/account/bookmarks"
    )
    expect(screen.getByRole("menuitem", { name: "账户设置" })).toHaveAttribute(
      "href",
      "/account"
    )
    expect(screen.getByRole("menuitem", { name: "员工后台" })).toHaveAttribute(
      "href",
      "/staff"
    )
  })

  it("keeps the signed-in Rousi shell on account entry routes", async () => {
    const user = userEvent.setup()
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
      online_users: 9,
      default_torrent_view: "list",
      show_latest_announcement: true,
    })
    queryClient.setQueryData(sessionKeys.current(), {
      user: {
        id: userId,
        username: "demo",
        display_name: "演示用户",
        email_verified: true,
      },
      expires_at: "2026-08-13T00:00:00Z",
      csrf_token: "c".repeat(43),
    })
    queryClient.setQueryData(capabilityKeys.current(userId), {
      policy_version: "2026-08-12",
      items: [{ action: "traffic.read.self" }],
    })
    queryClient.setQueryData(trafficKeys.current(userId), {
      totals: {
        raw_uploaded_bytes: "0",
        raw_downloaded_bytes: "0",
        credited_uploaded_bytes: "0",
        charged_downloaded_bytes: "0",
        entry_count: "0",
        last_settled_at: null,
        projection_updated_at: null,
      },
      entries: [],
    })

    render(
      <MemoryRouter initialEntries={["/forgot-password"]}>
        <QueryClientProvider client={queryClient}>
          <TooltipProvider>
            <AppShell>
              <main>找回密码</main>
            </AppShell>
          </TooltipProvider>
        </QueryClientProvider>
      </MemoryRouter>
    )

    expect(
      screen.getByRole("button", { name: "打开账户菜单" })
    ).toBeInTheDocument()

    await user.click(screen.getByRole("button", { name: "切换侧栏" }))
    const navigation = await screen.findByRole("dialog")

    expect(
      within(navigation).getByRole("link", { name: "首页" })
    ).toHaveAttribute("href", "/")
    expect(
      within(navigation).queryByRole("link", { name: "登录" })
    ).not.toBeInTheDocument()
  })
})

const disabledHumanVerification = {
  provider: "disabled" as const,
  site_key: "",
  registration_enabled: false,
  login_enabled: false,
  password_recovery_enabled: false,
}

function LocationProbe() {
  const location = useLocation()
  return <output data-testid="current-path">{location.pathname}</output>
}

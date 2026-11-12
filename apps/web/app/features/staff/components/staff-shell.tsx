import type { ReactNode } from "react"
import { Link, useLocation, useNavigate } from "react-router"
import {
  BadgeCheckIcon,
  BellRingIcon,
  ChevronRightIcon,
  ExternalLinkIcon,
  FolderTreeIcon,
  GraduationCapIcon,
  HomeIcon,
  KeyRoundIcon,
  LayoutDashboardIcon,
  LogOutIcon,
  MegaphoneIcon,
  MessageSquareWarningIcon,
  ShieldCheckIcon,
  Settings2Icon,
  UserRoundIcon,
  UsersRoundIcon,
  ClipboardCheckIcon,
  HardDriveIcon,
  RouterIcon,
  ServerCogIcon,
  ShieldAlertIcon,
  ShoppingBagIcon,
  RocketIcon,
  MessageCircleMoreIcon,
  BookOpenTextIcon,
} from "lucide-react"

import { Badge } from "~/components/ui/badge"
import { Button } from "~/components/ui/button"
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "~/components/ui/collapsible"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "~/components/ui/dropdown-menu"
import { Separator } from "~/components/ui/separator"
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarInset,
  SidebarMenu,
  SidebarMenuBadge,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarMenuSub,
  SidebarMenuSubButton,
  SidebarMenuSubItem,
  SidebarProvider,
  SidebarRail,
  SidebarTrigger,
} from "~/components/ui/sidebar"
import { Spinner } from "~/components/ui/spinner"
import {
  useDeleteWebSession,
  useWebSession,
} from "~/features/auth/api/session.mutations"
import { useCapabilities } from "~/features/authz/api/capabilities.queries"
import { useSiteInfo } from "~/features/site/api/site.queries"
import { HeaderTrafficSummary } from "~/features/shell/components/header-traffic-summary"
import {
  navigationButtonClass,
  navigationCountBadgeClass,
  navigationSubMenuClass,
  subNavigationButtonClass,
} from "~/features/shell/components/navigation-styles"
import { ShellBreadcrumb } from "~/features/shell/components/shell-breadcrumb"
import { SidebarCollapseControl } from "~/features/shell/components/sidebar-collapse-control"
import { ThemeToggleButton } from "~/features/shell/components/theme-toggle-button"
import type { BreadcrumbItem } from "~/features/shell/model/breadcrumbs"
import {
  useDeleteStaffSession,
  useStaffCapabilities,
  useStaffSession,
} from "~/features/staff/api/staff-session.mutations"
import { useStaffPendingOverview } from "~/features/staff/api/staff-pending-overview.queries"
import { hasCapability } from "~/features/staff/model/capability"
import {
  staffSettingsNavigationGroups,
  type StaffSettingsNavigationItem,
} from "~/features/staff/model/staff-settings-navigation"
import { cn } from "~/lib/utils"
import { UserAvatar } from "~/shared/components/user-avatar"

const overviewNavigation = [
  { label: "仪表盘", to: "/staff", icon: LayoutDashboardIcon },
  { label: "上线检查", to: "/staff/setup", icon: RocketIcon },
]

const enrollmentNavigation = [
  { label: "安全凭据登记", to: "/staff/enroll", icon: KeyRoundIcon },
]

const siteSettingsGroup = staffSettingsNavigationGroups.find(
  (group) => group.id === "site"
)!
const torrentTrackerSettingsGroup = staffSettingsNavigationGroups.find(
  (group) => group.id === "torrent"
)!
const economySettingsGroup = staffSettingsNavigationGroups.find(
  (group) => group.id === "economy"
)!

const operationsNavigation = [
  {
    label: "Tracker 状态",
    to: "/staff/operations/tracker",
    icon: RouterIcon,
    action: "operations.monitor.read" as const,
  },
  {
    label: "Worker 状态",
    to: "/staff/operations/workers",
    icon: ServerCogIcon,
    action: "operations.monitor.read" as const,
  },
  {
    label: "任务异常与审计",
    to: "/staff/operations/incidents",
    icon: ShieldAlertIcon,
    action: "operations.monitor.read" as const,
  },
]

const userNavigation = [
  {
    label: "用户管理",
    to: "/staff/users",
    icon: UsersRoundIcon,
    action: "user.account.read" as const,
  },
  {
    label: "新人考核",
    to: "/staff/assessments",
    icon: GraduationCapIcon,
    action: "newcomer.assessment.read" as const,
  },
  {
    label: "工作组管理",
    to: "/staff/workgroups",
    icon: ShieldCheckIcon,
    action: "workgroup.manage.read" as const,
  },
  {
    label: "权限与任期",
    to: "/staff/governance",
    icon: BadgeCheckIcon,
    action: "authz.grant.read" as const,
  },
]

const contentNavigation = [
  {
    label: "种子管理",
    to: "/staff/content/torrents",
    icon: HardDriveIcon,
    action: "torrent.manage.read" as const,
  },
  {
    label: "购买记录",
    to: "/staff/content/torrent-purchases",
    icon: ShoppingBagIcon,
    action: "torrent.purchase.manage.read" as const,
  },
  {
    label: "种子审核",
    to: "/staff/content/torrent-reviews",
    icon: ClipboardCheckIcon,
    action: "torrent.review" as const,
  },
  {
    label: "公告管理",
    to: "/staff/content/announcements",
    icon: MegaphoneIcon,
    action: "announcement.manage.read" as const,
  },
  {
    label: "Wiki 管理",
    to: "/staff/content/wiki",
    icon: BookOpenTextIcon,
    action: "wiki.page.manage.read" as const,
  },
  {
    label: "评论审核",
    to: "/staff/content/comments",
    icon: MessageSquareWarningIcon,
    action: "social.report.read" as const,
  },
  {
    label: "动态圈管理",
    to: "/staff/content/social",
    icon: MessageCircleMoreIcon,
    action: "social.board.manage.read" as const,
  },
  {
    label: "分类管理",
    to: "/staff/content/categories",
    icon: FolderTreeIcon,
    action: "category.manage.read" as const,
  },
]

export function StaffShell({ children }: { children: ReactNode }) {
  const location = useLocation()
  const navigate = useNavigate()
  const webSession = useWebSession()
  const webCapabilities = useCapabilities(webSession.data?.user.id)
  const siteInfo = useSiteInfo()
  const staffSession = useStaffSession()
  const staffCapabilities = useStaffCapabilities(staffSession.data?.user.id)
  const deleteStaffSession = useDeleteStaffSession()
  const deleteWebSession = useDeleteWebSession()
  const compactSiteMark = Array.from(siteInfo.data?.name ?? "PeerGo")[0] ?? "P"

  const canEnroll = hasCapability(
    webCapabilities.data,
    "staff.credential.enroll.self"
  )
  const canStartStaffSession = hasCapability(
    webCapabilities.data,
    "staff.session.create.self"
  )
  const canReadTraffic = hasCapability(
    webCapabilities.data,
    "traffic.read.self"
  )
  const canReadEconomy = hasCapability(
    webCapabilities.data,
    "economy.read.self"
  )
  // Before WebAuthn elevation the web-session projection intentionally cannot
  // disclose staff-audience grants. A member who is allowed to start a staff
  // session may still discover the implemented administration areas; every
  // destination remains protected by StaffAccessGate and is narrowed to the
  // actual staff-session capability set immediately after elevation.
  const visibleStaffCapabilities = staffSession.data
    ? staffCapabilities.data
    : undefined
  const pending = useStaffPendingOverview(visibleStaffCapabilities)
  const showImplementedStaffAreas = canStartStaffSession && !staffSession.data
  const userItems = userNavigation.filter(
    (item) =>
      showImplementedStaffAreas ||
      hasCapability(visibleStaffCapabilities, item.action)
  )
  const contentItems = contentNavigation.filter(
    (item) =>
      showImplementedStaffAreas ||
      hasCapability(visibleStaffCapabilities, item.action)
  )
  const siteSettingsItems = siteSettingsGroup.items.filter(
    (item) =>
      showImplementedStaffAreas ||
      hasCapability(visibleStaffCapabilities, item.action)
  )
  const torrentTrackerSettingsItems = torrentTrackerSettingsGroup.items.filter(
    (item) =>
      showImplementedStaffAreas ||
      hasCapability(visibleStaffCapabilities, item.action)
  )
  const economySettingsItems = economySettingsGroup.items.filter(
    (item) =>
      showImplementedStaffAreas ||
      hasCapability(visibleStaffCapabilities, item.action)
  )
  const operationsItems = operationsNavigation.filter(
    (item) =>
      showImplementedStaffAreas ||
      hasCapability(visibleStaffCapabilities, item.action)
  )
  const enrollmentItems = canEnroll ? enrollmentNavigation : []

  async function handleStaffLogout() {
    if (!staffSession.data) {
      return
    }
    try {
      if (staffSession.data.authentication_method === "account_session") {
        if (!webSession.data) {
          return
        }
        await deleteWebSession.mutateAsync(webSession.data.csrf_token)
        navigate("/login", { replace: true })
      } else {
        await deleteStaffSession.mutateAsync(staffSession.data.csrf_token)
        navigate("/staff", { replace: true })
      }
    } catch {
      // Mutation state leaves the retry affordance in the account menu.
    }
  }

  const accountSessionAdmin =
    staffSession.data?.authentication_method === "account_session"
  const logoutPending = accountSessionAdmin
    ? deleteWebSession.isPending
    : deleteStaffSession.isPending
  const logoutFailed = accountSessionAdmin
    ? deleteWebSession.isError
    : deleteStaffSession.isError

  return (
    <SidebarProvider defaultOpen>
      <Sidebar collapsible="icon">
        <SidebarHeader className="h-14 shrink-0 flex-row items-center gap-0 px-5 py-0 group-data-[collapsible=icon]:justify-center group-data-[collapsible=icon]:px-0 md:h-16">
          <Link
            to="/staff"
            className="flex min-w-0 items-center gap-2 rounded-md outline-none group-data-[collapsible=icon]:justify-center focus-visible:ring-2 focus-visible:ring-sidebar-ring"
          >
            <span className="truncate text-lg font-bold tracking-title text-foreground group-data-[collapsible=icon]:hidden">
              管理后台
            </span>
            <span className="shrink-0 rounded-sm border border-primary/40 px-1 py-px text-[0.5625rem] leading-none font-semibold tracking-[0.14em] text-primary uppercase group-data-[collapsible=icon]:hidden">
              staff
            </span>
            <span
              aria-hidden="true"
              className="hidden size-9 items-center justify-center rounded-lg bg-primary sheen text-sm font-bold text-primary-foreground shadow-button group-data-[collapsible=icon]:flex"
            >
              {compactSiteMark.toLocaleUpperCase()}
            </span>
          </Link>
        </SidebarHeader>

        <SidebarContent className="pb-2">
          <StaffNavigationGroup
            label="概览"
            items={overviewNavigation}
            pathname={location.pathname}
            badges={pending.byRoute}
          />
          {contentItems.length > 0 ? (
            <StaffNavigationGroup
              label="内容"
              items={contentItems}
              pathname={location.pathname}
              badges={pending.byRoute}
            />
          ) : null}
          {userItems.length > 0 ? (
            <StaffNavigationGroup
              label="用户"
              items={userItems}
              pathname={location.pathname}
              badges={pending.byRoute}
            />
          ) : null}
          {operationsItems.length > 0 ? (
            <StaffNavigationGroup
              label="运营监控"
              items={operationsItems}
              pathname={location.pathname}
              badges={pending.byRoute}
            />
          ) : null}
          {siteSettingsItems.length > 0 ||
          torrentTrackerSettingsItems.length > 0 ||
          economySettingsItems.length > 0 ||
          enrollmentItems.length > 0 ? (
            <StaffSettingsNavigationGroup
              siteSettingsItems={siteSettingsItems}
              torrentTrackerSettingsItems={torrentTrackerSettingsItems}
              economySettingsItems={economySettingsItems}
              enrollmentItems={enrollmentItems}
              pathname={location.pathname}
              badges={pending.byRoute}
            />
          ) : null}
        </SidebarContent>

        <SidebarFooter className="gap-1.5 p-3 group-data-[collapsible=icon]:px-2">
          <SidebarCollapseControl />
          <Link
            to="/"
            className="flex h-9 items-center gap-2 rounded-lg border border-sidebar-border px-2.5 text-sm font-medium text-sidebar-foreground transition-colors outline-none group-data-[collapsible=icon]:justify-center group-data-[collapsible=icon]:px-0 hover:bg-accent hover:text-foreground focus-visible:ring-2 focus-visible:ring-sidebar-ring"
          >
            <HomeIcon className="size-4 shrink-0" />
            <span className="group-data-[collapsible=icon]:sr-only">
              返回用户端
            </span>
          </Link>
        </SidebarFooter>
        <SidebarRail />
      </Sidebar>

      <SidebarInset>
        <header className="sticky top-0 z-20 flex h-(--shell-header-height) shrink-0 items-center gap-2 border-b bg-background/95 pr-3 pl-2 backdrop-blur-sm supports-backdrop-filter:bg-background/85 sm:gap-3 sm:pr-4 sm:pl-3">
          <SidebarTrigger
            aria-label="切换后台侧栏"
            className="text-muted-foreground hover:text-foreground"
          />
          <Separator
            orientation="vertical"
            className="h-6 max-md:hidden data-vertical:self-center"
          />
          <ShellBreadcrumb
            items={resolveStaffBreadcrumbs(location.pathname)}
            className="max-md:hidden"
          />
          <span className="truncate text-md font-bold tracking-title md:hidden">
            管理后台
          </span>

          <div className="ml-auto flex shrink-0 items-center gap-1.5 sm:gap-3">
            {siteInfo.data ? (
              <span
                className="hidden h-[1.625rem] items-center gap-1.5 rounded-full border border-input px-3 text-xs text-muted-foreground lg:inline-flex"
                title="最近 15 分钟内活跃的真实用户数"
              >
                <span
                  aria-hidden="true"
                  className="size-1.5 rounded-full bg-success"
                />
                <span className="font-mono tabular-nums">
                  {siteInfo.data.online_users.toLocaleString("zh-CN")}
                </span>
                在线
              </span>
            ) : null}
            <span
              className={cn(
                "hidden h-[1.625rem] items-center gap-1.5 rounded-full border px-3 text-xs sm:inline-flex",
                staffSession.data
                  ? "border-success/30 bg-success/10 text-success-foreground"
                  : "border-input text-muted-foreground"
              )}
            >
              <span
                aria-hidden="true"
                className={cn(
                  "size-1.5 rounded-full",
                  staffSession.data ? "bg-success" : "bg-faint"
                )}
              />
              {staffSession.data ? "管理员已登录" : "未登录后台"}
            </span>

            <HeaderTrafficSummary
              userId={webSession.data?.user.id}
              trafficEnabled={canReadTraffic}
              economyEnabled={canReadEconomy}
              className="hidden 2xl:flex"
            />

            {staffSession.data ? (
              <DropdownMenu>
                <DropdownMenuTrigger
                  render={
                    <Button
                      variant="ghost"
                      size="icon-sm"
                      className="relative text-muted-foreground hover:text-foreground data-popup-open:bg-accent"
                      aria-label={
                        pending.total > 0
                          ? `后台待办，共 ${pending.total} 项`
                          : "后台待办，无待处理事项"
                      }
                    />
                  }
                >
                  <BellRingIcon />
                  {pending.total > 0 ? (
                    <span className="absolute -top-0.5 -right-0.5 flex h-4 min-w-4 items-center justify-center rounded-full bg-destructive px-1 font-mono text-[0.625rem] leading-none font-semibold text-white tabular-nums ring-2 ring-background">
                      {formatPendingCount(pending.total)}
                    </span>
                  ) : null}
                </DropdownMenuTrigger>
                <DropdownMenuContent
                  align="end"
                  sideOffset={6}
                  className="w-80"
                >
                  <DropdownMenuLabel className="flex items-center justify-between gap-3 px-2 py-2 text-foreground">
                    <span>后台待办</span>
                    {pending.total > 0 ? (
                      <Badge variant="destructiveSolid">
                        {pending.total} 项
                      </Badge>
                    ) : (
                      <Badge variant="secondary">已清空</Badge>
                    )}
                  </DropdownMenuLabel>
                  <DropdownMenuSeparator />
                  {pending.items.length > 0 ? (
                    <DropdownMenuGroup>
                      {pending.items.map((item) => (
                        <DropdownMenuItem
                          key={item.id}
                          render={<Link to={item.to} prefetch="intent" />}
                          className="items-start gap-3 px-2 py-2.5"
                        >
                          <span className="flex min-w-0 flex-1 flex-col gap-0.5">
                            <span className="font-medium text-foreground">
                              {item.label}
                            </span>
                            <span className="line-clamp-1 text-xs text-muted-foreground">
                              {item.description}
                            </span>
                          </span>
                          <Badge
                            variant="destructive"
                            className="mt-0.5 shrink-0 tabular-nums"
                          >
                            {formatPendingCount(item.count)}
                          </Badge>
                        </DropdownMenuItem>
                      ))}
                    </DropdownMenuGroup>
                  ) : (
                    <p className="px-3 py-5 text-center text-sm text-muted-foreground">
                      当前没有需要处理的事项
                    </p>
                  )}
                  <DropdownMenuSeparator />
                  <DropdownMenuItem render={<Link to="/staff" />}>
                    <LayoutDashboardIcon />
                    前往待办工作台
                  </DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenu>
            ) : null}

            <DropdownMenu>
              <DropdownMenuTrigger
                render={
                  <Button
                    variant="ghost"
                    size="icon"
                    className="rounded-full p-0 data-popup-open:bg-accent"
                    aria-label="打开后台账户菜单"
                  />
                }
              >
                <UserAvatar
                  username={webSession.data?.user.username ?? "guest"}
                  displayName={webSession.data?.user.display_name ?? "访"}
                  className="size-[2.125rem]"
                />
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" sideOffset={6} className="w-56">
                <DropdownMenuGroup>
                  <DropdownMenuLabel className="text-foreground">
                    {staffSession.data
                      ? `@${staffSession.data.user.username}`
                      : "普通 Web 身份"}
                  </DropdownMenuLabel>
                  <DropdownMenuItem render={<Link to="/" />}>
                    <ExternalLinkIcon />
                    返回内容站
                  </DropdownMenuItem>
                  <DropdownMenuItem render={<Link to="/account/permissions" />}>
                    <UserRoundIcon />
                    我的权限
                  </DropdownMenuItem>
                </DropdownMenuGroup>
                {staffSession.data ? (
                  <>
                    <DropdownMenuSeparator />
                    <DropdownMenuGroup>
                      <DropdownMenuItem
                        variant="destructive"
                        disabled={logoutPending}
                        onClick={handleStaffLogout}
                      >
                        {logoutPending ? <Spinner /> : <LogOutIcon />}
                        {logoutPending
                          ? "正在退出"
                          : logoutFailed
                            ? "退出失败，请重试"
                            : accountSessionAdmin
                              ? "退出账号"
                              : "退出后台会话"}
                      </DropdownMenuItem>
                    </DropdownMenuGroup>
                  </>
                ) : null}
              </DropdownMenuContent>
            </DropdownMenu>

            <ThemeToggleButton />
          </div>
        </header>

        {children}
      </SidebarInset>
    </SidebarProvider>
  )
}

/**
 * Keep PeerGo's real setting surfaces in one predictable section while using
 * the parent/child hierarchy from PtYes. Only implemented routes are rendered:
 * future mail or payment settings must not appear here before their contracts
 * and permission checks exist.
 */
function StaffSettingsNavigationGroup({
  siteSettingsItems,
  torrentTrackerSettingsItems,
  economySettingsItems,
  enrollmentItems,
  pathname,
  badges,
}: {
  siteSettingsItems: StaffSettingsNavigationItem[]
  torrentTrackerSettingsItems: StaffSettingsNavigationItem[]
  economySettingsItems: StaffSettingsNavigationItem[]
  enrollmentItems: typeof enrollmentNavigation
  pathname: string
  badges: Record<string, number>
}) {
  const settingsCenterActive = pathname === "/staff/settings"

  return (
    <SidebarGroup className="px-3 py-0">
      <SidebarGroupLabel className="group-data-[collapsible=icon]:sr-only">
        设置
      </SidebarGroupLabel>
      <SidebarGroupContent>
        <SidebarMenu className="gap-1">
          <SidebarMenuItem>
            <SidebarMenuButton
              tooltip="设置中心"
              isActive={settingsCenterActive}
              variant="classic"
              render={<Link to="/staff/settings" prefetch="intent" />}
              className={navigationButtonClass}
            >
              <Settings2Icon />
              <span className="group-data-[collapsible=icon]:hidden">
                设置中心
              </span>
            </SidebarMenuButton>
          </SidebarMenuItem>
          <StaffSettingsNavigationSection
            label={siteSettingsGroup.label}
            icon={siteSettingsGroup.icon}
            items={siteSettingsItems}
            pathname={pathname}
            badges={badges}
          />
          <StaffSettingsNavigationSection
            label={torrentTrackerSettingsGroup.label}
            icon={torrentTrackerSettingsGroup.icon}
            items={torrentTrackerSettingsItems}
            pathname={pathname}
            badges={badges}
          />
          <StaffSettingsNavigationSection
            label={economySettingsGroup.label}
            icon={economySettingsGroup.icon}
            items={economySettingsItems}
            pathname={pathname}
            badges={badges}
          />

          {enrollmentItems.map((item) => {
            const active =
              pathname === item.to || pathname.startsWith(`${item.to}/`)
            return (
              <SidebarMenuItem key={item.to}>
                <SidebarMenuButton
                  tooltip={item.label}
                  isActive={active}
                  variant="classic"
                  render={<Link to={item.to} prefetch="intent" />}
                  className={navigationButtonClass}
                >
                  <item.icon />
                  <span className="group-data-[collapsible=icon]:hidden">
                    {item.label}
                  </span>
                </SidebarMenuButton>
              </SidebarMenuItem>
            )
          })}
        </SidebarMenu>
      </SidebarGroupContent>
    </SidebarGroup>
  )
}

function StaffSettingsNavigationSection({
  label,
  icon: Icon,
  items,
  pathname,
  badges,
}: {
  label: string
  icon: typeof ShieldCheckIcon
  items: StaffSettingsNavigationItem[]
  pathname: string
  badges: Record<string, number>
}) {
  if (items.length === 0) return null

  const active = items.some(
    (item) => pathname === item.to || pathname.startsWith(`${item.to}/`)
  )
  const pendingCount = items.reduce(
    (total, item) => total + (badges[item.to] ?? 0),
    0
  )

  return (
    <Collapsible defaultOpen={active} render={<SidebarMenuItem />}>
      <CollapsibleTrigger
        render={
          <SidebarMenuButton
            tooltip={label}
            variant="classic"
            className={navigationButtonClass}
          />
        }
      >
        <Icon />
        <span className="group-data-[collapsible=icon]:hidden">{label}</span>
        <span className="ml-auto flex items-center gap-1.5 group-data-[collapsible=icon]:hidden">
          {pendingCount > 0 ? (
            <Badge
              variant="destructiveSolid"
              className={navigationCountBadgeClass}
            >
              {formatPendingCount(pendingCount)}
            </Badge>
          ) : null}
          <ChevronRightIcon className="size-3.5! text-faint transition-transform in-data-open:rotate-90" />
        </span>
      </CollapsibleTrigger>
      <CollapsibleContent>
        <SidebarMenuSub className={navigationSubMenuClass}>
          {items.map((item) => {
            const itemActive =
              pathname === item.to || pathname.startsWith(`${item.to}/`)
            const count = badges[item.to] ?? 0
            return (
              <SidebarMenuSubItem key={item.to}>
                <SidebarMenuSubButton
                  render={<Link to={item.to} prefetch="intent" />}
                  isActive={itemActive}
                  className={subNavigationButtonClass}
                >
                  <item.icon />
                  <span>{item.label}</span>
                  {count > 0 ? (
                    <Badge
                      variant="destructiveSolid"
                      className={navigationCountBadgeClass}
                    >
                      {formatPendingCount(count)}
                    </Badge>
                  ) : null}
                </SidebarMenuSubButton>
              </SidebarMenuSubItem>
            )
          })}
        </SidebarMenuSub>
      </CollapsibleContent>
    </Collapsible>
  )
}

function StaffNavigationGroup({
  label,
  items,
  pathname,
  badges,
}: {
  label?: string
  items: Array<{
    label: string
    to: string
    icon: typeof ShieldCheckIcon
  }>
  pathname: string
  badges: Record<string, number>
}) {
  return (
    <SidebarGroup className="px-3 py-0">
      {label ? (
        <SidebarGroupLabel className="group-data-[collapsible=icon]:sr-only">
          {label}
        </SidebarGroupLabel>
      ) : null}
      <SidebarGroupContent>
        <SidebarMenu className="gap-1">
          {items.map((item) => {
            const active =
              item.to === "/staff"
                ? pathname === item.to
                : pathname === item.to || pathname.startsWith(`${item.to}/`)
            const pendingCount = badges[item.to] ?? 0
            return (
              <SidebarMenuItem key={item.to}>
                <SidebarMenuButton
                  tooltip={
                    pendingCount > 0
                      ? `${item.label}，${pendingCount} 项待处理`
                      : item.label
                  }
                  isActive={active}
                  variant="classic"
                  render={<Link to={item.to} prefetch="intent" />}
                  className={cn(
                    navigationButtonClass,
                    pendingCount > 0 && "pr-8"
                  )}
                >
                  <item.icon />
                  <span className="group-data-[collapsible=icon]:hidden">
                    {item.label}
                  </span>
                </SidebarMenuButton>
                {pendingCount > 0 ? (
                  <SidebarMenuBadge className="top-2.5! right-2 h-4 min-w-[1.125rem] rounded-full bg-destructive px-1 font-mono text-[0.625rem] font-semibold text-white">
                    {formatPendingCount(pendingCount)}
                  </SidebarMenuBadge>
                ) : null}
              </SidebarMenuItem>
            )
          })}
        </SidebarMenu>
      </SidebarGroupContent>
    </SidebarGroup>
  )
}

function formatPendingCount(count: number) {
  return count > 99 ? "99+" : count.toString()
}

const staffBreadcrumbRoot: BreadcrumbItem = { label: "管理后台", to: "/staff" }

function resolveStaffBreadcrumbs(pathname: string): BreadcrumbItem[] {
  const path = pathname.length > 1 ? pathname.replace(/\/+$/, "") : pathname
  if (path === "/staff") return [staffBreadcrumbRoot, { label: "仪表盘" }]

  const pages = [
    ...overviewNavigation,
    ...enrollmentNavigation,
    ...contentNavigation,
    ...userNavigation,
    ...operationsNavigation,
  ]
  const page = pages.find((item) => item.to === path)
  if (page) return [staffBreadcrumbRoot, { label: page.label }]

  if (path === "/staff/settings") {
    return [staffBreadcrumbRoot, { label: "设置中心" }]
  }
  const setting = staffSettingsNavigationGroups
    .flatMap((group) => group.items)
    .find((item) => path === item.to || path.startsWith(`${item.to}/`))
  if (setting) {
    return [
      staffBreadcrumbRoot,
      { label: "设置中心", to: "/staff/settings" },
      { label: setting.label },
    ]
  }

  const parent = pages.find(
    (item) => item.to !== "/staff" && path.startsWith(`${item.to}/`)
  )
  if (parent) {
    return [
      staffBreadcrumbRoot,
      { label: parent.label, to: parent.to },
      { label: "详情" },
    ]
  }
  return [staffBreadcrumbRoot]
}

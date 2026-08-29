import type { CSSProperties, ReactNode } from "react"
import { Link, useLocation, useNavigate } from "react-router"
import {
  BadgeCheckIcon,
  BellRingIcon,
  ChevronDownIcon,
  ExternalLinkIcon,
  FolderTreeIcon,
  GraduationCapIcon,
  HomeIcon,
  KeyRoundIcon,
  LayoutDashboardIcon,
  LogOutIcon,
  MegaphoneIcon,
  MessageSquareWarningIcon,
  MoonIcon,
  ShieldCheckIcon,
  Settings2Icon,
  SunIcon,
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
  SidebarSeparator,
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
import { SidebarCollapseControl } from "~/features/shell/components/sidebar-collapse-control"
import { useTheme } from "~/features/shell/model/use-theme"
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

const staffNavigationButtonClass =
  "h-11 justify-end gap-3 rounded-none px-4 group-data-[collapsible=icon]:h-11! group-data-[collapsible=icon]:w-full! group-data-[collapsible=icon]:justify-center [&_svg]:size-[18px]"

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
  const { theme, toggleTheme } = useTheme()
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
    <SidebarProvider
      defaultOpen
      style={
        {
          "--sidebar-width": "12.5rem",
          "--sidebar-width-icon": "3.75rem",
        } as CSSProperties
      }
    >
      <Sidebar collapsible="icon">
        <SidebarHeader className="h-[60px] justify-center border-b px-4">
          <Link
            to="/staff"
            className="flex w-full min-w-0 items-center justify-end text-primary outline-none group-data-[collapsible=icon]:justify-center focus-visible:ring-2 focus-visible:ring-sidebar-ring max-lg:w-[calc(100%-2.5rem)] max-lg:self-start"
          >
            <span className="font-heading text-xl font-semibold tracking-tight text-primary group-data-[collapsible=icon]:hidden">
              管理后台
            </span>
            <span className="hidden font-heading text-base font-semibold text-primary group-data-[collapsible=icon]:inline">
              {compactSiteMark.toLocaleUpperCase()}
            </span>
          </Link>
        </SidebarHeader>

        <SidebarContent className="py-2">
          <StaffNavigationGroup
            label="概览"
            items={overviewNavigation}
            pathname={location.pathname}
            badges={pending.byRoute}
          />
          {contentItems.length > 0 ? (
            <>
              <SidebarSeparator />
              <StaffNavigationGroup
                label="内容"
                items={contentItems}
                pathname={location.pathname}
                badges={pending.byRoute}
              />
            </>
          ) : null}
          {userItems.length > 0 ? (
            <>
              <SidebarSeparator />
              <StaffNavigationGroup
                label="用户"
                items={userItems}
                pathname={location.pathname}
                badges={pending.byRoute}
              />
            </>
          ) : null}
          {operationsItems.length > 0 ? (
            <>
              <SidebarSeparator />
              <StaffNavigationGroup
                label="运营监控"
                items={operationsItems}
                pathname={location.pathname}
                badges={pending.byRoute}
              />
            </>
          ) : null}
          {siteSettingsItems.length > 0 ||
          torrentTrackerSettingsItems.length > 0 ||
          economySettingsItems.length > 0 ||
          enrollmentItems.length > 0 ? (
            <>
              <SidebarSeparator />
              <StaffSettingsNavigationGroup
                siteSettingsItems={siteSettingsItems}
                torrentTrackerSettingsItems={torrentTrackerSettingsItems}
                economySettingsItems={economySettingsItems}
                enrollmentItems={enrollmentItems}
                pathname={location.pathname}
                badges={pending.byRoute}
              />
            </>
          ) : null}
        </SidebarContent>

        <SidebarFooter className="gap-0 p-0">
          <SidebarCollapseControl />
          <Separator />
          <Link
            to="/"
            className="flex h-11 items-center justify-end gap-3 px-5 text-sm text-muted-foreground transition-colors group-data-[collapsible=icon]:justify-center group-data-[collapsible=icon]:px-0 hover:bg-sidebar-accent hover:text-sidebar-accent-foreground"
          >
            <span className="group-data-[collapsible=icon]:hidden">
              返回用户端
            </span>
            <HomeIcon className="size-5" />
          </Link>
          <Separator />
          <div className="px-4 py-3 text-right text-xs text-muted-foreground/60 group-data-[collapsible=icon]:hidden">
            Powered by PeerGo
          </div>
        </SidebarFooter>
        <SidebarRail />
      </Sidebar>

      <SidebarInset>
        <header className="sticky top-0 z-20 h-[60px] shrink-0 border-b bg-background">
          <div className="flex size-full items-center gap-2 px-4 lg:px-6">
            <SidebarTrigger
              aria-label="切换后台侧栏"
              className="lg:hidden [&_svg]:size-5"
            />
            <Separator orientation="vertical" className="h-4 lg:hidden" />

            <div className="ml-auto flex items-center gap-1.5 sm:gap-2">
              {siteInfo.data ? (
                <Badge
                  variant="secondary"
                  className="hidden gap-1.5 rounded-full md:inline-flex"
                  title="最近 15 分钟内活跃的真实用户数"
                >
                  <span className="size-1.5 rounded-full bg-success" />
                  {siteInfo.data.online_users} 在线
                </Badge>
              ) : null}
              <Badge
                variant="outline"
                className={cn(
                  "hidden gap-1.5 sm:inline-flex",
                  staffSession.data
                    ? "border-success/30 bg-success/10 text-success-foreground"
                    : "text-muted-foreground"
                )}
              >
                <span
                  className={cn(
                    "size-1.5 rounded-full",
                    staffSession.data ? "bg-success" : "bg-muted-foreground/50"
                  )}
                />
                {staffSession.data ? "管理员已登录" : "未登录后台"}
              </Badge>

              <HeaderTrafficSummary
                userId={webSession.data?.user.id}
                trafficEnabled={canReadTraffic}
                economyEnabled={canReadEconomy}
              />

              {staffSession.data ? (
                <DropdownMenu>
                  <DropdownMenuTrigger
                    render={
                      <Button
                        variant="ghost"
                        size="icon"
                        className="relative rounded-full"
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
                      <span className="text-destructive-foreground absolute -top-0.5 -right-0.5 flex min-w-4 items-center justify-center rounded-full bg-destructive px-1 text-[10px] leading-4 font-semibold tabular-nums">
                        {formatPendingCount(pending.total)}
                      </span>
                    ) : null}
                  </DropdownMenuTrigger>
                  <DropdownMenuContent align="end" className="w-80">
                    <DropdownMenuLabel className="flex items-center justify-between gap-3 px-2 py-2">
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
                      size="icon-lg"
                      className="rounded-full"
                      aria-label="打开后台账户菜单"
                    />
                  }
                >
                  <UserAvatar
                    username={webSession.data?.user.username ?? "guest"}
                    displayName={webSession.data?.user.display_name ?? "访"}
                    size="sm"
                  />
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end" className="w-52">
                  <DropdownMenuGroup>
                    <DropdownMenuLabel>
                      {staffSession.data
                        ? `@${staffSession.data.user.username}`
                        : "普通 Web 身份"}
                    </DropdownMenuLabel>
                    <DropdownMenuItem render={<Link to="/" />}>
                      <ExternalLinkIcon />
                      返回内容站
                    </DropdownMenuItem>
                    <DropdownMenuItem
                      render={<Link to="/account/permissions" />}
                    >
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

              <Button
                variant="secondary"
                size="icon"
                onClick={toggleTheme}
                aria-label={
                  theme === "dark" ? "切换到浅色模式" : "切换到深色模式"
                }
              >
                {theme === "light" ? <SunIcon /> : <MoonIcon />}
              </Button>
            </div>
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
    <SidebarGroup className="p-0">
      <SidebarGroupLabel className="px-5 text-right group-data-[collapsible=icon]:sr-only">
        设置
      </SidebarGroupLabel>
      <SidebarGroupContent>
        <SidebarMenu>
          <SidebarMenuItem>
            <SidebarMenuButton
              tooltip="设置中心"
              isActive={settingsCenterActive}
              variant="classic"
              render={<Link to="/staff/settings" prefetch="intent" />}
              className={staffNavigationButtonClass}
            >
              <span className="group-data-[collapsible=icon]:hidden">
                设置中心
              </span>
              <Settings2Icon />
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
                  className={staffNavigationButtonClass}
                >
                  <span className="group-data-[collapsible=icon]:hidden">
                    {item.label}
                  </span>
                  <item.icon />
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
            className={staffNavigationButtonClass}
          />
        }
      >
        <ChevronDownIcon className="transition-transform group-data-[collapsible=icon]:hidden in-data-open:rotate-180" />
        <span className="group-data-[collapsible=icon]:hidden">{label}</span>
        <Icon />
        {pendingCount > 0 ? (
          <SidebarMenuBadge className="text-destructive-foreground right-auto left-3 bg-destructive">
            {formatPendingCount(pendingCount)}
          </SidebarMenuBadge>
        ) : null}
      </CollapsibleTrigger>
      <CollapsibleContent>
        <SidebarMenuSub className="mx-0 gap-0 border-l-0 px-0 py-0">
          {items.map((item) => {
            const itemActive =
              pathname === item.to || pathname.startsWith(`${item.to}/`)
            const count = badges[item.to] ?? 0
            return (
              <SidebarMenuSubItem key={item.to}>
                <SidebarMenuSubButton
                  render={<Link to={item.to} prefetch="intent" />}
                  isActive={itemActive}
                  className="h-9 translate-x-0 justify-end rounded-none border-r-2 border-transparent px-4 text-muted-foreground transition-colors data-active:border-primary data-active:bg-primary/10 data-active:text-primary"
                >
                  {count > 0 ? (
                    <Badge
                      variant="destructive"
                      className="mr-auto h-5 min-w-5 justify-center px-1 tabular-nums"
                    >
                      {formatPendingCount(count)}
                    </Badge>
                  ) : null}
                  <span>{item.label}</span>
                  <item.icon />
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
    <SidebarGroup className="p-0">
      {label ? (
        <SidebarGroupLabel className="px-5 text-right group-data-[collapsible=icon]:sr-only">
          {label}
        </SidebarGroupLabel>
      ) : null}
      <SidebarGroupContent>
        <SidebarMenu>
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
                  className={staffNavigationButtonClass}
                >
                  <span className="group-data-[collapsible=icon]:hidden">
                    {item.label}
                  </span>
                  <item.icon />
                </SidebarMenuButton>
                {pendingCount > 0 ? (
                  <SidebarMenuBadge className="text-destructive-foreground right-auto left-3 bg-destructive">
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

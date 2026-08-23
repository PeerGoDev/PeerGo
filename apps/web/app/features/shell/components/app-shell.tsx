import {
  useCallback,
  useState,
  type ComponentProps,
  type CSSProperties,
  type ReactNode,
} from "react"
import { Link, useLocation, useNavigate } from "react-router"
import {
  type LucideIcon,
  AwardIcon,
  BookmarkIcon,
  ChartNoAxesCombinedIcon,
  ChevronDownIcon,
  ClockAlertIcon,
  DownloadIcon,
  FileCheckIcon,
  FolderClockIcon,
  GaugeIcon,
  GraduationCapIcon,
  HomeIcon,
  LogInIcon,
  LogOutIcon,
  ListIcon,
  MailCheckIcon,
  MailIcon,
  CoinsIcon,
  MoonIcon,
  MessageCircleIcon,
  NewspaperIcon,
  PinIcon,
  SearchIcon,
  ServerIcon,
  ShoppingBagIcon,
  ShieldAlertIcon,
  SettingsIcon,
  ShieldIcon,
  SunIcon,
  TicketIcon,
  UploadIcon,
  UserIcon,
  UserPlusIcon,
  UsersIcon,
  UsersRoundIcon,
  RssIcon,
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
import { Spinner } from "~/components/ui/spinner"
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
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarMenuSub,
  SidebarMenuSubButton,
  SidebarMenuSubItem,
  SidebarProvider,
  SidebarRail,
  SidebarSeparator,
  SidebarTrigger,
  useSidebar,
} from "~/components/ui/sidebar"
import {
  useDeleteWebSession,
  useWebSession,
} from "~/features/auth/api/session.mutations"
import { useCapabilities } from "~/features/authz/api/capabilities.queries"
import { useMyNotificationSummary } from "~/features/notification/api/notifications.queries"
import { useSiteInfo } from "~/features/site/api/site.queries"
import { HeaderAttendanceButton } from "~/features/economy/components/header-attendance-button"
import { SidebarCollapseControl } from "~/features/shell/components/sidebar-collapse-control"
import { HeaderTrafficSummary } from "~/features/shell/components/header-traffic-summary"
import { useTheme } from "~/features/shell/model/use-theme"
import {
  readDesktopSidebarOpen,
  writeDesktopSidebarOpen,
} from "~/features/shell/model/sidebar-preference"
import { UserAvatar } from "~/shared/components/user-avatar"

type NavigationItem = {
  label: string
  to: string
  icon: LucideIcon
  badge?: string
}

type NavigationGroup = {
  label: string
  items: NavigationItem[]
  collapsible?: boolean
  icon?: LucideIcon
}

const appNavigationButtonClass =
  "h-10 justify-end gap-3 rounded-none px-4 group-data-[collapsible=icon]:h-10! group-data-[collapsible=icon]:w-full! group-data-[collapsible=icon]:justify-center [&_svg]:size-5"

export function AppShell({ children }: { children: ReactNode }) {
  const location = useLocation()
  const navigate = useNavigate()
  const siteInfo = useSiteInfo()
  const session = useWebSession()
  const activeSession = session.data
  const capabilities = useCapabilities(activeSession?.user.id)
  const canEnterStaff = capabilities.data?.items.some(
    (capability) => capability.action === "staff.session.create.self"
  )
  const canReadTraffic = Boolean(
    capabilities.data?.items.some(
      (capability) => capability.action === "traffic.read.self"
    )
  )
  const canReadEconomy = Boolean(
    capabilities.data?.items.some(
      (capability) => capability.action === "economy.read.self"
    )
  )
  const canReadMedals = Boolean(
    capabilities.data?.items.some(
      (capability) => capability.action === "economy.medal.read.self"
    )
  )
  const canClaimAttendance = Boolean(
    capabilities.data?.items.some(
      (capability) => capability.action === "economy.attendance.claim.self"
    )
  )
  const canReadHNR = capabilities.data?.items.some(
    (capability) => capability.action === "hnr.read.self"
  )
  const canReadRatioWatch = capabilities.data?.items.some(
    (capability) => capability.action === "ratio.assessment.read.self"
  )
  const canReadNewcomerAssessment = capabilities.data?.items.some(
    (capability) => capability.action === "newcomer.assessment.read.self"
  )
  const canReadInvitations = capabilities.data?.items.some(
    (capability) => capability.action === "invitation.read.self"
  )
  const canReadDownloadRestriction = capabilities.data?.items.some(
    (capability) => capability.action === "user.downloadrestriction.read.self"
  )
  const canReadTorrentSubmissions = capabilities.data?.items.some(
    (capability) => capability.action === "torrent.submission.read.self"
  )
  const canReadTorrentBookmarks = capabilities.data?.items.some(
    (capability) => capability.action === "torrent.bookmark.read.self"
  )
  const canReadTorrentPurchases = capabilities.data?.items.some(
    (capability) => capability.action === "torrent.purchase.read.self"
  )
  const canPurchasePromotion = capabilities.data?.items.some(
    (capability) => capability.action === "torrent.promotion.purchase.self"
  )
  const canReadNotifications = capabilities.data?.items.some(
    (capability) => capability.action === "notification.read.self"
  )
  const canReadWorkgroups = capabilities.data?.items.some(
    (capability) => capability.action === "workgroup.read.self"
  )
  const canReadSeedboxes = capabilities.data?.items.some(
    (capability) => capability.action === "tracker.seedbox.read.self"
  )
  const canReadRSS = capabilities.data?.items.some(
    (capability) => capability.action === "rss.subscription.read.self"
  )
  const notificationSummary = useMyNotificationSummary(
    activeSession?.user.id,
    canReadNotifications
  )
  const canSubmitTorrent = Boolean(
    activeSession?.user.email_verified &&
    capabilities.data?.items.some(
      (capability) => capability.action === "torrent.submit"
    )
  )
  const registrationAvailable = siteInfo.data?.registration_mode !== "closed"
  const siteName = siteInfo.data?.name ?? "PeerGo"
  const notificationBadge = unreadNotificationBadge(
    notificationSummary.data?.unread_count
  )
  const communityNavigationItems: NavigationItem[] = [
    ...(canReadInvitations
      ? [
          {
            label: "邀请",
            to: "/account/invitations",
            icon: TicketIcon,
          },
        ]
      : []),
    ...(canReadWorkgroups
      ? [
          {
            label: "工作组",
            to: "/workgroups",
            icon: UsersRoundIcon,
          },
        ]
      : []),
    ...(canReadNotifications
      ? [
          {
            label: "消息",
            to: "/notifications",
            icon: MailIcon,
            badge: notificationBadge,
          },
        ]
      : []),
  ]
  const accountDataNavigationItems: NavigationItem[] = [
    ...(canReadTraffic
      ? [
          {
            label: "流量统计",
            to: "/account/traffic",
            icon: ChartNoAxesCombinedIcon,
          },
        ]
      : []),
    ...(canReadDownloadRestriction
      ? [
          {
            label: "下载限制",
            to: "/account/download-restriction",
            icon: DownloadIcon,
          },
        ]
      : []),
    ...(canReadRatioWatch
      ? [
          {
            label: "分享率考核",
            to: "/account/ratio",
            icon: GaugeIcon,
          },
        ]
      : []),
    ...(canReadNewcomerAssessment
      ? [
          {
            label: "新人考核",
            to: "/account/assessment",
            icon: GraduationCapIcon,
          },
        ]
      : []),
    ...(canReadEconomy
      ? [
          {
            label: "等级与魔力值",
            to: "/account/economy",
            icon: CoinsIcon,
          },
        ]
      : []),
    ...(canReadMedals
      ? [
          {
            label: "勋章",
            to: "/medals",
            icon: AwardIcon,
          },
        ]
      : []),
    ...(canReadHNR
      ? [
          {
            label: "H&R",
            to: "/account/hnr",
            icon: ClockAlertIcon,
          },
        ]
      : []),
  ]
  const torrentAccountNavigationItems: NavigationItem[] = [
    ...(canReadRSS
      ? [
          {
            label: "RSS",
            to: "/account/rss",
            icon: RssIcon,
          },
        ]
      : []),
    ...(canReadSeedboxes
      ? [
          {
            label: "盒子申报",
            to: "/account/seedbox",
            icon: ServerIcon,
          },
        ]
      : []),
    ...(canReadTorrentBookmarks
      ? [
          {
            label: "我的收藏",
            to: "/account/bookmarks",
            icon: BookmarkIcon,
          },
        ]
      : []),
    ...(canReadTorrentPurchases
      ? [
          {
            label: "已购种子",
            to: "/account/purchases",
            icon: ShoppingBagIcon,
          },
        ]
      : []),
    ...(canPurchasePromotion
      ? [
          {
            label: "促销记录",
            to: "/account/promotions",
            icon: PinIcon,
          },
        ]
      : []),
    ...(canReadTorrentSubmissions
      ? [
          {
            label: "我的上传",
            to: "/account/submissions",
            icon: FolderClockIcon,
          },
        ]
      : []),
  ]
  // The account dropdown and sidebar share these capability-backed entries so
  // navigation cannot drift when a permission or route changes. The sidebar
  // presents them in compact sections; the dropdown remains a flat shortcut.
  const personalNavigationItems = [
    ...communityNavigationItems,
    ...accountDataNavigationItems,
    ...torrentAccountNavigationItems,
  ]
  const navigationGroups: NavigationGroup[] = !activeSession
    ? [
        {
          label: "账户入口",
          items: [
            { label: "登录", to: "/login", icon: LogInIcon },
            ...(registrationAvailable
              ? [{ label: "注册", to: "/register", icon: UserPlusIcon }]
              : []),
            {
              label: "封禁记录与申诉",
              to: "/restrictions",
              icon: ShieldAlertIcon,
            },
          ],
        },
      ]
    : [
        {
          label: "主要功能",
          items: [
            { label: "首页", to: "/", icon: HomeIcon },
            { label: "种子", to: "/torrents", icon: ListIcon },
            { label: "搜索", to: "/search", icon: SearchIcon },
            ...(canSubmitTorrent
              ? [{ label: "上传", to: "/upload", icon: UploadIcon }]
              : []),
          ],
        },
        {
          label: "社区",
          items: [
            {
              label: "动态圈",
              to: "/social",
              icon: MessageCircleIcon,
            },
            {
              label: "公告",
              to: "/announcements",
              icon: NewspaperIcon,
            },
            ...communityNavigationItems,
          ],
        },
        {
          label: "账户与成长",
          items: accountDataNavigationItems,
          collapsible: true,
          icon: CoinsIcon,
        },
        {
          label: "种子与订阅",
          items: torrentAccountNavigationItems,
          collapsible: true,
          icon: FolderClockIcon,
        },
        {
          label: "审核",
          items: [
            {
              label: "种子审核",
              to: "/review",
              icon: FileCheckIcon,
            },
          ],
        },
        {
          label: "账户",
          items: [
            {
              label: activeSession.user.username,
              to: `/user/${encodeURIComponent(activeSession.user.username)}`,
              icon: UserIcon,
            },
            {
              label: "账户设置",
              to: "/account",
              icon: SettingsIcon,
            },
            ...(!activeSession.user.email_verified
              ? [
                  {
                    label: "验证邮箱",
                    to: "/account/email",
                    icon: MailCheckIcon,
                  },
                ]
              : []),
            ...(canEnterStaff
              ? [
                  {
                    label: "管理后台",
                    to: "/staff",
                    icon: SettingsIcon,
                  },
                ]
              : []),
          ],
        },
      ]
  const deleteSession = useDeleteWebSession()
  const { theme, toggleTheme } = useTheme()
  const [desktopSidebarOpen, setDesktopSidebarOpen] = useState(
    readDesktopSidebarOpen
  )

  const handleDesktopSidebarOpenChange = useCallback((open: boolean) => {
    setDesktopSidebarOpen(open)
    writeDesktopSidebarOpen(open)
  }, [])

  async function handleLogout() {
    if (!activeSession) {
      return
    }
    try {
      await deleteSession.mutateAsync(activeSession.csrf_token)
      navigate("/login")
    } catch {
      // The menu keeps the user in place and exposes a retry label below.
    }
  }

  return (
    <SidebarProvider
      open={desktopSidebarOpen}
      onOpenChange={handleDesktopSidebarOpenChange}
      style={
        {
          "--sidebar-width": "12.5rem",
          "--sidebar-width-icon": "3.75rem",
        } as CSSProperties
      }
    >
      <Sidebar collapsible="icon">
        <SidebarHeader className="h-[60px] justify-center gap-0 border-b px-4 py-0">
          <Link
            to="/"
            className="flex w-full min-w-0 items-baseline justify-end gap-1 outline-none group-data-[collapsible=icon]:justify-center focus-visible:ring-2 focus-visible:ring-sidebar-ring"
          >
            <span className="font-heading text-xl font-semibold tracking-tight group-data-[collapsible=icon]:hidden">
              {siteName}
            </span>
            <Badge
              variant="outline"
              className="h-4 border-warning/40 bg-warning/10 px-1 text-[9px] text-warning-foreground uppercase group-data-[collapsible=icon]:hidden"
            >
              beta
            </Badge>
            <span className="hidden font-heading text-sm font-semibold group-data-[collapsible=icon]:inline">
              {siteName.slice(0, 2)}
            </span>
          </Link>
        </SidebarHeader>

        <SidebarContent className="py-2">
          {navigationGroups
            .filter((group) => group.items.length > 0)
            .map((group, groupIndex) => (
              <div key={group.label}>
                {groupIndex > 0 ? <SidebarSeparator /> : null}
                <AppNavigationGroup
                  group={group}
                  pathname={location.pathname}
                  hash={location.hash}
                />
              </div>
            ))}
        </SidebarContent>

        <SidebarSeparator className="mx-0" />
        <SidebarFooter className="gap-0 p-0">
          <SidebarCollapseControl />
          <Separator className="hidden group-data-[collapsible=icon]:hidden lg:block" />
          <div className="hidden p-3 text-right text-xs text-muted-foreground group-data-[collapsible=icon]:hidden lg:block">
            Powered by {siteName}
          </div>
        </SidebarFooter>
        <SidebarRail />
      </Sidebar>

      <SidebarInset>
        <header className="sticky top-0 z-20 h-[60px] shrink-0 border-b bg-background">
          <div className="mx-auto flex size-full max-w-[1248px] items-center gap-2 px-4 lg:px-6">
            <SidebarTrigger
              aria-label="切换侧栏"
              className="-ml-2 size-10 rounded-lg lg:hidden [&_svg]:size-6"
            />
            {activeSession ? (
              <div className="ml-auto flex items-center gap-1.5 sm:gap-2">
                {siteInfo.data ? (
                  <>
                    <Badge
                      variant="secondary"
                      className="hidden gap-1.5 rounded-full md:inline-flex"
                      title="最近 15 分钟内活跃的真实用户数"
                    >
                      <span className="size-1.5 rounded-full bg-success" />
                      {siteInfo.data.online_users} 在线
                    </Badge>
                  </>
                ) : (
                  <Badge variant="outline" className="hidden sm:inline-flex">
                    {siteInfo.isError ? "服务暂不可用" : "正在连接"}
                  </Badge>
                )}

                <HeaderTrafficSummary
                  userId={activeSession.user.id}
                  trafficEnabled={canReadTraffic}
                  economyEnabled={canReadEconomy}
                />

                <HeaderAttendanceButton
                  userId={activeSession.user.id}
                  csrfToken={activeSession.csrf_token}
                  enabled={canClaimAttendance}
                />

                <DropdownMenu>
                  <DropdownMenuTrigger
                    render={
                      <Button
                        variant="ghost"
                        size="icon-lg"
                        className="rounded-full"
                        aria-label="打开账户菜单"
                      />
                    }
                  >
                    <UserAvatar
                      username={activeSession.user.username}
                      displayName={activeSession.user.display_name}
                      className="ring-2 ring-border"
                    />
                  </DropdownMenuTrigger>
                  <DropdownMenuContent align="end" className="w-56">
                    {activeSession ? (
                      <>
                        <DropdownMenuGroup>
                          <DropdownMenuLabel className="flex items-center gap-3 p-3 font-normal">
                            <UserAvatar
                              username={activeSession.user.username}
                              displayName={activeSession.user.display_name}
                            />
                            <span className="flex min-w-0 flex-col gap-1">
                              <span className="truncate text-sm font-medium text-foreground">
                                {activeSession.user.display_name}
                              </span>
                              <span className="truncate text-xs text-muted-foreground">
                                @{activeSession.user.username}
                              </span>
                            </span>
                          </DropdownMenuLabel>
                        </DropdownMenuGroup>
                        <DropdownMenuSeparator />
                        <DropdownMenuGroup>
                          <DropdownMenuItem
                            render={
                              <Link
                                to={`/user/${encodeURIComponent(activeSession.user.username)}`}
                              />
                            }
                          >
                            <UserIcon />
                            个人主页
                          </DropdownMenuItem>
                          {personalNavigationItems.map((item) => (
                            <DropdownMenuItem
                              key={item.to}
                              render={<Link to={item.to} />}
                            >
                              <item.icon />
                              {item.label}
                              {item.badge ? (
                                <Badge
                                  variant="secondary"
                                  className="ml-auto min-w-5 justify-center px-1.5"
                                >
                                  {item.badge}
                                </Badge>
                              ) : null}
                            </DropdownMenuItem>
                          ))}
                          <DropdownMenuItem render={<Link to="/account" />}>
                            <SettingsIcon />
                            账户设置
                          </DropdownMenuItem>
                          {canEnterStaff ? (
                            <DropdownMenuItem render={<Link to="/staff" />}>
                              <ShieldIcon />
                              员工后台
                            </DropdownMenuItem>
                          ) : null}
                        </DropdownMenuGroup>
                        <DropdownMenuSeparator />
                        <DropdownMenuGroup>
                          <DropdownMenuItem
                            variant="destructive"
                            disabled={deleteSession.isPending}
                            onClick={handleLogout}
                          >
                            {deleteSession.isPending ? (
                              <Spinner />
                            ) : (
                              <LogOutIcon />
                            )}
                            {deleteSession.isPending
                              ? "正在退出"
                              : deleteSession.isError
                                ? "退出失败，请重试"
                                : "退出登录"}
                          </DropdownMenuItem>
                        </DropdownMenuGroup>
                      </>
                    ) : (
                      <>
                        <DropdownMenuGroup>
                          <DropdownMenuLabel>
                            {session.isError
                              ? "暂时无法确认登录状态"
                              : "尚未登录"}
                          </DropdownMenuLabel>
                        </DropdownMenuGroup>
                        <DropdownMenuSeparator />
                        <DropdownMenuGroup>
                          <DropdownMenuItem render={<Link to="/login" />}>
                            <UserIcon />
                            登录
                          </DropdownMenuItem>
                          {registrationAvailable ? (
                            <DropdownMenuItem render={<Link to="/register" />}>
                              <UserPlusIcon />
                              创建账户
                            </DropdownMenuItem>
                          ) : null}
                          <DropdownMenuItem render={<Link to="/" />}>
                            <UsersIcon />
                            内容首页
                          </DropdownMenuItem>
                        </DropdownMenuGroup>
                      </>
                    )}
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
            ) : null}
          </div>
        </header>

        {children}
      </SidebarInset>
    </SidebarProvider>
  )
}

function AppNavigationGroup({
  group,
  pathname,
  hash,
}: {
  group: NavigationGroup
  pathname: string
  hash: string
}) {
  if (group.collapsible) {
    const active = group.items.some((item) =>
      navigationItemIsActive(item.to, pathname, hash)
    )
    const GroupIcon = group.icon ?? UserIcon

    return (
      <SidebarGroup className="p-0">
        <SidebarGroupContent>
          <SidebarMenu>
            <Collapsible defaultOpen={active} render={<SidebarMenuItem />}>
              <CollapsibleTrigger
                render={
                  <SidebarMenuButton
                    tooltip={group.label}
                    variant="classic"
                    className={appNavigationButtonClass}
                  />
                }
              >
                <ChevronDownIcon className="transition-transform group-data-[collapsible=icon]:hidden in-data-open:rotate-180" />
                <span className="group-data-[collapsible=icon]:hidden">
                  {group.label}
                </span>
                <GroupIcon />
              </CollapsibleTrigger>
              <CollapsibleContent>
                <SidebarMenuSub className="mx-0 gap-0 border-l-0 px-0 py-0">
                  {group.items.map((item) => {
                    const itemActive = navigationItemIsActive(
                      item.to,
                      pathname,
                      hash
                    )
                    return (
                      <SidebarMenuSubItem key={item.to}>
                        <SidebarMenuSubButton
                          render={<SidebarNavigationLink to={item.to} />}
                          isActive={itemActive}
                          className="h-9 translate-x-0 justify-end rounded-none border-r-2 border-transparent px-4 text-muted-foreground transition-colors data-active:border-primary data-active:bg-primary/10 data-active:text-primary"
                        >
                          <span>{item.label}</span>
                          {item.badge ? (
                            <Badge
                              variant="secondary"
                              className="min-w-5 justify-center px-1.5"
                            >
                              {item.badge}
                            </Badge>
                          ) : null}
                          <item.icon />
                        </SidebarMenuSubButton>
                      </SidebarMenuSubItem>
                    )
                  })}
                </SidebarMenuSub>
              </CollapsibleContent>
            </Collapsible>
          </SidebarMenu>
        </SidebarGroupContent>
      </SidebarGroup>
    )
  }

  return (
    <SidebarGroup className="p-0">
      <SidebarGroupLabel className="px-5 text-right group-data-[collapsible=icon]:sr-only">
        {group.label}
      </SidebarGroupLabel>
      <SidebarGroupContent>
        <SidebarMenu>
          {group.items.map((item) => {
            const active = navigationItemIsActive(item.to, pathname, hash)
            return (
              <SidebarMenuItem key={item.to}>
                <SidebarMenuButton
                  tooltip={item.label}
                  isActive={active}
                  variant="classic"
                  render={<SidebarNavigationLink to={item.to} />}
                  className={appNavigationButtonClass}
                >
                  <span className="group-data-[collapsible=icon]:hidden">
                    {item.label}
                  </span>
                  {item.badge ? (
                    <Badge className="min-w-5 justify-center px-1.5 group-data-[collapsible=icon]:hidden">
                      {item.badge}
                    </Badge>
                  ) : null}
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

function SidebarNavigationLink({
  onClick,
  ...props
}: ComponentProps<typeof Link>) {
  const { isMobile, setOpenMobile } = useSidebar()

  return (
    <Link
      prefetch="intent"
      {...props}
      onClick={(event) => {
        onClick?.(event)
        if (isMobile && !event.defaultPrevented) {
          setOpenMobile(false)
        }
      }}
    />
  )
}

function navigationItemIsActive(
  target: string,
  pathname: string,
  hash: string
) {
  if (target === "/") {
    return pathname === "/" && hash === ""
  }
  if (target === "/#latest-torrents") {
    return (
      pathname.startsWith("/torrents/") ||
      (pathname === "/" && hash === "#latest-torrents")
    )
  }
  if (target === "/torrents") {
    return pathname === "/torrents" || pathname.startsWith("/torrents/")
  }
  if (target === "/announcements") {
    return (
      pathname === "/announcements" || pathname.startsWith("/announcements/")
    )
  }
  if (target === "/notifications") {
    return pathname === "/notifications"
  }
  if (target === "/social") {
    return pathname === "/social" || pathname.startsWith("/social/")
  }
  if (target === "/review") {
    return pathname === "/review" || pathname.startsWith("/review/")
  }
  if (target === "/account") {
    return (
      pathname === "/account" ||
      pathname === "/account/security" ||
      pathname === "/account/email" ||
      pathname === "/account/permissions"
    )
  }
  if (target.startsWith("/user/")) {
    return pathname === target
  }
  return pathname === target
}

function unreadNotificationBadge(value: number | undefined) {
  if (!value || value < 1) return undefined
  return value > 99 ? "99+" : value.toLocaleString("zh-CN")
}

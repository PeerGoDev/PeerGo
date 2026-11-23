import {
  useCallback,
  useState,
  type ComponentProps,
  type ReactNode,
} from "react"
import { Link, useLocation, useNavigate } from "react-router"
import {
  type LucideIcon,
  AwardIcon,
  BookmarkIcon,
  BookOpenIcon,
  ChartNoAxesCombinedIcon,
  ChevronRightIcon,
  ChevronUpIcon,
  ClockAlertIcon,
  DownloadIcon,
  ExternalLinkIcon,
  FileCheckIcon,
  FolderClockIcon,
  Gamepad2Icon,
  GaugeIcon,
  GraduationCapIcon,
  HomeIcon,
  LogInIcon,
  LogOutIcon,
  ListIcon,
  MailCheckIcon,
  MailIcon,
  CoinsIcon,
  MessageCircleIcon,
  NewspaperIcon,
  PinIcon,
  SearchIcon,
  ServerIcon,
  ShoppingBagIcon,
  ShieldAlertIcon,
  SettingsIcon,
  ShieldIcon,
  StoreIcon,
  TicketIcon,
  UploadIcon,
  UserIcon,
  UserPlusIcon,
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
  SidebarTrigger,
  useSidebar,
} from "~/components/ui/sidebar"
import {
  useDeleteWebSession,
  useWebSession,
} from "~/features/auth/api/session.mutations"
import { useCapabilities } from "~/features/authz/api/capabilities.queries"
import { useNameStyles } from "~/features/economy/api/magic-store.queries"
import { useMyNotificationSummary } from "~/features/notification/api/notifications.queries"
import { useSiteInfo } from "~/features/site/api/site.queries"
import { HeaderAttendanceButton } from "~/features/economy/components/header-attendance-button"
import { SidebarCollapseControl } from "~/features/shell/components/sidebar-collapse-control"
import { HeaderTrafficSummary } from "~/features/shell/components/header-traffic-summary"
import {
  navigationButtonClass,
  navigationCountBadgeClass,
  navigationSubMenuClass,
  subNavigationButtonClass,
} from "~/features/shell/components/navigation-styles"
import { ShellBreadcrumb } from "~/features/shell/components/shell-breadcrumb"
import { ThemeToggleButton } from "~/features/shell/components/theme-toggle-button"
import { resolveAppBreadcrumbs } from "~/features/shell/model/breadcrumbs"
import {
  readDesktopSidebarOpen,
  writeDesktopSidebarOpen,
} from "~/features/shell/model/sidebar-preference"
import { cn } from "~/lib/utils"
import { UserAvatar } from "~/shared/components/user-avatar"

type NavigationItem = {
  label: string
  to: string
  icon: LucideIcon
  badge?: string
  external?: boolean
  openInNewTab?: boolean
}

type NavigationGroup = {
  label: string
  items: NavigationItem[]
  collapsible?: boolean
  icon?: LucideIcon
}

const magicNavigationItems: NavigationItem[] = [
  { label: "魔力商店", to: "/shop", icon: StoreIcon },
  { label: "游戏中心", to: "/games", icon: Gamepad2Icon },
]

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
  useNameStyles(
    Boolean(
      capabilities.data?.items.some(
        (capability) => capability.action === "economy.namestyle.read"
      )
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
  const canReadWiki = capabilities.data?.items.some(
    (capability) => capability.action === "wiki.page.read.member"
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
  const customNavigationItems: NavigationItem[] = (
    siteInfo.data?.custom_navigation_items ?? []
  )
    .filter((item) => item.enabled)
    .map((item) => ({
      label: item.label,
      to: item.url,
      icon: ExternalLinkIcon,
      external: !item.url.startsWith("/"),
      openInNewTab: item.open_in_new_tab && !item.url.startsWith("mailto:"),
    }))
  const customNavigationGroups: NavigationGroup[] =
    customNavigationItems.length > 0
      ? [{ label: "站点链接", items: customNavigationItems }]
      : []
  const notificationBadge = unreadNotificationBadge(
    notificationSummary.data?.unread_count
  )
  const communityNavigationItems: NavigationItem[] = [
    ...(canReadWiki
      ? [
          {
            label: "Wiki",
            to: "/wiki",
            icon: BookOpenIcon,
          },
        ]
      : []),
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
  // The sidebar is the full navigation (and the phone menu); the account
  // dropdown keeps only the personal shortcuts people reach for most.
  const accountShortcutItems: NavigationItem[] = [
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
    ...(canReadTorrentSubmissions
      ? [
          {
            label: "我的上传",
            to: "/account/submissions",
            icon: FolderClockIcon,
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
        ...customNavigationGroups,
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
            ...magicNavigationItems,
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
        ...customNavigationGroups,
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

  const accountMenu = activeSession ? (
    <AccountMenuItems
      username={activeSession.user.username}
      displayName={activeSession.user.display_name}
      userId={activeSession.user.id}
      trafficEnabled={canReadTraffic}
      economyEnabled={canReadEconomy}
      shortcutItems={accountShortcutItems}
      canEnterStaff={Boolean(canEnterStaff)}
      logoutPending={deleteSession.isPending}
      logoutFailed={deleteSession.isError}
      onLogout={handleLogout}
    />
  ) : null

  return (
    <SidebarProvider
      open={desktopSidebarOpen}
      onOpenChange={handleDesktopSidebarOpenChange}
    >
      <Sidebar collapsible="icon">
        <SidebarHeader className="h-14 shrink-0 flex-row items-center gap-0 px-5 py-0 group-data-[collapsible=icon]:justify-center group-data-[collapsible=icon]:px-0 md:h-16">
          <SiteBrandLink siteName={siteName} />
        </SidebarHeader>

        <SidebarContent className="pb-2">
          {navigationGroups
            .filter((group) => group.items.length > 0)
            .map((group) => (
              <AppNavigationGroup
                key={group.label}
                group={group}
                pathname={location.pathname}
                hash={location.hash}
              />
            ))}
        </SidebarContent>

        <SidebarFooter className="gap-1.5 p-3 group-data-[collapsible=icon]:px-2">
          <SidebarCollapseControl />
          {activeSession && accountMenu ? (
            <DropdownMenu>
              <DropdownMenuTrigger
                render={
                  <button
                    type="button"
                    className="flex w-full min-w-0 items-center gap-3 rounded-lg border border-sidebar-border p-2 text-left transition-colors outline-none group-data-[collapsible=icon]:justify-center group-data-[collapsible=icon]:border-transparent group-data-[collapsible=icon]:p-1 hover:bg-accent focus-visible:ring-2 focus-visible:ring-sidebar-ring data-popup-open:bg-accent"
                  />
                }
              >
                <UserAvatar
                  username={activeSession.user.username}
                  displayName={activeSession.user.display_name}
                  shape="square"
                  className="size-10 group-data-[collapsible=icon]:size-8"
                  fallbackClassName="text-base group-data-[collapsible=icon]:text-sm"
                />
                <span className="flex min-w-0 flex-1 flex-col gap-0.5 group-data-[collapsible=icon]:hidden">
                  <span className="truncate text-sm font-semibold tracking-title text-foreground">
                    {activeSession.user.display_name}
                  </span>
                  <span className="truncate text-xs text-muted-foreground">
                    @{activeSession.user.username}
                  </span>
                </span>
                <ChevronUpIcon
                  aria-hidden="true"
                  className="size-3.5 shrink-0 text-faint group-data-[collapsible=icon]:hidden"
                />
              </DropdownMenuTrigger>
              <DropdownMenuContent
                side="top"
                align="start"
                sideOffset={6}
                className="w-(--anchor-width) min-w-60"
              >
                {accountMenu}
              </DropdownMenuContent>
            </DropdownMenu>
          ) : null}
        </SidebarFooter>
        <SidebarRail />
      </Sidebar>

      <SidebarInset>
        <header className="sticky top-0 z-20 flex h-(--shell-header-height) shrink-0 items-center gap-2 border-b bg-background/95 pr-3 pl-2 backdrop-blur-sm supports-backdrop-filter:bg-background/85 sm:gap-3 sm:pr-4 sm:pl-3">
          <SidebarTrigger
            aria-label="切换侧栏"
            className="text-muted-foreground hover:text-foreground"
          />
          <Link
            to="/"
            className="flex min-w-0 items-center gap-1.5 rounded-md text-md font-bold tracking-title outline-none focus-visible:ring-2 focus-visible:ring-ring/40 lg:hidden"
          >
            <span className="truncate">{siteName}</span>
          </Link>
          <Separator
            orientation="vertical"
            className="h-6 max-md:hidden data-vertical:self-center"
          />
          <ShellBreadcrumb
            items={resolveAppBreadcrumbs(location.pathname)}
            className="max-md:hidden"
          />

          <div className="ml-auto flex shrink-0 items-center gap-1.5 sm:gap-3">
            {siteInfo.data ? (
              <span
                className="hidden h-[1.625rem] items-center gap-1.5 rounded-full border border-input px-3 text-xs text-muted-foreground md:inline-flex"
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
            ) : (
              <span className="hidden h-[1.625rem] items-center rounded-full border border-input px-3 text-xs text-muted-foreground sm:inline-flex">
                {siteInfo.isError ? "服务暂不可用" : "正在连接"}
              </span>
            )}

            {activeSession && accountMenu ? (
              <>
                <HeaderTrafficSummary
                  userId={activeSession.user.id}
                  trafficEnabled={canReadTraffic}
                  economyEnabled={canReadEconomy}
                  className="hidden xl:flex"
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
                        size="icon"
                        className="rounded-full p-0 data-popup-open:bg-accent"
                        aria-label="打开账户菜单"
                      />
                    }
                  >
                    <UserAvatar
                      username={activeSession.user.username}
                      displayName={activeSession.user.display_name}
                      className="size-[2.125rem]"
                    />
                  </DropdownMenuTrigger>
                  <DropdownMenuContent
                    align="end"
                    sideOffset={6}
                    className="w-64"
                  >
                    {accountMenu}
                  </DropdownMenuContent>
                </DropdownMenu>
              </>
            ) : !session.isPending ? (
              <>
                <Button
                  nativeButton={false}
                  render={<Link to="/login" />}
                  size="sm"
                >
                  登录
                </Button>
                {registrationAvailable ? (
                  <Button
                    nativeButton={false}
                    render={<Link to="/register" />}
                    variant="outline"
                    size="sm"
                    className="max-sm:hidden"
                  >
                    注册
                  </Button>
                ) : null}
              </>
            ) : null}

            <ThemeToggleButton />
          </div>
        </header>

        {children}
      </SidebarInset>
    </SidebarProvider>
  )
}

function SiteBrandLink({ siteName }: { siteName: string }) {
  return (
    <Link
      to="/"
      className="flex min-w-0 items-center gap-2 rounded-md outline-none group-data-[collapsible=icon]:justify-center focus-visible:ring-2 focus-visible:ring-sidebar-ring"
    >
      <span className="truncate text-lg font-bold tracking-title text-foreground group-data-[collapsible=icon]:hidden">
        {siteName}
      </span>
      <span className="shrink-0 rounded-sm border border-primary/40 px-1 py-px text-[0.5625rem] leading-none font-semibold tracking-[0.14em] text-primary uppercase group-data-[collapsible=icon]:hidden">
        beta
      </span>
      <span
        aria-hidden="true"
        className="hidden size-9 items-center justify-center rounded-lg bg-primary sheen text-sm font-bold text-primary-foreground shadow-button group-data-[collapsible=icon]:flex"
      >
        {Array.from(siteName)[0]?.toLocaleUpperCase() ?? "P"}
      </span>
    </Link>
  )
}

function AccountMenuItems({
  username,
  displayName,
  userId,
  trafficEnabled,
  economyEnabled,
  shortcutItems,
  canEnterStaff,
  logoutPending,
  logoutFailed,
  onLogout,
}: {
  username: string
  displayName: string
  userId: string
  trafficEnabled: boolean
  economyEnabled: boolean
  shortcutItems: NavigationItem[]
  canEnterStaff: boolean
  logoutPending: boolean
  logoutFailed: boolean
  onLogout: () => void
}) {
  return (
    <>
      <DropdownMenuGroup>
        <DropdownMenuLabel className="flex flex-col gap-3 p-2 font-normal text-foreground">
          <span className="flex items-center gap-3">
            <UserAvatar
              username={username}
              displayName={displayName}
              shape="square"
              className="size-9"
            />
            <span className="flex min-w-0 flex-col gap-0.5">
              <span className="truncate text-sm font-semibold tracking-title text-foreground">
                {displayName}
              </span>
              <span className="truncate text-xs text-muted-foreground">
                @{username}
              </span>
            </span>
          </span>
          <HeaderTrafficSummary
            userId={userId}
            trafficEnabled={trafficEnabled}
            economyEnabled={economyEnabled}
            variant="menu"
            className="xl:hidden"
          />
        </DropdownMenuLabel>
      </DropdownMenuGroup>
      <DropdownMenuSeparator />
      <DropdownMenuGroup>
        <DropdownMenuItem
          render={<Link to={`/user/${encodeURIComponent(username)}`} />}
        >
          <UserIcon />
          个人主页
        </DropdownMenuItem>
        {shortcutItems.map((item) => (
          <DropdownMenuItem key={item.to} render={<Link to={item.to} />}>
            <item.icon />
            {item.label}
            {item.badge ? (
              <Badge className={navigationCountBadgeClass}>{item.badge}</Badge>
            ) : null}
          </DropdownMenuItem>
        ))}
      </DropdownMenuGroup>
      <DropdownMenuSeparator />
      <DropdownMenuGroup>
        <DropdownMenuItem render={<Link to="/account" />}>
          <SettingsIcon />
          账户设置
        </DropdownMenuItem>
        {canEnterStaff ? (
          <DropdownMenuItem render={<Link to="/staff" />}>
            <ShieldIcon />
            管理后台
          </DropdownMenuItem>
        ) : null}
      </DropdownMenuGroup>
      <DropdownMenuSeparator />
      <DropdownMenuGroup>
        <DropdownMenuItem
          variant="destructive"
          disabled={logoutPending}
          onClick={onLogout}
        >
          {logoutPending ? <Spinner /> : <LogOutIcon />}
          {logoutPending
            ? "正在退出"
            : logoutFailed
              ? "退出失败，请重试"
              : "退出登录"}
        </DropdownMenuItem>
      </DropdownMenuGroup>
    </>
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
      <SidebarGroup className="px-3 py-0">
        <SidebarGroupContent>
          <SidebarMenu className="gap-1">
            <Collapsible defaultOpen={active} render={<SidebarMenuItem />}>
              <CollapsibleTrigger
                render={
                  <SidebarMenuButton
                    tooltip={group.label}
                    variant="classic"
                    className={navigationButtonClass}
                  />
                }
              >
                <GroupIcon />
                <span className="group-data-[collapsible=icon]:hidden">
                  {group.label}
                </span>
                <ChevronRightIcon className="ml-auto size-3.5! text-faint transition-transform group-data-[collapsible=icon]:hidden in-data-open:rotate-90" />
              </CollapsibleTrigger>
              <CollapsibleContent>
                <SidebarMenuSub className={navigationSubMenuClass}>
                  {group.items.map((item) => {
                    const itemActive = navigationItemIsActive(
                      item.to,
                      pathname,
                      hash
                    )
                    return (
                      <SidebarMenuSubItem key={item.to}>
                        <SidebarMenuSubButton
                          render={
                            <SidebarNavigationLink
                              to={item.to}
                              external={item.external}
                              openInNewTab={item.openInNewTab}
                            />
                          }
                          isActive={itemActive}
                          className={subNavigationButtonClass}
                        >
                          <item.icon />
                          <span>{item.label}</span>
                          {item.badge ? (
                            <NavigationCountBadge value={item.badge} />
                          ) : null}
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
    <SidebarGroup className="px-3 py-0">
      <SidebarGroupLabel className="group-data-[collapsible=icon]:sr-only">
        {group.label}
      </SidebarGroupLabel>
      <SidebarGroupContent>
        <SidebarMenu className="gap-1">
          {group.items.map((item) => {
            const active = navigationItemIsActive(item.to, pathname, hash)
            return (
              <SidebarMenuItem key={item.to}>
                <SidebarMenuButton
                  tooltip={item.label}
                  isActive={active}
                  variant="classic"
                  render={
                    <SidebarNavigationLink
                      to={item.to}
                      external={item.external}
                      openInNewTab={item.openInNewTab}
                    />
                  }
                  className={navigationButtonClass}
                >
                  <item.icon />
                  <span className="group-data-[collapsible=icon]:sr-only">
                    {item.label}
                  </span>
                  {item.badge ? (
                    <NavigationCountBadge
                      value={item.badge}
                      className="group-data-[collapsible=icon]:hidden"
                    />
                  ) : null}
                </SidebarMenuButton>
              </SidebarMenuItem>
            )
          })}
        </SidebarMenu>
      </SidebarGroupContent>
    </SidebarGroup>
  )
}

function NavigationCountBadge({
  value,
  className,
}: {
  value: string
  className?: string
}) {
  return (
    <Badge className={cn(navigationCountBadgeClass, className)}>{value}</Badge>
  )
}

function SidebarNavigationLink({
  external = false,
  openInNewTab = false,
  to,
  onClick,
  ...props
}: ComponentProps<typeof Link> & {
  external?: boolean
  openInNewTab?: boolean
}) {
  const { isMobile, setOpenMobile } = useSidebar()

  const handleClick: ComponentProps<"a">["onClick"] = (event) => {
    onClick?.(event)
    if (isMobile && !event.defaultPrevented) {
      setOpenMobile(false)
    }
  }

  if (external && typeof to === "string") {
    return (
      <a
        {...props}
        href={to}
        target={openInNewTab ? "_blank" : undefined}
        rel={openInNewTab ? "noopener noreferrer" : undefined}
        onClick={handleClick}
      />
    )
  }

  return (
    <Link
      prefetch="intent"
      to={to}
      {...props}
      target={openInNewTab ? "_blank" : undefined}
      rel={openInNewTab ? "noopener noreferrer" : undefined}
      onClick={handleClick}
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
  if (target === "/wiki") {
    return pathname === "/wiki" || pathname.startsWith("/wiki/")
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

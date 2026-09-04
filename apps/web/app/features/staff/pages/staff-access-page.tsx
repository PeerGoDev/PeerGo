import type { ReactNode } from "react"
import { useQuery } from "@tanstack/react-query"
import { Link } from "react-router"
import {
  ArrowUpRightIcon,
  BadgeCheckIcon,
  BadgePercentIcon,
  Clock3Icon,
  ClipboardCheckIcon,
  FingerprintIcon,
  FolderTreeIcon,
  HardDriveIcon,
  MegaphoneIcon,
  MessageSquareWarningIcon,
  RefreshCwIcon,
  Settings2Icon,
  ShieldCheckIcon,
  ShoppingBagIcon,
  UserRoundIcon,
  UsersRoundIcon,
  MessageCircleMoreIcon,
  ListTodoIcon,
  CircleCheckBigIcon,
  CircleAlertIcon,
} from "lucide-react"

import { Badge } from "~/components/ui/badge"
import { Button, buttonVariants } from "~/components/ui/button"
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from "~/components/ui/card"
import { StaffAccessGate } from "~/features/staff/components/staff-access-gate"
import {
  useStaffPendingOverview,
  type StaffPendingItem,
} from "~/features/staff/api/staff-pending-overview.queries"
import { StaffPageHeader } from "~/features/staff/components/staff-page-header"
import { StaffPageFrame } from "~/features/staff/components/staff-page-frame"
import { hasCapability } from "~/features/staff/model/capability"
import { staffSettingsNavigationItems } from "~/features/staff/model/staff-settings-navigation"
import { managedUserListQueryOptions } from "~/features/staff/api/user-administration.queries"
import { torrentListQueryOptions } from "~/features/torrent/api/torrent.queries"
import { useSiteInfo } from "~/features/site/api/site.queries"
import type { StaffSession } from "~/features/staff/api/staff-session.mutations"
import type { components } from "~/generated/api"
import { cn } from "~/lib/utils"
import { formatDateTime } from "~/shared/formatters/date-time"

type CapabilityList = components["schemas"]["CapabilityList"]
type CapabilityAction = components["schemas"]["CapabilityAction"]

const dashboardUserFilters = {
  query: "",
  status: "all" as const,
  page: 1,
  pageSize: 20,
}

const quickActions: Array<{
  label: string
  to: string
  icon: typeof Settings2Icon
  action: CapabilityAction
}> = [
  {
    label: "用户管理",
    to: "/staff/users",
    icon: UsersRoundIcon,
    action: "user.account.read",
  },
  {
    label: "种子管理",
    to: "/staff/content/torrents",
    icon: HardDriveIcon,
    action: "torrent.manage.read",
  },
  {
    label: "购买记录",
    to: "/staff/content/torrent-purchases",
    icon: ShoppingBagIcon,
    action: "torrent.purchase.manage.read",
  },
  {
    label: "种子优惠",
    to: "/staff/settings/promotions",
    icon: BadgePercentIcon,
    action: "promotion.manage.read",
  },
  {
    label: "种子审核",
    to: "/staff/content/torrent-reviews",
    icon: ClipboardCheckIcon,
    action: "torrent.review",
  },
  {
    label: "公告管理",
    to: "/staff/content/announcements",
    icon: MegaphoneIcon,
    action: "announcement.manage.read",
  },
  {
    label: "评论审核",
    to: "/staff/content/comments",
    icon: MessageSquareWarningIcon,
    action: "social.report.read",
  },
  {
    label: "动态圈管理",
    to: "/staff/content/social",
    icon: MessageCircleMoreIcon,
    action: "social.board.manage.read",
  },
  {
    label: "分类管理",
    to: "/staff/content/categories",
    icon: FolderTreeIcon,
    action: "category.manage.read",
  },
]

export function StaffAccessPage() {
  return (
    <StaffAccessGate pageHeader={{ title: "仪表盘" }}>
      {({ session, capabilities }) => (
        <StaffDashboard session={session} capabilities={capabilities} />
      )}
    </StaffAccessGate>
  )
}

function StaffDashboard({
  session,
  capabilities,
}: {
  session: StaffSession
  capabilities: CapabilityList
}) {
  const siteInfo = useSiteInfo()
  const canReadUsers = hasCapability(capabilities, "user.account.read")
  const users = useQuery({
    ...managedUserListQueryOptions(dashboardUserFilters),
    enabled: canReadUsers,
  })
  const torrents = useQuery(torrentListQueryOptions({ limit: 1 }))
  const canReadGovernance = hasCapability(capabilities, "authz.grant.read")
  const canAccessSettings = staffSettingsNavigationItems.some((item) =>
    hasCapability(capabilities, item.action)
  )
  const pending = useStaffPendingOverview(capabilities)
  const availableQuickActions = quickActions.filter((item) =>
    hasCapability(capabilities, item.action)
  )
  const refreshing =
    siteInfo.isFetching ||
    users.isFetching ||
    torrents.isFetching ||
    pending.isFetching

  async function refreshDashboard() {
    await Promise.all([
      siteInfo.refetch(),
      torrents.refetch(),
      pending.refetch(),
      ...(canReadUsers ? [users.refetch()] : []),
    ])
  }

  return (
    <StaffPageFrame className="gap-6">
      <StaffPageHeader
        title="仪表盘"
        className="sm:items-center"
        actions={
          <Button
            variant="outline"
            size="sm"
            className="min-w-[76px]"
            disabled={refreshing}
            onClick={() => void refreshDashboard()}
          >
            <RefreshCwIcon
              data-icon="inline-start"
              className={refreshing ? "animate-spin" : undefined}
            />
            {refreshing ? "刷新中" : "刷新"}
          </Button>
        }
      />

      <div className="grid grid-cols-1 gap-4 md:grid-cols-4">
        <StaffSummaryCard
          title="用户数"
          value={users.data ? users.data.total.toString() : "—"}
          description={
            canReadUsers ? "当前站点账户总数" : "当前后台身份无用户目录权限"
          }
        />
        <StaffSummaryCard
          title="在线人数"
          value={siteInfo.data ? siteInfo.data.online_users.toString() : "—"}
          description="当前站点在线用户"
          accent
        />
        <StaffSummaryCard
          title="种子数"
          value={torrents.data ? torrents.data.total.toString() : "—"}
          description="当前公开种子总数"
        />
        <StaffSummaryCard
          title="可用权限"
          value={capabilities.items.length.toString()}
          description="当前管理员可用权限数"
        />
      </div>

      <StaffPendingWorkbench
        items={pending.items}
        total={pending.total}
        hasError={pending.hasError}
      />

      {canAccessSettings || availableQuickActions.length > 0 ? (
        <Card>
          <CardHeader>
            <CardTitle className="text-base">快捷操作</CardTitle>
          </CardHeader>
          <CardContent className="flex flex-wrap gap-3">
            {canAccessSettings ? (
              <Link
                to="/staff/settings"
                className={buttonVariants({
                  variant: "outline",
                  className: "min-w-[114px]",
                })}
              >
                <Settings2Icon data-icon="inline-start" />
                设置中心
              </Link>
            ) : null}
            {availableQuickActions.map((item) => (
              <Link
                key={item.to}
                to={item.to}
                className={buttonVariants({
                  variant: "outline",
                  className: "min-w-[114px]",
                })}
              >
                <item.icon data-icon="inline-start" />
                {item.label}
              </Link>
            ))}
          </CardContent>
        </Card>
      ) : null}

      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2 text-base">
            <ShieldCheckIcon className="size-[18px] text-success" />
            管理员登录
          </CardTitle>
          <CardDescription>
            管理权限来自当前账号的站点管理员角色。
          </CardDescription>
          <CardAction>
            <Badge
              variant="outline"
              className="border-success/30 bg-success/10 text-success-foreground"
            >
              有效
            </Badge>
          </CardAction>
        </CardHeader>
        <CardContent className="grid gap-x-6 gap-y-5 text-sm sm:grid-cols-2">
          <SessionFact
            icon={UserRoundIcon}
            label="当前后台身份"
            value={`@${session.user.username}`}
          />
          <SessionFact icon={BadgeCheckIcon} label="权限策略" value="已加载" />
          <SessionFact
            icon={FingerprintIcon}
            label="管理员身份确认时间"
            value={session.authenticated_at}
            dateTime
          />
          <SessionFact
            icon={Clock3Icon}
            label="当前登录有效至"
            value={session.expires_at}
            dateTime
          />
        </CardContent>
        {canReadGovernance ? (
          <CardFooter>
            <Link
              to="/staff/governance"
              className={buttonVariants({ variant: "outline" })}
            >
              <BadgeCheckIcon data-icon="inline-start" />
              打开权限与任期
            </Link>
          </CardFooter>
        ) : null}
      </Card>
    </StaffPageFrame>
  )
}

function StaffPendingWorkbench({
  items,
  total,
  hasError,
}: {
  items: StaffPendingItem[]
  total: number
  hasError: boolean
}) {
  return (
    <Card className="gap-0 py-0">
      <CardHeader className="border-b p-6">
        <CardTitle className="flex items-center gap-2 text-base">
          <ListTodoIcon className="size-[18px] text-primary" />
          待办工作台
        </CardTitle>
        <CardDescription>
          只汇总需要人工处理的队列，处理完成后徽章会自动更新。
        </CardDescription>
        <CardAction>
          <Badge variant={total > 0 ? "destructiveSolid" : "secondary"}>
            {total > 0 ? `${total} 项待处理` : "暂无待办"}
          </Badge>
        </CardAction>
      </CardHeader>
      <CardContent className="p-0">
        {items.length > 0 ? (
          <div className="grid gap-3 p-4 md:grid-cols-2 xl:grid-cols-3">
            {items.map((item) => (
              <Link
                key={item.id}
                to={item.to}
                className="group flex min-h-24 items-center gap-3 rounded-lg border bg-background p-4 transition-colors outline-none hover:bg-muted/60 focus-visible:ring-2 focus-visible:ring-ring"
              >
                <Badge
                  variant="destructive"
                  className="h-9 min-w-9 rounded-lg px-2 text-sm tabular-nums"
                >
                  {item.count > 99 ? "99+" : item.count}
                </Badge>
                <span className="flex min-w-0 flex-1 flex-col gap-1">
                  <span className="font-medium">{item.label}</span>
                  <span className="line-clamp-1 text-sm text-muted-foreground">
                    {item.description}
                  </span>
                </span>
                <ArrowUpRightIcon className="size-4 shrink-0 text-muted-foreground transition-transform group-hover:translate-x-0.5 group-hover:-translate-y-0.5" />
              </Link>
            ))}
          </div>
        ) : (
          <div className="flex min-h-28 items-center justify-center gap-3 px-6 py-8 text-sm text-muted-foreground">
            {hasError ? (
              <>
                <CircleAlertIcon className="size-5 text-destructive" />
                部分待办队列暂时无法读取，请刷新后重试
              </>
            ) : (
              <>
                <CircleCheckBigIcon className="size-5 text-success" />
                当前没有需要人工处理的事项
              </>
            )}
          </div>
        )}
      </CardContent>
    </Card>
  )
}

function StaffSummaryCard({
  title,
  value,
  description,
  accent = false,
}: {
  title: string
  value: ReactNode
  description: string
  accent?: boolean
}) {
  return (
    <Card className="h-[114px] gap-0 py-0">
      <CardHeader className="px-6 pt-6 pb-2">
        <CardTitle className="text-sm text-muted-foreground">{title}</CardTitle>
        <CardDescription className="sr-only">{description}</CardDescription>
      </CardHeader>
      <CardContent className="px-6 pt-0 pb-6">
        <p
          className={cn(
            "truncate text-3xl font-bold tabular-nums",
            accent && "text-success"
          )}
          title={typeof value === "string" ? value : undefined}
        >
          {value}
        </p>
      </CardContent>
    </Card>
  )
}

function SessionFact({
  icon: Icon,
  label,
  value,
  dateTime = false,
}: {
  icon: typeof Settings2Icon
  label: string
  value: string
  dateTime?: boolean
}) {
  return (
    <div className="flex gap-3">
      <span className="mt-0.5 text-muted-foreground [&>svg]:size-4">
        <Icon />
      </span>
      <div className="flex flex-col gap-1.5">
        <span className="font-medium">{label}</span>
        {dateTime ? (
          <time className="text-muted-foreground" dateTime={value}>
            {formatDateTime(value)}
          </time>
        ) : (
          <span className="text-muted-foreground">{value}</span>
        )}
      </div>
    </div>
  )
}

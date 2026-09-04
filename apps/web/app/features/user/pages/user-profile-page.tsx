import {
  useEffect,
  useMemo,
  useState,
  type ComponentType,
  type ReactNode,
} from "react"
import { Link, useParams } from "react-router"
import { useQuery } from "@tanstack/react-query"
import {
  ArrowDownToLineIcon,
  ArrowUpFromLineIcon,
  CalendarDaysIcon,
  CircleAlertIcon,
  GaugeIcon,
  LogInIcon,
  RefreshCwIcon,
  SettingsIcon,
  ShieldCheckIcon,
  SparklesIcon,
} from "lucide-react"

import { Badge } from "~/components/ui/badge"
import { Button, buttonVariants } from "~/components/ui/button"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "~/components/ui/card"
import { Skeleton } from "~/components/ui/skeleton"
import { useWebSession } from "~/features/auth/api/session.mutations"
import { useCapabilities } from "~/features/authz/api/capabilities.queries"
import {
  type EconomyOverview,
  useEconomyOverview,
} from "~/features/economy/api/economy.queries"
import { invitationOverviewQueryOptions } from "~/features/invitation/api/invitations.queries"
import { useSocialPosts } from "~/features/social/api/posts.queries"
import {
  managedUserDetailQueryOptions,
  managedUserListQueryOptions,
} from "~/features/staff/api/user-administration.queries"
import {
  useStaffCapabilities,
  useStaffSession,
} from "~/features/staff/api/staff-session.mutations"
import { useMyTorrentBookmarks } from "~/features/torrent/api/torrent-bookmarks.queries"
import { useMyTorrentSubmissions } from "~/features/torrent/api/torrent.queries"
import { useTrafficOverview } from "~/features/traffic/api/traffic.queries"
import { formatShareRatio } from "~/features/traffic/model/format"
import { usePublicUserProfile } from "~/features/user/api/user.queries"
import {
  managedTrackerActivityQueryOptions,
  useMyTrackerActivity,
} from "~/features/user/api/tracker-activity.queries"
import { UserPublishedTorrentsCard } from "~/features/user/components/user-published-torrents-card"
import { UserRecentPostsCard } from "~/features/user/components/user-recent-posts-card"
import { UserTorrentActivityCard } from "~/features/user/components/user-torrent-activity-card"
import { UserTrackerActivityCard } from "~/features/user/components/user-tracker-activity-card"
import { UserWorkgroupTasksCard } from "~/features/user/components/user-workgroup-tasks-card"
import { myWorkgroupTasksQueryOptions } from "~/features/workgroups/api/workgroups.queries"
import { cn } from "~/lib/utils"
import { ApiProblemError, requestErrorDescription } from "~/shared/api/problem"
import { PageLayout } from "~/shared/components/page-layout"
import { UserAvatar } from "~/shared/components/user-avatar"
import { formatBytes } from "~/shared/formatters/bytes"
import { formatCompactDateTime } from "~/shared/formatters/date-time"

export function UserProfilePage() {
  const { username = "" } = useParams()
  const [recentPostsOffset, setRecentPostsOffset] = useState(0)
  const session = useWebSession()
  const user = session.data?.user
  const profile = usePublicUserProfile(username, Boolean(user))
  const isOwnProfile = Boolean(
    user && user.username.toLocaleLowerCase() === username.toLocaleLowerCase()
  )
  const capabilities = useCapabilities(user?.id)

  const canReadTraffic = hasCapability(
    capabilities.data?.items,
    "traffic.read.self"
  )
  const canReadEconomy = hasCapability(
    capabilities.data?.items,
    "economy.read.self"
  )
  const canReadSubmissions = hasCapability(
    capabilities.data?.items,
    "torrent.submission.read.self"
  )
  const canReadBookmarks = hasCapability(
    capabilities.data?.items,
    "torrent.bookmark.read.self"
  )
  const canReadWorkgroups = hasCapability(
    capabilities.data?.items,
    "workgroup.read.self"
  )
  const canReadInvitations = hasCapability(
    capabilities.data?.items,
    "invitation.read.self"
  )
  const canStartStaffSession = hasCapability(
    capabilities.data?.items,
    "staff.session.create.self"
  )

  const traffic = useTrafficOverview(
    isOwnProfile && canReadTraffic ? user?.id : undefined
  )
  const economy = useEconomyOverview(
    isOwnProfile && canReadEconomy ? user?.id : undefined
  )
  const ownTracker = useMyTrackerActivity(
    isOwnProfile && canReadTraffic ? user?.id : undefined
  )
  const submissions = useMyTorrentSubmissions(
    isOwnProfile ? user?.id : undefined,
    canReadSubmissions
  )
  const bookmarks = useMyTorrentBookmarks(
    isOwnProfile ? user?.id : undefined,
    1,
    0,
    canReadBookmarks
  )
  const invitations = useQuery({
    ...invitationOverviewQueryOptions(user?.id, 1, 0),
    enabled: Boolean(isOwnProfile && user?.id && canReadInvitations),
  })
  const workgroupTasks = useQuery({
    ...myWorkgroupTasksQueryOptions(user?.id ?? "anonymous"),
    enabled: Boolean(isOwnProfile && user?.id && canReadWorkgroups),
  })

  const staffSession = useStaffSession(
    Boolean(user && canStartStaffSession && !isOwnProfile)
  )
  const staffCapabilities = useStaffCapabilities(staffSession.data?.user.id)
  const canReadManagedUser = hasCapability(
    staffCapabilities.data?.items,
    "user.account.read"
  )
  const managedUsers = useQuery({
    ...managedUserListQueryOptions({
      query: username,
      status: "all",
      page: 1,
      pageSize: 20,
    }),
    enabled: Boolean(
      user && profile.data && !isOwnProfile && canReadManagedUser
    ),
  })
  const managedUserId = useMemo(
    () =>
      managedUsers.data?.items.find(
        (item) =>
          item.username.toLocaleLowerCase() === username.toLocaleLowerCase()
      )?.id,
    [managedUsers.data?.items, username]
  )
  const managedUser = useQuery({
    ...managedUserDetailQueryOptions(managedUserId ?? ""),
    enabled: Boolean(managedUserId && canReadManagedUser),
  })
  const managedTracker = useQuery({
    ...managedTrackerActivityQueryOptions(managedUserId ?? ""),
    enabled: Boolean(managedUserId && canReadManagedUser),
  })

  const recentPosts = useSocialPosts("newest", 3, recentPostsOffset, {
    authorUsername: username,
    enabled: Boolean(user && profile.data),
  })

  useEffect(() => setRecentPostsOffset(0), [username])

  if (session.isPending || (user && profile.isPending))
    return <UserProfileSkeleton />
  if (session.isError) {
    return (
      <ProfileStateCard
        title="成员资料暂时无法读取"
        description={requestErrorDescription(
          session.error,
          "无法确认当前登录状态，请稍后重试。"
        )}
        action={
          <Button type="button" onClick={() => void session.refetch()}>
            <RefreshCwIcon data-icon="inline-start" />
            重试
          </Button>
        }
      />
    )
  }
  if (!user) {
    return (
      <ProfileStateCard
        title="登录后查看成员资料"
        description="成员资料只向已登录用户开放。"
        action={
          <Link to="/login" className={buttonVariants()}>
            <LogInIcon data-icon="inline-start" />
            前往登录
          </Link>
        }
      />
    )
  }
  if (profile.isError) {
    const notFound =
      profile.error instanceof ApiProblemError && profile.error.status === 404
    return (
      <ProfileStateCard
        title={notFound ? "没有找到该成员" : "成员资料暂时无法读取"}
        description={
          notFound
            ? "成员不存在、已停用，或资料当前不可访问。"
            : requestErrorDescription(
                profile.error,
                "成员资料请求未能完成，请稍后重试。"
              )
        }
        action={
          notFound ? (
            <Link to="/" className={buttonVariants()}>
              返回首页
            </Link>
          ) : (
            <Button type="button" onClick={() => void profile.refetch()}>
              <RefreshCwIcon data-icon="inline-start" />
              重试
            </Button>
          )
        }
      />
    )
  }
  if (!profile.data) {
    return (
      <ProfileStateCard
        title="成员资料暂时无法读取"
        description="资料响应不完整，请稍后重试。"
        action={
          <Button type="button" onClick={() => void profile.refetch()}>
            <RefreshCwIcon data-icon="inline-start" />
            重试
          </Button>
        }
      />
    )
  }

  const publicUser = profile.data
  const managed = managedUser.data
  const isAdminView = Boolean(!isOwnProfile && managed)
  const selfTotals = traffic.data?.totals
  const uploaded = isOwnProfile
    ? selfTotals?.credited_uploaded_bytes
    : managed?.uploaded_bytes
  const downloaded = isOwnProfile
    ? selfTotals?.charged_downloaded_bytes
    : managed?.downloaded_bytes
  const tracker = isOwnProfile ? ownTracker : managedTracker
  const trackerItems = tracker.data?.items ?? []
  const seedingCount = trackerItems.filter(
    (item) => item.seeding_connections > 0
  ).length
  const leechingCount = trackerItems.filter(
    (item) => item.leeching_connections > 0
  ).length

  return (
    <PageLayout className="gap-6">
      <header className="flex flex-col items-stretch gap-4 sm:min-h-[88px] sm:flex-row sm:items-start sm:justify-between">
        <div className="flex min-w-0 items-start gap-4">
          <UserAvatar
            username={publicUser.username}
            displayName={publicUser.display_name}
            className="mt-1 size-14! sm:mt-3 sm:size-16!"
            fallbackClassName="text-xl"
          />
          <div className="flex min-w-0 flex-col gap-2">
            <div className="flex flex-wrap items-center gap-2">
              <h1 className="truncate font-heading text-2xl font-bold">
                {publicUser.display_name}
              </h1>
              <span className="text-sm text-muted-foreground">
                @{publicUser.username}
              </span>
              <Badge variant={isAdminView ? "default" : "secondary"}>
                {isOwnProfile
                  ? "我的资料"
                  : isAdminView
                    ? "管理员视图"
                    : "公开资料"}
              </Badge>
            </div>
            <p className="text-sm text-muted-foreground">
              用户 ID {publicUser.numeric_id.toLocaleString("zh-CN")}
            </p>
          </div>
        </div>
        {isOwnProfile ? (
          <Button
            variant="outline"
            size="legacySm"
            className="w-full justify-center sm:w-auto"
            nativeButton={false}
            render={<Link to="/account" />}
          >
            <SettingsIcon data-icon="inline-start" />
            设置
          </Button>
        ) : null}
      </header>

      <section
        aria-label="用户统计"
        className="grid gap-4 md:grid-cols-2 xl:grid-cols-3 xl:gap-6"
      >
        <ProfileCard title="基本信息">
          <ProfileMetric
            icon={CalendarDaysIcon}
            label="注册时间"
            value={<JoinedAt value={publicUser.joined_at} />}
          />
          <ProfileMetric
            label="数字 ID"
            value={publicUser.numeric_id.toLocaleString("zh-CN")}
          />
          {isOwnProfile ? (
            <ProfileMetric
              label="邮箱状态"
              value={user.email_verified ? "已验证" : "未验证"}
            />
          ) : null}
          {isOwnProfile && invitations.data ? (
            <ProfileMetric
              label="可用邀请"
              value={`${invitations.data.eligibility.remaining_invites.toLocaleString("zh-CN")} 个`}
            />
          ) : null}
          {managed ? (
            <>
              <ProfileMetric
                icon={ShieldCheckIcon}
                label="邮箱"
                value={managed.email}
              />
              <ProfileMetric
                label="角色"
                value={
                  managed.role_names.length
                    ? managed.role_names.join("、")
                    : "普通用户"
                }
              />
              <ProfileMetric
                label="账户状态"
                value={managedUserStatus(managed.status)}
              />
              <ProfileMetric
                label="最后活跃"
                value={
                  managed.last_active_at
                    ? formatCompactDateTime(managed.last_active_at)
                    : "暂无"
                }
              />
            </>
          ) : null}
        </ProfileCard>

        <ProfileCard title="流量统计">
          <ProfileMetric
            icon={ArrowUpFromLineIcon}
            iconClassName="text-success-foreground"
            label="上传量"
            value={
              uploaded
                ? formatBytes(uploaded)
                : isOwnProfile || isAdminView
                  ? "—"
                  : "未公开"
            }
          />
          <ProfileMetric
            icon={ArrowDownToLineIcon}
            iconClassName="text-chart-3"
            label="下载量"
            value={
              downloaded
                ? formatBytes(downloaded)
                : isOwnProfile || isAdminView
                  ? "—"
                  : "未公开"
            }
          />
          <ProfileMetric
            icon={GaugeIcon}
            iconClassName="text-warning-foreground"
            label="分享率"
            value={
              uploaded && downloaded
                ? formatShareRatio(uploaded, downloaded)
                : isOwnProfile || isAdminView
                  ? "—"
                  : "未公开"
            }
          />
          {!isOwnProfile && !isAdminView ? (
            <p className="pt-2 text-xs text-muted-foreground">
              流量属于私密信息，仅本人和具备账户读取权限的管理员可见。
            </p>
          ) : null}
        </ProfileCard>

        <ProfileCard title="活动统计">
          <ProfileMetric
            label="公开发布"
            value={formatCount(publicUser.published_torrent_count)}
          />
          <ProfileMetric
            label="做种中"
            value={
              isOwnProfile || isAdminView ? formatCount(seedingCount) : "未公开"
            }
          />
          <ProfileMetric
            label="下载中"
            value={
              isOwnProfile || isAdminView
                ? formatCount(leechingCount)
                : "未公开"
            }
          />
          <ProfileMetric
            label="收藏种子"
            value={isOwnProfile ? formatCount(bookmarks.data?.total) : "未公开"}
          />
          {managed ? (
            <>
              <ProfileMetric
                label="等级"
                value={managed.level.toLocaleString("zh-CN")}
              />
              <ProfileMetric
                label="魔力值"
                value={formatExactInteger(managed.magic_balance)}
              />
            </>
          ) : null}
        </ProfileCard>
      </section>

      {isOwnProfile ? (
        <SeedingRewardCard
          reward={economy.data?.latest_seeding_reward}
          loading={economy.isPending}
        />
      ) : null}

      <section aria-label="最新动态">
        <UserRecentPostsCard
          username={publicUser.username}
          page={recentPosts.data}
          loading={recentPosts.isPending}
          error={recentPosts.isError}
          currentUserId={user.id}
          csrfToken={session.data?.csrf_token}
          offset={recentPostsOffset}
          pageSize={3}
          onRetry={() => void recentPosts.refetch()}
          onPageChange={setRecentPostsOffset}
        />
      </section>

      <section aria-label="已发布种子">
        <UserPublishedTorrentsCard
          items={publicUser.published_torrents}
          total={publicUser.published_torrent_count}
        />
      </section>

      {isOwnProfile && canReadSubmissions ? (
        <section aria-label="种子活动">
          <UserTorrentActivityCard
            page={submissions.data}
            loading={submissions.isPending}
          />
        </section>
      ) : null}
      {isOwnProfile && canReadWorkgroups ? (
        <section aria-label="工作组任务">
          <UserWorkgroupTasksCard
            page={workgroupTasks.data}
            loading={workgroupTasks.isPending}
          />
        </section>
      ) : null}
      {isOwnProfile || isAdminView ? (
        <section aria-label="在线 BT 活动">
          <UserTrackerActivityCard
            activity={tracker.data}
            loading={tracker.isPending}
            error={tracker.isError}
            visibility={isOwnProfile ? "self" : "admin"}
          />
        </section>
      ) : null}
    </PageLayout>
  )
}

function SeedingRewardCard({
  reward,
  loading,
}: {
  reward: EconomyOverview["latest_seeding_reward"] | undefined
  loading: boolean
}) {
  return (
    <Card className="gap-0 py-0">
      <CardHeader className="px-4 pt-4 pb-3 sm:px-6 sm:pt-6 sm:pb-4">
        <CardTitle>
          <h2 className="flex items-center gap-2">
            <SparklesIcon className="size-4 text-warning-foreground" />
            做种奖励
          </h2>
        </CardTitle>
        <CardDescription>仅自己可见的最近一次结算结果。</CardDescription>
      </CardHeader>
      <CardContent className="grid gap-3 px-4 pb-4 sm:grid-cols-2 sm:px-6 sm:pb-6 lg:grid-cols-4">
        {loading && !reward ? (
          <p className="text-sm text-muted-foreground">正在加载奖励结算…</p>
        ) : reward ? (
          <>
            <RewardMetric
              label="符合条件种子"
              value={`${reward.eligible_torrent_count.toLocaleString("zh-CN")} 个`}
            />
            <RewardMetric
              label="本次魔力奖励"
              value={formatExactInteger(reward.reward)}
            />
            <RewardMetric
              label="经验奖励"
              value={formatExactInteger(reward.experience_amount)}
            />
            <RewardMetric
              label="结算区间"
              value={`${formatCompactDateTime(reward.window_start)} — ${formatCompactDateTime(reward.window_end)}`}
            />
          </>
        ) : (
          <p className="text-sm text-muted-foreground">暂无做种奖励结算。</p>
        )}
      </CardContent>
    </Card>
  )
}

function RewardMetric({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-lg bg-muted/50 p-3">
      <p className="text-xs text-muted-foreground">{label}</p>
      <p className="mt-1 font-mono text-sm font-medium tabular-nums">{value}</p>
    </div>
  )
}

function ProfileStateCard({
  title,
  description,
  action,
}: {
  title: string
  description: string
  action: ReactNode
}) {
  return (
    <PageLayout className="items-center justify-center">
      <Card className="w-full max-w-md gap-0 py-0">
        <CardHeader className="px-6 pt-6 pb-3">
          <div className="flex items-start gap-3">
            <CircleAlertIcon className="mt-0.5 size-5 text-muted-foreground" />
            <div className="flex flex-col gap-1">
              <CardTitle>
                <h1>{title}</h1>
              </CardTitle>
              <CardDescription>{description}</CardDescription>
            </div>
          </div>
        </CardHeader>
        <CardContent className="px-6 pb-6">{action}</CardContent>
      </Card>
    </PageLayout>
  )
}

function ProfileCard({
  title,
  children,
  className,
}: {
  title: string
  children: ReactNode
  className?: string
}) {
  return (
    <Card className={cn("gap-0 py-0 md:min-h-[262px]", className)}>
      <CardHeader className="px-4 pt-4 pb-2 sm:px-6 sm:pt-6">
        <CardTitle className="leading-6">
          <h2>{title}</h2>
        </CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-2 px-4 pb-4 sm:px-6 sm:pb-6">
        {children}
      </CardContent>
    </Card>
  )
}

function ProfileMetric({
  icon: Icon,
  iconClassName,
  label,
  value,
}: {
  icon?: ComponentType<{ className?: string }>
  iconClassName?: string
  label: string
  value: ReactNode
}) {
  return (
    <div className="flex items-start justify-between gap-4 text-sm">
      <span className="flex shrink-0 items-center gap-2 text-muted-foreground">
        {Icon ? <Icon className={cn("size-3.5", iconClassName)} /> : null}
        {label}
      </span>
      <span className="min-w-0 text-right font-mono break-all tabular-nums">
        {value}
      </span>
    </div>
  )
}

function UserProfileSkeleton() {
  return (
    <PageLayout className="gap-6" aria-busy="true">
      <div className="flex items-center gap-4">
        <Skeleton className="size-16 rounded-full" />
        <div className="flex flex-col gap-2">
          <Skeleton className="h-7 w-56" />
          <Skeleton className="h-4 w-32" />
        </div>
      </div>
      <div className="grid gap-6 xl:grid-cols-3">
        {Array.from({ length: 3 }, (_, index) => (
          <Skeleton key={index} className="h-56 rounded-lg" />
        ))}
      </div>
      <Skeleton className="h-40 rounded-lg" />
      <Skeleton className="h-64 rounded-lg" />
    </PageLayout>
  )
}

function formatCount(value: number | undefined) {
  return value === undefined ? "—" : `${value.toLocaleString("zh-CN")} 个`
}
function formatExactInteger(value: string) {
  try {
    return BigInt(value).toLocaleString("zh-CN")
  } catch {
    return value
  }
}
function managedUserStatus(value: string) {
  return (
    (
      {
        active: "正常",
        banned: "已封禁",
        pending_activation: "待激活",
      } as Record<string, string>
    )[value] ?? value
  )
}

function JoinedAt({ value }: { value: string }) {
  const formatted = formatJoinedAt(value)
  if (!formatted) return "时间未知"
  return (
    <span>
      <time dateTime={value} className="whitespace-nowrap">
        {formatted.date}
      </time>
      <span className="ml-1 text-xs text-muted-foreground">
        ({formatted.duration})
      </span>
    </span>
  )
}

function formatJoinedAt(value: string) {
  const timestamp = Date.parse(value)
  if (!Number.isFinite(timestamp)) return null
  const days = Math.max(0, Math.floor((Date.now() - timestamp) / 86_400_000))
  const years = Math.floor(days / 365)
  const months = Math.floor((days % 365) / 30)
  const duration =
    [years > 0 ? `${years}年` : "", months > 0 ? `${months}月` : ""].join("") ||
    `${days}天`
  return {
    date: formatCompactDateTime(value),
    duration: `${duration}前, ${(days / 7).toFixed(1)}周`,
  }
}

function hasCapability(
  items: readonly { action: string }[] | undefined,
  action: string
) {
  return Boolean(items?.some((item) => item.action === action))
}

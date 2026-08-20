import { Link, useParams } from "react-router"
import { useEffect, useState, type ComponentType, type ReactNode } from "react"
import {
  ArrowDownToLineIcon,
  ArrowUpFromLineIcon,
  CalendarDaysIcon,
  CircleAlertIcon,
  GaugeIcon,
  LogInIcon,
  RefreshCwIcon,
  SettingsIcon,
} from "lucide-react"

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
import { useSocialPosts } from "~/features/social/api/posts.queries"
import { useMyTorrentBookmarks } from "~/features/torrent/api/torrent-bookmarks.queries"
import { useMyTorrentSubmissions } from "~/features/torrent/api/torrent.queries"
import { useTrafficOverview } from "~/features/traffic/api/traffic.queries"
import { formatShareRatio } from "~/features/traffic/model/format"
import { UserTorrentActivityCard } from "~/features/user/components/user-torrent-activity-card"
import { UserRecentPostsCard } from "~/features/user/components/user-recent-posts-card"
import { usePublicUserProfile } from "~/features/user/api/user.queries"
import { PageLayout } from "~/shared/components/page-layout"
import { UserAvatar } from "~/shared/components/user-avatar"
import { ApiProblemError, requestErrorDescription } from "~/shared/api/problem"
import { formatBytes } from "~/shared/formatters/bytes"
import { formatCompactDateTime } from "~/shared/formatters/date-time"
import { cn } from "~/lib/utils"

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
  const canReadSubmissions = hasCapability(
    capabilities.data?.items,
    "torrent.submission.read.self"
  )
  const canReadBookmarks = hasCapability(
    capabilities.data?.items,
    "torrent.bookmark.read.self"
  )
  const traffic = useTrafficOverview(
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
  const recentPosts = useSocialPosts("newest", 3, recentPostsOffset, {
    authorUsername: username,
    enabled: Boolean(user && profile.data),
  })

  useEffect(() => setRecentPostsOffset(0), [username])

  if (session.isPending || (user && profile.isPending)) {
    return <UserProfileSkeleton />
  }

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
  const totals = traffic.data?.totals

  return (
    <PageLayout className="gap-6">
      <header className="flex min-h-[116px] items-start justify-between gap-4 md:min-h-[88px]">
        <div className="flex min-w-0 items-start gap-4">
          <UserAvatar
            username={publicUser.username}
            displayName={publicUser.display_name}
            className="mt-3 size-16!"
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
            </div>
          </div>
        </div>
        {isOwnProfile ? (
          <Button
            variant="outline"
            size="legacySm"
            nativeButton={false}
            render={<Link to="/account" />}
          >
            <SettingsIcon data-icon="inline-start" />
            设置
          </Button>
        ) : null}
      </header>

      <section aria-label="用户统计" className="grid gap-6 md:grid-cols-3">
        <ProfileCard title="基本信息">
          <ProfileMetric
            icon={CalendarDaysIcon}
            label="注册时间"
            value={<JoinedAt value={publicUser.joined_at} />}
          />
        </ProfileCard>

        <ProfileCard title="流量统计">
          <ProfileMetric
            icon={ArrowUpFromLineIcon}
            iconClassName="text-success-foreground"
            label="上传量"
            value={
              isOwnProfile
                ? totals
                  ? formatBytes(totals.credited_uploaded_bytes)
                  : "—"
                : "未公开"
            }
          />
          <ProfileMetric
            icon={ArrowDownToLineIcon}
            iconClassName="text-chart-3"
            label="下载量"
            value={
              isOwnProfile
                ? totals
                  ? formatBytes(totals.charged_downloaded_bytes)
                  : "—"
                : "未公开"
            }
          />
          <ProfileMetric
            icon={GaugeIcon}
            iconClassName="text-warning-foreground"
            label="分享率"
            value={
              isOwnProfile && totals
                ? formatShareRatio(
                    totals.credited_uploaded_bytes,
                    totals.charged_downloaded_bytes
                  )
                : "—"
            }
          />
        </ProfileCard>

        <ProfileCard title="活动统计">
          <ProfileMetric
            label="上传种子"
            value={formatCount(publicUser.published_torrent_count)}
          />
          <ProfileMetric label="做种中" value="—" />
          <ProfileMetric label="下载中" value="—" />
          <ProfileMetric
            label="收藏种子"
            value={isOwnProfile ? formatCount(bookmarks.data?.total) : "—"}
          />
        </ProfileCard>
      </section>

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

      {isOwnProfile && canReadSubmissions ? (
        <section aria-label="种子活动">
          <UserTorrentActivityCard
            page={submissions.data}
            loading={submissions.isPending}
          />
        </section>
      ) : null}
    </PageLayout>
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
      <Card className="w-full max-w-md gap-0 rounded-lg border py-0 shadow-sm ring-0">
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
    <Card
      className={cn(
        "gap-0 rounded-lg border py-0 shadow-sm ring-0 md:min-h-[262px]",
        className
      )}
    >
      <CardHeader className="px-6 pt-6 pb-2">
        <CardTitle className="text-base leading-6 font-semibold">
          <h2>{title}</h2>
        </CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-2 px-6 pb-6">
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
    <div className="flex items-center justify-between text-sm">
      <span className="flex items-center gap-2 text-muted-foreground">
        {Icon ? <Icon className={cn("size-3.5", iconClassName)} /> : null}
        {label}
      </span>
      <span className="min-w-0 text-right">{value}</span>
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
      <div className="grid gap-6 md:grid-cols-3">
        {Array.from({ length: 3 }, (_, index) => (
          <Skeleton key={index} className="h-36 rounded-lg md:h-[262px]" />
        ))}
      </div>
    </PageLayout>
  )
}

function formatCount(value: number | undefined) {
  return value === undefined ? "—" : `${value.toLocaleString("zh-CN")} 个`
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

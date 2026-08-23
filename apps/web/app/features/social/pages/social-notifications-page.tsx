import * as React from "react"
import { Link, useNavigate } from "react-router"
import {
  ArrowLeftIcon,
  BellRingIcon,
  CheckCheckIcon,
  CircleAlertIcon,
  HeartIcon,
  LogInIcon,
  MessageCircleIcon,
  RefreshCwIcon,
  Repeat2Icon,
  SparklesIcon,
  UserPlusIcon,
} from "lucide-react"

import { Alert, AlertDescription, AlertTitle } from "~/components/ui/alert"
import { Badge } from "~/components/ui/badge"
import { Button, buttonVariants } from "~/components/ui/button"
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "~/components/ui/empty"
import { Skeleton } from "~/components/ui/skeleton"
import { Spinner } from "~/components/ui/spinner"
import { useWebSession } from "~/features/auth/api/session.mutations"
import { useCapabilities } from "~/features/authz/api/capabilities.queries"
import {
  type SocialNotification,
  type SocialNotificationCategory,
  useMarkAllSocialNotificationsRead,
  useMarkSocialNotificationRead,
  useSocialNotifications,
} from "~/features/social/api/social-notifications.queries"
import { formatRelativeTime } from "~/features/torrent/model/format"
import { cn } from "~/lib/utils"
import { requestErrorDescription } from "~/shared/api/problem"
import { OffsetPagination } from "~/shared/components/offset-pagination"
import { PageLayout } from "~/shared/components/page-layout"
import { UserAvatar } from "~/shared/components/user-avatar"
import { formatDateTime } from "~/shared/formatters/date-time"

const pageSize = 20

const categories: Array<{
  value: SocialNotificationCategory
  label: string
}> = [
  { value: "all", label: "全部" },
  { value: "replies", label: "评论与回复" },
  { value: "likes", label: "赞与转发" },
  { value: "follows", label: "新关注" },
]

export function SocialNotificationsPage() {
  const navigate = useNavigate()
  const [category, setCategory] =
    React.useState<SocialNotificationCategory>("all")
  const [offset, setOffset] = React.useState(0)
  const session = useWebSession()
  const capabilities = useCapabilities(session.data?.user.id)
  const actions = React.useMemo(
    () => new Set(capabilities.data?.items.map((item) => item.action) ?? []),
    [capabilities.data?.items]
  )
  const canRead = actions.has("notification.read.self")
  const canWrite = actions.has("notification.read.state.write.self")
  const notifications = useSocialNotifications(
    session.data?.user.id,
    category,
    pageSize,
    offset,
    canRead
  )
  const markRead = useMarkSocialNotificationRead(
    session.data?.user.id,
    session.data?.csrf_token
  )
  const markAllRead = useMarkAllSocialNotificationsRead(
    session.data?.user.id,
    session.data?.csrf_token
  )

  React.useEffect(() => setOffset(0), [category])

  async function openNotification(notification: SocialNotification) {
    if (canWrite && notification.read_at === null) {
      try {
        await markRead.mutateAsync(notification.id)
      } catch {
        // Reading the referenced interaction must not be blocked by a stale
        // unread-state request; the page can retry it on the next visit.
      }
    }
    navigate(socialNotificationTarget(notification))
  }

  const accessPending =
    session.isPending || Boolean(session.data && capabilities.isPending)

  return (
    <PageLayout className="max-w-[760px] gap-0 px-4! pt-6! pb-10! sm:px-6! lg:pt-8!">
      <header className="sticky top-0 z-10 -mx-4 flex h-14 items-center justify-between border-b bg-background/95 px-4 backdrop-blur sm:-mx-6 sm:px-6">
        <div className="flex min-w-0 items-center gap-3">
          <Button
            render={<Link to="/social" />}
            nativeButton={false}
            variant="ghost"
            size="icon"
            aria-label="返回动态圈"
          >
            <ArrowLeftIcon />
          </Button>
          <div className="min-w-0">
            <h1 className="font-heading text-xl font-bold">动态圈通知</h1>
            <p className="text-xs text-muted-foreground">
              互动提醒，与站内信分开
            </p>
          </div>
        </div>
        {session.data && canRead && notifications.data && canWrite ? (
          <Button
            type="button"
            variant="ghost"
            size="sm"
            disabled={
              notifications.data.unread_count === 0 || markAllRead.isPending
            }
            onClick={() => markAllRead.mutate()}
          >
            {markAllRead.isPending ? (
              <Spinner data-icon="inline-start" />
            ) : (
              <CheckCheckIcon data-icon="inline-start" />
            )}
            全部已读
          </Button>
        ) : null}
      </header>

      <nav
        className="grid grid-cols-4 border-b bg-background"
        aria-label="动态圈通知分类"
      >
        {categories.map((item) => (
          <button
            key={item.value}
            type="button"
            className={cn(
              "relative min-w-0 px-1 py-3 text-sm text-muted-foreground transition-colors hover:bg-muted/40 hover:text-foreground",
              category === item.value && "font-semibold text-foreground"
            )}
            aria-current={category === item.value ? "page" : undefined}
            onClick={() => setCategory(item.value)}
          >
            <span className="truncate">{item.label}</span>
            {category === item.value ? (
              <span className="absolute inset-x-1/4 bottom-0 h-0.5 rounded-full bg-primary" />
            ) : null}
          </button>
        ))}
      </nav>

      {accessPending ? <SocialNotificationsSkeleton /> : null}

      {session.isError || (session.data && capabilities.isError) ? (
        <NotificationError
          error={
            (session.isError ? session.error : capabilities.error) ??
            new Error("social notification access unavailable")
          }
          retry={() => {
            void session.refetch()
            void capabilities.refetch()
          }}
        />
      ) : null}

      {!session.isPending && !session.isError && !session.data ? (
        <Empty className="min-h-80 rounded-none border-0">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <LogInIcon />
            </EmptyMedia>
            <EmptyTitle>登录后查看互动通知</EmptyTitle>
            <EmptyDescription>
              这里集中显示关注、点赞、转发、评论和回复。
            </EmptyDescription>
          </EmptyHeader>
          <Link to="/login" className={buttonVariants()}>
            前往登录
          </Link>
        </Empty>
      ) : null}

      {session.data && capabilities.data && !canRead ? (
        <Empty className="min-h-80 rounded-none border-0">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <BellRingIcon />
            </EmptyMedia>
            <EmptyTitle>当前账户不能查看动态通知</EmptyTitle>
            <EmptyDescription>如有疑问，请联系站点管理人员。</EmptyDescription>
          </EmptyHeader>
        </Empty>
      ) : null}

      {session.data && canRead && notifications.isPending ? (
        <SocialNotificationsSkeleton />
      ) : null}

      {session.data && canRead && notifications.isError ? (
        <NotificationError
          error={notifications.error}
          retry={() => void notifications.refetch()}
        />
      ) : null}

      {session.data && canRead && notifications.data ? (
        <section aria-label="动态圈互动通知">
          {notifications.data.items.length === 0 ? (
            <Empty className="min-h-80 rounded-none border-0">
              <EmptyHeader>
                <EmptyMedia variant="icon">
                  <BellRingIcon />
                </EmptyMedia>
                <EmptyTitle>暂时没有这类通知</EmptyTitle>
                <EmptyDescription>
                  新的关注和内容互动会出现在这里。
                </EmptyDescription>
              </EmptyHeader>
            </Empty>
          ) : (
            <ol className="divide-y">
              {notifications.data.items.map((notification) => (
                <SocialNotificationRow
                  key={notification.id}
                  notification={notification}
                  pending={
                    markRead.isPending && markRead.variables === notification.id
                  }
                  onOpen={() => void openNotification(notification)}
                />
              ))}
            </ol>
          )}
          <OffsetPagination
            total={notifications.data.total}
            limit={notifications.data.limit}
            offset={notifications.data.offset}
            onOffsetChange={setOffset}
            ariaLabel="动态圈通知分页"
            buttonVariant="ghost"
            className="border-t py-3"
          />
        </section>
      ) : null}
    </PageLayout>
  )
}

function SocialNotificationRow({
  notification,
  pending,
  onOpen,
}: {
  notification: SocialNotification
  pending: boolean
  onOpen: () => void
}) {
  const presentation = socialNotificationPresentation(notification)
  const unread = notification.read_at === null
  return (
    <li>
      <button
        type="button"
        className={cn(
          "group relative flex w-full gap-3 px-3 py-4 text-left transition-colors hover:bg-muted/40 sm:px-4",
          unread && "bg-primary/[0.035]"
        )}
        onClick={onOpen}
      >
        <span
          className={cn(
            "mt-1 flex size-8 shrink-0 items-center justify-center rounded-full",
            presentation.iconClass
          )}
          aria-hidden="true"
        >
          {presentation.icon}
        </span>
        <UserAvatar
          username={notification.actor.username}
          displayName={notification.actor.display_name}
          online={notification.actor.online}
          className="size-10 shrink-0"
        />
        <span className="min-w-0 flex-1">
          <span className="flex flex-wrap items-center gap-x-1.5 gap-y-1">
            <span
              className={cn(
                "font-semibold",
                notification.actor.administrator
                  ? "bg-gradient-to-r from-fuchsia-500 via-primary to-amber-500 bg-clip-text text-transparent"
                  : notification.actor.vip
                    ? "text-amber-600 dark:text-amber-400"
                    : "text-foreground"
              )}
            >
              {notification.actor.display_name}
            </span>
            {notification.actor.administrator ? (
              <Badge className="h-4 border-0 bg-gradient-to-r from-fuchsia-500/15 via-primary/15 to-amber-500/15 px-1.5 text-[10px] text-fuchsia-700 shadow-none dark:text-fuchsia-300">
                管理员
              </Badge>
            ) : null}
            {notification.actor.vip ? (
              <Badge className="h-4 border border-amber-400/40 bg-amber-400/10 px-1.5 text-[10px] font-bold text-amber-700 shadow-none dark:text-amber-300">
                VIP
              </Badge>
            ) : null}
            {(notification.actor.medals ?? []).map((medal) => (
              <span
                key={medal.id}
                className="flex size-4 items-center justify-center"
                title={medal.name}
              >
                {medal.image_path ? (
                  <img
                    src={medal.image_path}
                    alt=""
                    className="max-h-full max-w-full object-contain"
                  />
                ) : (
                  <SparklesIcon className="size-3.5 text-amber-500" />
                )}
              </span>
            ))}
            <span className="text-sm text-muted-foreground">
              {presentation.action}
            </span>
          </span>
          {presentation.primaryPreview ? (
            <span className="mt-1.5 line-clamp-2 block text-sm leading-5 text-foreground/90">
              “{presentation.primaryPreview}”
            </span>
          ) : null}
          {presentation.secondaryPreview ? (
            <span className="mt-1 line-clamp-1 block text-xs text-muted-foreground">
              原动态：{presentation.secondaryPreview}
            </span>
          ) : null}
          <time
            dateTime={notification.created_at}
            title={formatDateTime(notification.created_at)}
            className="mt-1.5 block text-xs text-muted-foreground"
          >
            {formatRelativeTime(notification.created_at)}
          </time>
        </span>
        {pending ? (
          <Spinner className="mt-2 shrink-0" />
        ) : unread ? (
          <span
            className="mt-3 size-2 shrink-0 rounded-full bg-primary"
            aria-label="未读"
          />
        ) : null}
      </button>
    </li>
  )
}

function socialNotificationPresentation(notification: SocialNotification) {
  switch (notification.kind) {
    case "follow":
      return {
        action: "关注了你",
        icon: <UserPlusIcon className="size-4" />,
        iconClass: "bg-sky-500/10 text-sky-600 dark:text-sky-400",
        primaryPreview: undefined,
        secondaryPreview: undefined,
      }
    case "post_like":
      return {
        action: "赞了你的动态",
        icon: <HeartIcon className="size-4 fill-current" />,
        iconClass: "bg-rose-500/10 text-rose-600 dark:text-rose-400",
        primaryPreview: notification.post_preview,
        secondaryPreview: undefined,
      }
    case "post_repost":
      return {
        action: "转发了你的动态",
        icon: <Repeat2Icon className="size-4" />,
        iconClass: "bg-emerald-500/10 text-emerald-600 dark:text-emerald-400",
        primaryPreview: notification.post_preview,
        secondaryPreview: undefined,
      }
    case "post_comment":
      return {
        action: "评论了你的动态",
        icon: <MessageCircleIcon className="size-4" />,
        iconClass: "bg-violet-500/10 text-violet-600 dark:text-violet-400",
        primaryPreview: notification.comment_preview,
        secondaryPreview: notification.post_preview,
      }
    case "comment_reply":
      return {
        action: "回复了你的评论",
        icon: <MessageCircleIcon className="size-4" />,
        iconClass: "bg-amber-500/10 text-amber-600 dark:text-amber-400",
        primaryPreview: notification.comment_preview,
        secondaryPreview: notification.post_preview,
      }
  }
}

function socialNotificationTarget(notification: SocialNotification) {
  if (notification.kind === "follow") {
    return `/user/${encodeURIComponent(notification.actor.username)}`
  }
  if (!notification.post_id) return "/social"
  return `/social/post/${notification.post_id}${
    notification.comment_id ? `#comment-${notification.comment_id}` : ""
  }`
}

function SocialNotificationsSkeleton() {
  return (
    <div className="divide-y" aria-label="正在加载动态圈通知">
      {Array.from({ length: 5 }, (_, index) => (
        <div key={index} className="flex gap-3 px-4 py-4">
          <Skeleton className="size-8 rounded-full" />
          <Skeleton className="size-10 rounded-full" />
          <div className="flex flex-1 flex-col gap-2">
            <Skeleton className="h-4 w-48" />
            <Skeleton className="h-4 w-4/5" />
            <Skeleton className="h-3 w-20" />
          </div>
        </div>
      ))}
    </div>
  )
}

function NotificationError({
  error,
  retry,
}: {
  error: Error
  retry: () => void
}) {
  return (
    <Alert variant="destructive" className="my-6">
      <CircleAlertIcon />
      <AlertTitle>动态圈通知暂时无法读取</AlertTitle>
      <AlertDescription>
        {requestErrorDescription(error, "通知请求未能完成，请稍后重试。")}
      </AlertDescription>
      <Button type="button" variant="outline" size="sm" onClick={retry}>
        <RefreshCwIcon data-icon="inline-start" />
        重试
      </Button>
    </Alert>
  )
}

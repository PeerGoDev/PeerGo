import * as React from "react"
import { Link, useSearchParams } from "react-router"
import {
  CheckIcon,
  CircleAlertIcon,
  LogInIcon,
  MessageSquarePlusIcon,
  RefreshCwIcon,
  SendIcon,
  ShieldXIcon,
  Trash2Icon,
} from "lucide-react"

import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogMedia,
  AlertDialogTitle,
} from "~/components/ui/alert-dialog"
import {
  Alert,
  AlertAction,
  AlertDescription,
  AlertTitle,
} from "~/components/ui/alert"
import { Badge } from "~/components/ui/badge"
import { Button, buttonVariants } from "~/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "~/components/ui/card"
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "~/components/ui/dialog"
import {
  Field,
  FieldError,
  FieldGroup,
  FieldLabel,
} from "~/components/ui/field"
import { Input } from "~/components/ui/input"
import { Textarea } from "~/components/ui/textarea"
import {
  Empty,
  EmptyContent,
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
  useArchiveAllMyNotifications,
  useCreateNotificationFeedback,
  useMarkAllMyNotificationsRead,
  useMarkMyNotificationRead,
  useMyNotificationPage,
  type MyNotificationPage,
} from "~/features/notification/api/notifications.queries"
import { formatRatioBasisPoints } from "~/features/traffic/model/ratio-watch-format"
import {
  contributionMetricLabel,
  formatContributionValue,
} from "~/features/workgroups/model/contribution-format"
import { cn } from "~/lib/utils"
import { requestErrorDescription } from "~/shared/api/problem"
import { OffsetPagination } from "~/shared/components/offset-pagination"
import { PageLayout } from "~/shared/components/page-layout"
import { formatDateTime } from "~/shared/formatters/date-time"
import { formatInteger } from "~/shared/formatters/integer"

const notificationPageSize = 20
const maximumNotificationPage = Math.floor(99_999 / notificationPageSize) + 1
type MyNotification = MyNotificationPage["items"][number]

export function NotificationPage() {
  const [searchParams, setSearchParams] = useSearchParams()
  const [selectedNotificationID, setSelectedNotificationID] = React.useState<
    string | null
  >(null)
  const [feedbackOpen, setFeedbackOpen] = React.useState(false)
  const [feedbackTitle, setFeedbackTitle] = React.useState("")
  const [feedbackContent, setFeedbackContent] = React.useState("")
  const [feedbackError, setFeedbackError] = React.useState<string>()
  const [feedbackSent, setFeedbackSent] = React.useState(false)
  const [archiveConfirmOpen, setArchiveConfirmOpen] = React.useState(false)
  const pageNumber = notificationPageNumber(searchParams.get("page"))
  const unreadOnly = searchParams.get("unread") === "1"
  const offset = (pageNumber - 1) * notificationPageSize
  const session = useWebSession()
  const capabilities = useCapabilities(session.data?.user.id)
  const canRead = Boolean(
    capabilities.data?.items.some(
      (capability) => capability.action === "notification.read.self"
    )
  )
  const canWrite = Boolean(
    capabilities.data?.items.some(
      (capability) => capability.action === "notification.read.state.write.self"
    )
  )
  const canArchive = Boolean(
    capabilities.data?.items.some(
      (capability) => capability.action === "notification.archive.self"
    )
  )
  const canCreateFeedback = Boolean(
    capabilities.data?.items.some(
      (capability) => capability.action === "notification.feedback.create.self"
    )
  )
  const notifications = useMyNotificationPage(
    session.data?.user.id,
    notificationPageSize,
    offset,
    unreadOnly,
    canRead
  )
  const markRead = useMarkMyNotificationRead(
    session.data?.user.id,
    session.data?.csrf_token
  )
  const markAllRead = useMarkAllMyNotificationsRead(
    session.data?.user.id,
    session.data?.csrf_token
  )
  const archiveAll = useArchiveAllMyNotifications(
    session.data?.user.id,
    session.data?.csrf_token
  )
  const createFeedback = useCreateNotificationFeedback(
    session.data?.user.id,
    session.data?.csrf_token
  )

  function submitFeedback(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const title = feedbackTitle.trim()
    const content = feedbackContent.trim()
    if (!title || title.length > 100 || !content || content.length > 2000) {
      setFeedbackError("标题需为 1 到 100 个字符，内容需为 1 到 2000 个字符。")
      return
    }
    setFeedbackError(undefined)
    createFeedback.mutate(
      { title, content },
      {
        onSuccess: () => {
          setFeedbackTitle("")
          setFeedbackContent("")
          setFeedbackSent(true)
          setFeedbackOpen(false)
        },
      }
    )
  }

  React.useEffect(() => {
    if (
      !notifications.data ||
      offset === 0 ||
      offset < notifications.data.total
    ) {
      return
    }
    setNotificationOffset(
      Math.max(
        0,
        Math.floor(
          (Math.max(notifications.data.total, 1) - 1) / notificationPageSize
        ) * notificationPageSize
      ),
      true
    )
  }, [notifications.data, offset])

  function setNotificationOffset(nextOffset: number, replace = false) {
    const next = new URLSearchParams(searchParams)
    const nextPage = Math.floor(nextOffset / notificationPageSize) + 1
    if (nextPage <= 1) next.delete("page")
    else next.set("page", String(nextPage))
    setSelectedNotificationID(null)
    setSearchParams(next, { replace, preventScrollReset: true })
  }

  function setUnreadOnly(nextUnreadOnly: boolean) {
    const next = new URLSearchParams(searchParams)
    next.delete("page")
    if (nextUnreadOnly) next.set("unread", "1")
    else next.delete("unread")
    setSelectedNotificationID(null)
    setSearchParams(next, { preventScrollReset: true })
  }

  const accessPending =
    session.isPending || Boolean(session.data && capabilities.isPending)
  const selectedNotification =
    notifications.data?.items.find(
      (notification) => notification.id === selectedNotificationID
    ) ?? null

  function selectNotification(notification: MyNotification) {
    setSelectedNotificationID(notification.id)
    if (canWrite && notification.read_at === null && !markRead.isPending) {
      markRead.mutate(notification.id)
    }
  }

  return (
    <PageLayout className="gap-6 px-8! pt-12! pb-8! md:max-w-3xl lg:max-w-5xl lg:px-10! lg:pt-14! xl:max-w-7xl">
      <header className="flex items-center justify-between gap-4 max-md:flex-col max-md:items-stretch">
        <h1 className="font-heading text-3xl font-bold">站内消息</h1>
        {session.data && canRead && notifications.data ? (
          <div className="grid min-w-0 grid-cols-2 gap-2 md:flex">
            {canCreateFeedback ? (
              <Button
                type="button"
                size="legacy"
                className="h-10 min-w-0 border-0 whitespace-nowrap md:shrink-0"
                onClick={() => {
                  setFeedbackError(undefined)
                  setFeedbackOpen(true)
                }}
              >
                <MessageSquarePlusIcon data-icon="inline-start" />
                联系管理员
              </Button>
            ) : null}
            <Button
              type="button"
              variant="soft"
              size="legacy"
              className="h-10 min-w-0 border-0 whitespace-nowrap md:shrink-0"
              onClick={() => setUnreadOnly(!unreadOnly)}
            >
              {unreadOnly ? "显示全部" : "仅未读"}
            </Button>
            {canWrite ? (
              <Button
                type="button"
                variant="soft"
                size="legacy"
                className="h-10 min-w-0 border-0 whitespace-nowrap md:shrink-0"
                disabled={
                  markAllRead.isPending || notifications.data.unread_count === 0
                }
                onClick={() => markAllRead.mutate()}
              >
                {markAllRead.isPending ? (
                  <Spinner data-icon="inline-start" />
                ) : null}
                全部标记为已读
              </Button>
            ) : null}
            {canArchive ? (
              <Button
                type="button"
                variant="softDestructive"
                size="legacy"
                className="h-10 min-w-0 border-0 whitespace-nowrap md:shrink-0"
                onClick={() => setArchiveConfirmOpen(true)}
              >
                删除全部
              </Button>
            ) : null}
          </div>
        ) : null}
      </header>

      {accessPending ? <NotificationPageSkeleton /> : null}

      {session.isError || (session.data && capabilities.isError) ? (
        <NotificationAlert
          title="消息暂时无法查看"
          description={requestErrorDescription(
            session.isError ? session.error : capabilities.error,
            "当前未能确认登录状态或消息权限，请稍后重试。"
          )}
          retry={() => {
            void session.refetch()
            void capabilities.refetch()
          }}
        />
      ) : null}

      {!session.isPending && !session.isError && !session.data ? (
        <NotificationAccessCard
          icon={<LogInIcon />}
          title="登录后查看消息"
          description="站内消息只对本人可见。"
          action={
            <Link to="/login" className={buttonVariants()}>
              <LogInIcon data-icon="inline-start" />
              前往登录
            </Link>
          }
        />
      ) : null}

      {session.data && capabilities.data && !canRead ? (
        <NotificationAccessCard
          icon={<ShieldXIcon />}
          title="当前账户不能查看消息"
          description="如有疑问，请联系站点管理人员。"
          action={
            <Link to="/" className={buttonVariants({ variant: "outline" })}>
              返回首页
            </Link>
          }
        />
      ) : null}

      {session.data && canRead && notifications.isPending ? (
        <NotificationPageSkeleton />
      ) : null}

      {session.data && canRead && notifications.isError ? (
        <NotificationAlert
          title="消息暂时无法查看"
          description={requestErrorDescription(
            notifications.error,
            "消息请求未能完成，请稍后重试。"
          )}
          retry={() => void notifications.refetch()}
        />
      ) : null}

      {session.data && canRead && notifications.data ? (
        <section
          aria-label="站内消息"
          className="grid grid-cols-1 gap-6 md:grid-cols-3"
        >
          <Card className="gap-0 py-0 md:col-span-1 md:min-h-[640px]">
            <CardHeader className="border-b p-4">
              <CardTitle className="text-base leading-6 font-semibold">
                <h2 id="notification-list-title">消息列表</h2>
              </CardTitle>
            </CardHeader>
            <CardContent className="min-h-0 flex-1 px-0">
              {notifications.data.items.length === 0 ? (
                <Empty className="flex-none gap-0 rounded-none border-0 p-4 text-muted-foreground">
                  <p>暂无消息</p>
                </Empty>
              ) : (
                <ol className="max-h-[600px] divide-y overflow-y-auto">
                  {notifications.data.items.map((notification) => {
                    const unread = notification.read_at === null
                    return (
                      <li key={notification.id}>
                        <button
                          type="button"
                          className={cn(
                            "w-full cursor-pointer p-4 text-left transition-colors hover:bg-muted/50",
                            unread && "bg-accent/50",
                            selectedNotificationID === notification.id &&
                              "bg-muted"
                          )}
                          onClick={() => selectNotification(notification)}
                        >
                          <span className="mb-1.5 flex items-start justify-between gap-2">
                            <span className="text-sm font-semibold">
                              系统消息
                            </span>
                            {unread ? (
                              <Badge aria-label="未读">未读</Badge>
                            ) : null}
                          </span>
                          <span className="mb-1.5 block truncate text-sm font-medium">
                            {notificationListTitle(notification)}
                          </span>
                          <time
                            className="block text-xs text-muted-foreground"
                            dateTime={notification.created_at}
                          >
                            {formatTimeAgo(notification.created_at)}
                          </time>
                        </button>
                      </li>
                    )
                  })}
                </ol>
              )}
            </CardContent>

            {notifications.data.total > notifications.data.limit ? (
              <div className="border-t p-4">
                <OffsetPagination
                  total={notifications.data.total}
                  limit={notifications.data.limit}
                  offset={notifications.data.offset}
                  onOffsetChange={setNotificationOffset}
                  ariaLabel="消息分页"
                  summaryLabel="消息"
                  summaryUnit="条"
                  buttonVariant="ghost"
                  className="justify-between"
                />
              </div>
            ) : null}
          </Card>

          <Card className="min-w-0 gap-0 py-0 md:col-span-2 md:min-h-[640px]">
            <CardContent className="p-6">
              {selectedNotification ? (
                <NotificationDetail notification={selectedNotification} />
              ) : (
                <Empty className="flex-none gap-0 rounded-none border-0 p-0 text-muted-foreground">
                  <p>请选择一条消息查看详情</p>
                </Empty>
              )}
            </CardContent>
          </Card>
        </section>
      ) : null}

      <Dialog
        open={feedbackOpen}
        onOpenChange={(open) => {
          setFeedbackOpen(open)
          if (open) setFeedbackError(undefined)
        }}
      >
        <DialogContent
          overlayClassName="bg-black/80 supports-backdrop-filter:backdrop-blur-none"
          className="gap-4 rounded-lg border bg-background p-6 sm:max-w-lg"
        >
          <form className="contents" onSubmit={submitFeedback}>
            <DialogHeader className="gap-1.5">
              <DialogTitle className="flex items-center gap-2 text-lg leading-none font-semibold">
                <MessageSquarePlusIcon className="size-5" />
                联系管理员
              </DialogTitle>
              <DialogDescription>
                有问题或建议？请填写以下表单，管理员会尽快回复您。
              </DialogDescription>
            </DialogHeader>
            <FieldGroup className="pt-5 pb-4">
              <Field data-invalid={Boolean(feedbackError)}>
                <FieldLabel htmlFor="notification-feedback-title">
                  标题
                </FieldLabel>
                <Input
                  id="notification-feedback-title"
                  value={feedbackTitle}
                  onChange={(event) => setFeedbackTitle(event.target.value)}
                  placeholder="请输入标题"
                  maxLength={100}
                  aria-invalid={Boolean(feedbackError)}
                />
              </Field>
              <Field data-invalid={Boolean(feedbackError)}>
                <FieldLabel htmlFor="notification-feedback-content">
                  内容
                </FieldLabel>
                <Textarea
                  id="notification-feedback-content"
                  value={feedbackContent}
                  onChange={(event) => setFeedbackContent(event.target.value)}
                  placeholder="请详细描述您的问题或建议..."
                  maxLength={2000}
                  rows={6}
                  aria-invalid={Boolean(feedbackError)}
                  className="min-h-[162px] resize-none text-base leading-6"
                />
                <span className="mt-1.5 text-right text-xs text-muted-foreground">
                  {feedbackContent.length}/2000
                </span>
                {feedbackError ? (
                  <FieldError>{feedbackError}</FieldError>
                ) : null}
              </Field>
            </FieldGroup>
            <DialogFooter className="m-0 border-0 bg-transparent p-0">
              <DialogClose render={<Button type="button" variant="outline" />}>
                取消
              </DialogClose>
              <Button
                type="submit"
                className="min-w-[84px]"
                disabled={
                  createFeedback.isPending ||
                  !feedbackTitle.trim() ||
                  !feedbackContent.trim()
                }
              >
                {createFeedback.isPending ? (
                  <Spinner data-icon="inline-start" />
                ) : (
                  <SendIcon data-icon="inline-start" />
                )}
                发送
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      <AlertDialog
        open={archiveConfirmOpen}
        onOpenChange={setArchiveConfirmOpen}
      >
        <AlertDialogContent size="sm">
          <AlertDialogHeader>
            <AlertDialogMedia className="bg-destructive/10 text-destructive">
              <Trash2Icon />
            </AlertDialogMedia>
            <AlertDialogTitle>清空全部消息？</AlertDialogTitle>
            <AlertDialogDescription>
              消息会从你的收件箱归档，相关审核记录仍会保留。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel variant="outline">取消</AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              onClick={() => {
                setSelectedNotificationID(null)
                archiveAll.mutate()
              }}
            >
              确认清空
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      {markRead.isError || markAllRead.isError || archiveAll.isError ? (
        <Alert variant="destructive">
          <CircleAlertIcon />
          <AlertTitle>消息状态更新失败</AlertTitle>
          <AlertDescription>请刷新页面后重试。</AlertDescription>
        </Alert>
      ) : null}

      {feedbackSent ? (
        <Alert>
          <CheckIcon />
          <AlertTitle>反馈已发送</AlertTitle>
          <AlertDescription>反馈已进入处理队列。</AlertDescription>
        </Alert>
      ) : null}

      {createFeedback.isError ? (
        <Alert variant="destructive">
          <CircleAlertIcon />
          <AlertTitle>反馈发送失败</AlertTitle>
          <AlertDescription>
            {requestErrorDescription(
              createFeedback.error,
              "反馈请求未能完成，请稍后重试。"
            )}
          </AlertDescription>
        </Alert>
      ) : null}
    </PageLayout>
  )
}

function NotificationDetail({
  notification,
}: {
  notification: MyNotification
}) {
  if (notification.kind === "content_tip") {
    return <ContentTipNotificationDetail notification={notification} />
  }
  if (notification.kind === "member_gift") {
    return <MemberGiftNotificationDetail notification={notification} />
  }
  if (notification.kind === "workgroup_contribution") {
    return (
      <WorkgroupContributionNotificationDetail notification={notification} />
    )
  }
  if (notification.kind === "ratio_watch") {
    return <RatioWatchNotificationDetail notification={notification} />
  }
  if (notification.kind === "ratio_appeal") {
    return <RatioAppealNotificationDetail notification={notification} />
  }
  if (notification.kind === "hnr") {
    return <HNRNotificationDetail notification={notification} />
  }
  if (notification.kind === "hnr_appeal") {
    return <HNRAppealNotificationDetail notification={notification} />
  }

  const published = notification.outcome === "published"
  const torrentTitle = notification.torrent_title ?? "种子"
  const reason = notification.reason ?? "审核结果详情暂时不可用。"

  return (
    <article>
      <header className="flex flex-col gap-2">
        <div className="min-w-0 flex-1">
          <h2 className="font-heading text-2xl leading-tight font-bold break-words">
            {published ? "审核通过" : "需要修改"}：{torrentTitle}
          </h2>
          <p className="mt-2 text-sm text-muted-foreground">
            来自：系统
            <span className="mx-2">•</span>
            <time dateTime={notification.created_at}>
              {formatTimeAgo(notification.created_at)}
            </time>
          </p>
        </div>
      </header>

      <p className="mt-6 text-base leading-7 break-words whitespace-pre-wrap">
        {reason}
      </p>

      <div className="mt-6">
        <Link
          to={
            published
              ? `/torrents/${notification.torrent_id ?? ""}`
              : "/account/submissions"
          }
          className={buttonVariants({ variant: "outline", size: "sm" })}
        >
          {published ? "查看种子" : "查看反馈"}
        </Link>
      </div>
    </article>
  )
}

function ContentTipNotificationDetail({
  notification,
}: {
  notification: MyNotification
}) {
  const senderDisplayName =
    notification.content_tip_sender_display_name ?? "站点成员"
  const senderUsername = notification.content_tip_sender_username
  const senderNumericID = notification.content_tip_sender_numeric_id
  const amount = formatInteger(notification.content_tip_net_amount ?? "")
  const targetTitle = notification.content_tip_target_title ?? "站内内容"
  const targetPath = contentTipTargetPath(notification)

  return (
    <article>
      <header className="flex flex-col gap-2">
        <div className="min-w-0 flex-1">
          <h2 className="font-heading text-2xl leading-tight font-bold break-words">
            收到内容打赏
          </h2>
          <p className="mt-2 text-sm text-muted-foreground">
            来自：{senderDisplayName}
            <span className="mx-2">•</span>
            <time dateTime={notification.created_at}>
              {formatTimeAgo(notification.created_at)}
            </time>
          </p>
        </div>
      </header>

      <dl className="mt-6 grid gap-4 rounded-md border bg-muted/30 p-4 text-sm sm:grid-cols-2">
        <div>
          <dt className="text-muted-foreground">到账魔力值</dt>
          <dd className="mt-1 text-lg font-semibold">{amount}</dd>
        </div>
        <div>
          <dt className="text-muted-foreground">打赏成员</dt>
          <dd className="mt-1 font-medium">
            {senderDisplayName}
            {senderNumericID ? ` #${senderNumericID}` : ""}
            {senderUsername ? ` · @${senderUsername}` : ""}
          </dd>
        </div>
      </dl>

      <div className="mt-6 rounded-md border p-4">
        <div className="text-sm text-muted-foreground">
          {contentTipTargetLabel(notification.content_tip_target_kind)}
        </div>
        <p className="mt-1 leading-7 break-words">{targetTitle}</p>
      </div>

      <div className="mt-6 flex flex-wrap gap-2">
        {targetPath ? (
          <Link
            to={targetPath}
            className={buttonVariants({ variant: "outline", size: "sm" })}
          >
            查看内容
          </Link>
        ) : null}
        <Link
          to="/account/economy"
          className={buttonVariants({ variant: "outline", size: "sm" })}
        >
          查看魔力值账本
        </Link>
      </div>
    </article>
  )
}

function MemberGiftNotificationDetail({
  notification,
}: {
  notification: MyNotification
}) {
  const senderDisplayName =
    notification.member_gift_sender_display_name ?? "站点成员"
  const senderUsername = notification.member_gift_sender_username
  const senderNumericID = notification.member_gift_sender_numeric_id
  const amount = formatInteger(notification.member_gift_net_amount ?? "")

  return (
    <article>
      <header className="flex flex-col gap-2">
        <div className="min-w-0 flex-1">
          <h2 className="font-heading text-2xl leading-tight font-bold break-words">
            收到成员赠送
          </h2>
          <p className="mt-2 text-sm text-muted-foreground">
            来自：{senderDisplayName}
            <span className="mx-2">•</span>
            <time dateTime={notification.created_at}>
              {formatTimeAgo(notification.created_at)}
            </time>
          </p>
        </div>
      </header>

      <dl className="mt-6 grid gap-4 rounded-md border bg-muted/30 p-4 text-sm sm:grid-cols-2">
        <div>
          <dt className="text-muted-foreground">到账魔力值</dt>
          <dd className="mt-1 text-lg font-semibold">{amount}</dd>
        </div>
        <div>
          <dt className="text-muted-foreground">赠送成员</dt>
          <dd className="mt-1 font-medium">
            {senderDisplayName}
            {senderNumericID ? ` #${senderNumericID}` : ""}
            {senderUsername ? ` · @${senderUsername}` : ""}
          </dd>
        </div>
      </dl>

      {notification.member_gift_message ? (
        <div className="mt-6 rounded-md border p-4">
          <div className="text-sm text-muted-foreground">留言</div>
          <p className="mt-1 leading-7 whitespace-pre-wrap">
            {notification.member_gift_message}
          </p>
        </div>
      ) : null}

      <div className="mt-6">
        <Link
          to="/account/economy"
          className={buttonVariants({ variant: "outline", size: "sm" })}
        >
          查看魔力值账本
        </Link>
      </div>
    </article>
  )
}

function WorkgroupContributionNotificationDetail({
  notification,
}: {
  notification: MyNotification
}) {
  const metric = notification.workgroup_metric
  const current = metric
    ? formatContributionValue(metric, notification.workgroup_current_value ?? 0)
    : "—"
  const target = metric
    ? formatContributionValue(metric, notification.workgroup_target_value ?? 0)
    : "—"
  return (
    <article>
      <header className="flex flex-col gap-2">
        <div className="min-w-0 flex-1">
          <h2 className="font-heading text-2xl leading-tight font-bold break-words">
            {workgroupKindLabel(notification.workgroup_kind)}贡献进度提醒
          </h2>
          <p className="mt-2 text-sm text-muted-foreground">
            来自：系统
            <span className="mx-2">•</span>
            <time dateTime={notification.created_at}>
              {formatTimeAgo(notification.created_at)}
            </time>
          </p>
        </div>
      </header>
      <p className="mt-6 text-base leading-7 whitespace-pre-wrap">
        {notification.workgroup_reason ?? "请关注本期工作组贡献进度。"}
      </p>
      <dl className="mt-6 grid gap-4 rounded-md border bg-muted/30 p-4 text-sm sm:grid-cols-2">
        <div>
          <dt className="text-muted-foreground">贡献周期</dt>
          <dd className="mt-1 font-medium">
            {formatWorkgroupPeriod(notification.workgroup_period_starts_at)}
          </dd>
        </div>
        <div>
          <dt className="text-muted-foreground">贡献项目</dt>
          <dd className="mt-1 font-medium">
            {metric ? contributionMetricLabel(metric) : "工作组贡献"}
          </dd>
        </div>
        <div>
          <dt className="text-muted-foreground">提醒时进度</dt>
          <dd className="mt-1 font-medium">
            {current} / {target}
          </dd>
        </div>
        <div>
          <dt className="text-muted-foreground">证据状态</dt>
          <dd className="mt-1 font-medium">
            {notification.workgroup_evidence_state === "complete"
              ? "证据完整"
              : "仍在采集"}
          </dd>
        </div>
      </dl>
      <p className="mt-4 text-sm leading-6 text-muted-foreground">
        这是一条人工进度提醒，不会自动暂停或结束你的成员资格。页面显示的是发送提醒时冻结的数值。
      </p>
      <div className="mt-6">
        <Link
          to="/workgroups"
          className={buttonVariants({ variant: "outline", size: "sm" })}
        >
          查看工作组贡献
        </Link>
      </div>
    </article>
  )
}

function HNRAppealNotificationDetail({
  notification,
}: {
  notification: MyNotification
}) {
  const approved = notification.hnr_status === "appeal_approved"
  const torrentTitle = notification.torrent_title ?? "种子"
  return (
    <article>
      <header className="flex flex-col gap-2">
        <div className="min-w-0 flex-1">
          <h2 className="font-heading text-2xl leading-tight font-bold break-words">
            {approved ? "H&R 申诉已批准" : "H&R 申诉已驳回"}：{torrentTitle}
          </h2>
          <p className="mt-2 text-sm text-muted-foreground">
            来自：系统
            <span className="mx-2">•</span>
            <time dateTime={notification.created_at}>
              {formatTimeAgo(notification.created_at)}
            </time>
          </p>
        </div>
      </header>
      <p className="mt-6 text-base leading-7">
        {approved
          ? "管理员已批准这条申诉，对应 H&R 义务已经豁免；其他独立限制仍按各自规则处理。"
          : "管理员已核对并驳回这条申诉，对应 H&R 义务仍需继续做种达标。"}
      </p>
      <div className="mt-6 rounded-md border bg-muted/30 p-4">
        <div className="text-sm text-muted-foreground">处理意见</div>
        <p className="mt-1 leading-7 whitespace-pre-wrap">
          {notification.hnr_appeal_response ??
            "请前往 H&R 页面查看当前处理结果。"}
        </p>
      </div>
      <div className="mt-6 flex flex-wrap gap-2">
        <Link
          to="/account/hnr"
          className={buttonVariants({ variant: "outline", size: "sm" })}
        >
          查看 H&amp;R
        </Link>
        {notification.torrent_id ? (
          <Link
            to={`/torrents/${notification.torrent_id}`}
            className={buttonVariants({ variant: "ghost", size: "sm" })}
          >
            查看种子
          </Link>
        ) : null}
      </div>
    </article>
  )
}

function HNRNotificationDetail({
  notification,
}: {
  notification: MyNotification
}) {
  const torrentTitle = notification.torrent_title ?? "种子"
  return (
    <article>
      <header className="flex flex-col gap-2">
        <div className="min-w-0 flex-1">
          <h2 className="font-heading text-2xl leading-tight font-bold break-words">
            {hnrNotificationTitle(notification.hnr_status)}：{torrentTitle}
          </h2>
          <p className="mt-2 text-sm text-muted-foreground">
            来自：系统
            <span className="mx-2">•</span>
            <time dateTime={notification.created_at}>
              {formatTimeAgo(notification.created_at)}
            </time>
          </p>
        </div>
      </header>
      <p className="mt-6 text-base leading-7 break-words">
        {hnrNotificationDescription(notification.hnr_status)}
      </p>
      <dl className="mt-6 text-sm">
        <div>
          <dt className="text-muted-foreground">宽限期截止</dt>
          <dd className="mt-1 font-medium">
            {notification.hnr_grace_ends_at
              ? formatDateTime(notification.hnr_grace_ends_at)
              : "—"}
          </dd>
        </div>
      </dl>
      <div className="mt-6 flex flex-wrap gap-2">
        <Link
          to="/account/hnr"
          className={buttonVariants({ variant: "outline", size: "sm" })}
        >
          查看 H&amp;R
        </Link>
        {notification.torrent_id ? (
          <Link
            to={`/torrents/${notification.torrent_id}`}
            className={buttonVariants({ variant: "ghost", size: "sm" })}
          >
            查看种子
          </Link>
        ) : null}
      </div>
    </article>
  )
}

function RatioAppealNotificationDetail({
  notification,
}: {
  notification: MyNotification
}) {
  return (
    <article>
      <header className="flex flex-col gap-2">
        <div className="min-w-0 flex-1">
          <h2 className="font-heading text-2xl leading-tight font-bold break-words">
            分享率申诉处理结果
          </h2>
          <p className="mt-2 text-sm text-muted-foreground">
            来自：系统
            <span className="mx-2">•</span>
            <time dateTime={notification.created_at}>
              {formatTimeAgo(notification.created_at)}
            </time>
          </p>
        </div>
      </header>
      <p className="mt-6 text-base leading-7">
        管理员已核对并驳回本期分享率申诉，本期考核继续生效。
      </p>
      <div className="mt-6 rounded-md border bg-muted/30 p-4">
        <div className="text-sm text-muted-foreground">处理意见</div>
        <p className="mt-1 leading-7 whitespace-pre-wrap">
          {notification.ratio_appeal_response ?? "请前往考核页面查看处理结果。"}
        </p>
      </div>
      <div className="mt-6">
        <Link
          to="/account/ratio"
          className={buttonVariants({ variant: "outline", size: "sm" })}
        >
          查看分享率考核
        </Link>
      </div>
    </article>
  )
}

function RatioWatchNotificationDetail({
  notification,
}: {
  notification: MyNotification
}) {
  const title = ratioWatchNotificationTitle(notification.ratio_watch_status)
  const currentRatio = formatRatioBasisPoints(
    notification.ratio_basis_points ?? -1
  )
  const targetRatio = formatRatioBasisPoints(
    notification.minimum_ratio_basis_points ?? -1
  )
  const restrictionRatio = formatRatioBasisPoints(
    notification.restriction_ratio_basis_points ?? -1
  )

  return (
    <article>
      <header className="flex flex-col gap-2">
        <div className="min-w-0 flex-1">
          <h2 className="font-heading text-2xl leading-tight font-bold break-words">
            {title}
          </h2>
          <p className="mt-2 text-sm text-muted-foreground">
            来自：系统
            <span className="mx-2">•</span>
            <time dateTime={notification.created_at}>
              {formatTimeAgo(notification.created_at)}
            </time>
          </p>
        </div>
      </header>

      <p className="mt-6 text-base leading-7 break-words">
        {ratioWatchNotificationDescription(
          notification.ratio_watch_status,
          targetRatio
        )}
      </p>

      <dl className="mt-6 grid gap-4 text-sm sm:grid-cols-2">
        <div>
          <dt className="text-muted-foreground">当时分享率</dt>
          <dd className="mt-1 font-medium">{currentRatio}</dd>
        </div>
        <div>
          <dt className="text-muted-foreground">恢复目标</dt>
          <dd className="mt-1 font-medium">{targetRatio}</dd>
        </div>
        <div>
          <dt className="text-muted-foreground">下载限制线</dt>
          <dd className="mt-1 font-medium">{restrictionRatio}</dd>
        </div>
        <div>
          <dt className="text-muted-foreground">观察期截止</dt>
          <dd className="mt-1 font-medium">
            {notification.deadline_at
              ? formatDateTime(notification.deadline_at)
              : "—"}
          </dd>
        </div>
      </dl>

      <div className="mt-6">
        <Link
          to="/account/ratio"
          className={buttonVariants({ variant: "outline", size: "sm" })}
        >
          查看分享率考核
        </Link>
      </div>
    </article>
  )
}

function notificationListTitle(notification: MyNotification) {
  if (notification.kind === "content_tip") {
    const sender =
      notification.content_tip_sender_display_name ??
      notification.content_tip_sender_username ??
      "站点成员"
    return `收到 ${sender} 打赏的 ${formatInteger(notification.content_tip_net_amount ?? "")} 魔力值`
  }
  if (notification.kind === "member_gift") {
    const sender =
      notification.member_gift_sender_display_name ??
      notification.member_gift_sender_username ??
      "站点成员"
    return `收到 ${sender} 赠送的 ${formatInteger(notification.member_gift_net_amount ?? "")} 魔力值`
  }
  if (notification.kind === "workgroup_contribution") {
    return `${workgroupKindLabel(notification.workgroup_kind)}贡献进度提醒`
  }
  if (notification.kind === "ratio_watch") {
    return ratioWatchNotificationTitle(notification.ratio_watch_status)
  }
  if (notification.kind === "ratio_appeal") {
    return "分享率申诉处理结果"
  }
  if (notification.kind === "hnr") {
    return `${hnrNotificationTitle(notification.hnr_status)}：${notification.torrent_title ?? "种子"}`
  }
  if (notification.kind === "hnr_appeal") {
    return `${hnrNotificationTitle(notification.hnr_status)}：${notification.torrent_title ?? "种子"}`
  }
  const prefix = notification.outcome === "published" ? "审核通过" : "需要修改"
  return `${prefix}：${notification.torrent_title ?? "种子"}`
}

function contentTipTargetPath(notification: MyNotification) {
  if (
    notification.content_tip_target_kind === "torrent" &&
    notification.content_tip_torrent_id
  ) {
    return `/torrents/${notification.content_tip_torrent_id}`
  }
  if (
    notification.content_tip_target_kind === "post" &&
    notification.content_tip_post_id
  ) {
    return `/social/post/${notification.content_tip_post_id}`
  }
  return null
}

function contentTipTargetLabel(
  kind: MyNotification["content_tip_target_kind"]
) {
  if (kind === "torrent") return "被打赏的种子"
  if (kind === "post") return "被打赏的动态"
  return "被打赏的评论"
}

function workgroupKindLabel(kind: MyNotification["workgroup_kind"]) {
  if (kind === "reseed") return "转种组"
  if (kind === "review") return "种审组"
  if (kind === "retention") return "保种组"
  return "工作组"
}

function formatWorkgroupPeriod(value: string | undefined) {
  if (!value) return "—"
  return new Intl.DateTimeFormat("zh-CN", {
    year: "numeric",
    month: "long",
    timeZone: "UTC",
  }).format(new Date(value))
}

function hnrNotificationTitle(status: MyNotification["hnr_status"]) {
  switch (status) {
    case "grace_started":
      return "H&R 已进入宽限期"
    case "download_restricted":
      return "H&R 待补做，下载受限"
    case "satisfied":
      return "H&R 已达标"
    case "appeal_approved":
      return "H&R 申诉已批准"
    case "appeal_rejected":
      return "H&R 申诉已驳回"
    default:
      return "H&R 状态已更新"
  }
}

function hnrNotificationDescription(status: MyNotification["hnr_status"]) {
  switch (status) {
    case "grace_started":
      return "本条记录尚未达到所需做种时长或实际分享率，请在宽限期结束前继续做种。"
    case "download_restricted":
      return "宽限期已结束，新种子下载暂时受限；Tracker 仍允许继续做种，满足本条义务后会自动恢复。"
    case "satisfied":
      return "本条记录已满足做种时长或实际分享率要求；若没有其他下载限制，新种子下载会自动恢复。"
    case "appeal_approved":
      return "本条 H&R 义务已按申诉处理结果豁免，其他独立限制仍按各自规则生效。"
    case "appeal_rejected":
      return "本条申诉已驳回，请继续做种以满足时长或实际分享率要求。"
    default:
      return "H&R 状态已更新，请前往 H&R 页面查看当前进度。"
  }
}

function ratioWatchNotificationTitle(
  status: MyNotification["ratio_watch_status"]
) {
  switch (status) {
    case "watching":
      return "已进入分享率观察期"
    case "warning":
      return "分享率观察期已结束"
    case "download_restricted":
      return "下载权限已受限"
    case "satisfied":
      return "分享率已经恢复达标"
    case "manually_cleared":
      return "分享率考核已人工解除"
    default:
      return "分享率考核状态已更新"
  }
}

function ratioWatchNotificationDescription(
  status: MyNotification["ratio_watch_status"],
  targetRatio: string
) {
  switch (status) {
    case "watching":
      return `你的有效下载量和分享率已达到考核条件。请在观察期结束前把分享率提升到 ${targetRatio}。`
    case "warning":
      return `观察期已经结束，当前分享率仍未达到 ${targetRatio}。请尽快增加有效上传，避免下载权限受限。`
    case "download_restricted":
      return `当前分享率已低于下载限制线，下载权限暂时受限。分享率恢复到 ${targetRatio} 后，系统会自动解除本次限制。`
    case "satisfied":
      return `当前分享率已经达到 ${targetRatio}，本次考核已结束；若此前由本次考核限制下载，系统已自动解除。`
    case "manually_cleared":
      return "管理员已人工解除本期分享率考核；PtYes 迁移或其他独立账户下载限制仍保持不变。"
    default:
      return "分享率考核状态已更新，请前往考核页面查看当前进度。"
  }
}

function formatTimeAgo(value: string) {
  const timestamp = Date.parse(value)
  if (!Number.isFinite(timestamp)) return "时间未知"

  const seconds = Math.max(0, Math.floor((Date.now() - timestamp) / 1_000))
  if (seconds < 60) return "刚刚"

  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return `${minutes}分钟前`

  const hours = Math.floor(minutes / 60)
  if (hours < 24) return `${hours}小时前`

  const days = Math.floor(hours / 24)
  if (days < 30) return `${days}天前`

  const months = Math.floor(days / 30)
  if (months < 12) return `${months}个月前`

  return `${Math.floor(months / 12)}年前`
}

function NotificationAlert({
  title,
  description,
  retry,
}: {
  title: string
  description: string
  retry: () => void
}) {
  return (
    <Alert variant="destructive">
      <CircleAlertIcon />
      <AlertTitle>{title}</AlertTitle>
      <AlertDescription>{description}</AlertDescription>
      <AlertAction>
        <Button type="button" variant="outline" size="sm" onClick={retry}>
          <RefreshCwIcon data-icon="inline-start" />
          重试
        </Button>
      </AlertAction>
    </Alert>
  )
}

function NotificationAccessCard({
  icon,
  title,
  description,
  action,
}: {
  icon: React.ReactNode
  title: string
  description: string
  action: React.ReactNode
}) {
  return (
    <Card>
      <CardContent>
        <Empty>
          <EmptyHeader>
            <EmptyMedia variant="icon">{icon}</EmptyMedia>
            <EmptyTitle>{title}</EmptyTitle>
            <EmptyDescription>{description}</EmptyDescription>
          </EmptyHeader>
          <EmptyContent>{action}</EmptyContent>
        </Empty>
      </CardContent>
    </Card>
  )
}

function NotificationPageSkeleton() {
  return (
    <Card
      className="gap-0 py-0 shadow-none"
      aria-label="正在加载消息"
      aria-busy="true"
    >
      <CardHeader className="border-b py-4">
        <Skeleton className="h-5 w-24" />
        <Skeleton className="h-4 w-64 max-w-full" />
      </CardHeader>
      <CardContent className="flex flex-col gap-4 py-4">
        {Array.from({ length: 4 }, (_, index) => (
          <div key={index} className="flex items-start gap-3">
            <Skeleton className="mt-1 size-2 rounded-full" />
            <div className="flex flex-1 flex-col gap-2">
              <Skeleton className="h-5 w-2/3" />
              <Skeleton className="h-4 w-full" />
            </div>
          </div>
        ))}
      </CardContent>
    </Card>
  )
}

function notificationPageNumber(value: string | null) {
  if (!value || !/^\d+$/.test(value)) return 1
  const parsed = Number(value)
  if (!Number.isSafeInteger(parsed) || parsed < 1) return 1
  return Math.min(parsed, maximumNotificationPage)
}

import * as React from "react"
import { Link, useSearchParams } from "react-router"
import {
  type LucideIcon,
  ArrowLeftIcon,
  CheckCheckIcon,
  CheckIcon,
  CircleAlertIcon,
  ClockAlertIcon,
  CoinsIcon,
  FileCheck2Icon,
  FilePenLineIcon,
  GaugeIcon,
  GiftIcon,
  InboxIcon,
  LogInIcon,
  MailOpenIcon,
  MessageSquarePlusIcon,
  RefreshCwIcon,
  SendIcon,
  ShieldXIcon,
  Trash2Icon,
  UsersRoundIcon,
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
import { Button, buttonVariants } from "~/components/ui/button"
import { Card, CardContent } from "~/components/ui/card"
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
import { ToggleGroup, ToggleGroupItem } from "~/components/ui/toggle-group"
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "~/components/ui/tooltip"
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
  const inboxRef = React.useRef<HTMLElement>(null)
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
    // Phones swap the list for the detail pane, so bring its top into view.
    if (
      typeof window.matchMedia === "function" &&
      window.matchMedia("(width < 48rem)").matches
    ) {
      inboxRef.current?.scrollIntoView({ block: "start" })
    }
  }

  return (
    <PageLayout className="gap-5">
      <header className="flex flex-wrap items-end justify-between gap-3">
        <div className="flex min-w-0 flex-col gap-1 pt-1">
          <h1 className="font-heading text-lg leading-tight font-semibold tracking-title">
            站内消息
          </h1>
          {notifications.data ? (
            <p className="text-xs text-muted-foreground">
              共 {notifications.data.total.toLocaleString("zh-CN")} 条
              {unreadOnly ? "未读消息" : "消息"}
              {notifications.data.unread_count > 0 && !unreadOnly
                ? `，${notifications.data.unread_count.toLocaleString("zh-CN")} 条未读`
                : ""}
            </p>
          ) : null}
        </div>
        {session.data && canRead && notifications.data && canCreateFeedback ? (
          <Button
            type="button"
            size="sm"
            onClick={() => {
              setFeedbackError(undefined)
              setFeedbackOpen(true)
            }}
          >
            <MessageSquarePlusIcon data-icon="inline-start" />
            联系管理员
          </Button>
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
          ref={inboxRef}
          aria-label="站内消息"
          className="scroll-mt-[calc(var(--shell-header-height)+1rem)]"
        >
          <Card className="gap-0 py-0 md:grid md:h-[640px] md:grid-cols-[minmax(0,21rem)_minmax(0,1fr)] lg:grid-cols-[minmax(0,23rem)_minmax(0,1fr)]">
            <div
              className={cn(
                "flex min-h-0 min-w-0 flex-col md:border-r",
                selectedNotification && "max-md:hidden"
              )}
            >
              <div className="flex items-center justify-between gap-2 border-b px-3 py-2.5">
                <h2 id="notification-list-title" className="sr-only">
                  消息列表
                </h2>
                <ToggleGroup
                  value={[unreadOnly ? "unread" : "all"]}
                  onValueChange={(values) => {
                    const next = values[0]
                    if (next) setUnreadOnly(next === "unread")
                  }}
                  variant="segment"
                  size="sm"
                  spacing={0.5}
                  aria-label="筛选消息"
                >
                  <ToggleGroupItem value="all" className="px-3">
                    全部
                  </ToggleGroupItem>
                  <ToggleGroupItem value="unread" className="px-3">
                    未读
                    {notifications.data.unread_count > 0 ? (
                      <span className="font-mono text-2xs text-link tabular-nums">
                        {notifications.data.unread_count > 99
                          ? "99+"
                          : notifications.data.unread_count}
                      </span>
                    ) : null}
                  </ToggleGroupItem>
                </ToggleGroup>
                <div className="flex items-center gap-0.5">
                  {canWrite ? (
                    <InboxToolbarButton
                      label="全部已读"
                      disabled={
                        markAllRead.isPending ||
                        notifications.data.unread_count === 0
                      }
                      onClick={() => markAllRead.mutate()}
                    >
                      {markAllRead.isPending ? <Spinner /> : <CheckCheckIcon />}
                    </InboxToolbarButton>
                  ) : null}
                  {canArchive ? (
                    <InboxToolbarButton
                      label="删除全部"
                      className="hover:text-destructive"
                      onClick={() => setArchiveConfirmOpen(true)}
                    >
                      <Trash2Icon />
                    </InboxToolbarButton>
                  ) : null}
                </div>
              </div>

              {notifications.data.items.length === 0 ? (
                <Empty className="min-h-64 flex-1 border-0">
                  <EmptyHeader>
                    <EmptyMedia variant="icon">
                      <InboxIcon />
                    </EmptyMedia>
                    <EmptyTitle>暂无消息</EmptyTitle>
                    <EmptyDescription>
                      {unreadOnly
                        ? "所有消息都已读完。"
                        : "审核结果、H&R 与魔力值变动等通知会出现在这里。"}
                    </EmptyDescription>
                  </EmptyHeader>
                </Empty>
              ) : (
                <ol className="min-h-0 flex-1 divide-y overflow-y-auto">
                  {notifications.data.items.map((notification) => (
                    <li key={notification.id}>
                      <NotificationListItem
                        notification={notification}
                        selected={selectedNotificationID === notification.id}
                        onSelect={() => selectNotification(notification)}
                      />
                    </li>
                  ))}
                </ol>
              )}

              {notifications.data.total > notifications.data.limit ? (
                <div className="border-t px-3 py-2.5">
                  <OffsetPagination
                    total={notifications.data.total}
                    limit={notifications.data.limit}
                    offset={notifications.data.offset}
                    onOffsetChange={setNotificationOffset}
                    ariaLabel="消息分页"
                    summaryLabel="消息"
                    summaryUnit="条"
                    buttonVariant="ghost"
                    compact
                    className="justify-between"
                  />
                </div>
              ) : null}
            </div>

            <div
              className={cn(
                "min-h-0 min-w-0 overflow-y-auto",
                !selectedNotification && "max-md:hidden"
              )}
            >
              {selectedNotification ? (
                <>
                  <div className="border-b px-2 py-1.5 md:hidden">
                    <Button
                      type="button"
                      variant="ghost"
                      size="sm"
                      onClick={() => setSelectedNotificationID(null)}
                    >
                      <ArrowLeftIcon data-icon="inline-start" />
                      返回列表
                    </Button>
                  </div>
                  <NotificationDetail notification={selectedNotification} />
                </>
              ) : (
                <Empty className="h-full border-0">
                  <EmptyHeader>
                    <EmptyMedia variant="icon">
                      <MailOpenIcon />
                    </EmptyMedia>
                    <EmptyTitle>请选择一条消息查看详情</EmptyTitle>
                    <EmptyDescription>
                      点击左侧消息即可查看完整内容和相关操作。
                    </EmptyDescription>
                  </EmptyHeader>
                </Empty>
              )}
            </div>
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
          className="gap-4 sm:max-w-lg"
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
                  className="min-h-[162px] resize-none leading-6"
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

type NotificationTone = "success" | "warning" | "destructive" | "info"

const notificationToneClasses: Record<NotificationTone, string> = {
  success: "bg-success/12 text-success-foreground",
  warning: "bg-warning/12 text-warning-foreground",
  destructive: "bg-destructive/12 text-destructive",
  info: "bg-info/12 text-info",
}

type NotificationPresentation = {
  category: string
  icon: LucideIcon
  tone: NotificationTone
  title: string
  sender: string
}

function notificationPresentation(
  notification: MyNotification
): NotificationPresentation {
  const torrentTitle = notification.torrent_title ?? "种子"
  switch (notification.kind) {
    case "content_tip":
      return {
        category: "魔力值",
        icon: CoinsIcon,
        tone: "warning",
        title: "收到内容打赏",
        sender: notification.content_tip_sender_display_name ?? "站点成员",
      }
    case "member_gift":
      return {
        category: "魔力值",
        icon: GiftIcon,
        tone: "warning",
        title: "收到成员赠送",
        sender: notification.member_gift_sender_display_name ?? "站点成员",
      }
    case "workgroup_contribution":
      return {
        category: "工作组",
        icon: UsersRoundIcon,
        tone:
          notification.workgroup_disciplinary_action === "membership_ended"
            ? "destructive"
            : notification.workgroup_disciplinary_action === "marked"
              ? "warning"
              : "info",
        title: notificationListTitle(notification),
        sender: "系统",
      }
    case "ratio_watch":
      return {
        category: "分享率",
        icon: GaugeIcon,
        tone:
          notification.ratio_watch_status === "download_restricted"
            ? "destructive"
            : notification.ratio_watch_status === "satisfied" ||
                notification.ratio_watch_status === "manually_cleared"
              ? "success"
              : "warning",
        title: ratioWatchNotificationTitle(notification.ratio_watch_status),
        sender: "系统",
      }
    case "ratio_appeal":
      return {
        category: "分享率",
        icon: GaugeIcon,
        tone: "warning",
        title: "分享率申诉处理结果",
        sender: "系统",
      }
    case "hnr":
      return {
        category: "H&R",
        icon: ClockAlertIcon,
        tone:
          notification.hnr_status === "download_restricted"
            ? "destructive"
            : notification.hnr_status === "satisfied"
              ? "success"
              : "warning",
        title: `${hnrNotificationTitle(notification.hnr_status)}：${torrentTitle}`,
        sender: "系统",
      }
    case "hnr_appeal":
      return {
        category: "H&R",
        icon: ClockAlertIcon,
        tone:
          notification.hnr_status === "appeal_approved"
            ? "success"
            : "destructive",
        title: `${
          notification.hnr_status === "appeal_approved"
            ? "H&R 申诉已批准"
            : "H&R 申诉已驳回"
        }：${torrentTitle}`,
        sender: "系统",
      }
    default: {
      const published = notification.outcome === "published"
      return {
        category: "种子审核",
        icon: published ? FileCheck2Icon : FilePenLineIcon,
        tone: published ? "success" : "warning",
        title: `${published ? "审核通过" : "需要修改"}：${torrentTitle}`,
        sender: "系统",
      }
    }
  }
}

function NotificationKindIcon({
  presentation,
  className,
}: {
  presentation: NotificationPresentation
  className?: string
}) {
  const Icon = presentation.icon
  return (
    <span
      aria-hidden="true"
      className={cn(
        "flex shrink-0 items-center justify-center rounded-lg",
        notificationToneClasses[presentation.tone],
        className
      )}
    >
      <Icon />
    </span>
  )
}

function NotificationListItem({
  notification,
  selected,
  onSelect,
}: {
  notification: MyNotification
  selected: boolean
  onSelect: () => void
}) {
  const presentation = notificationPresentation(notification)
  const unread = notification.read_at === null
  return (
    <button
      type="button"
      aria-current={selected ? "true" : undefined}
      className={cn(
        "relative flex w-full gap-3 px-4 py-3 text-left transition-colors outline-none hover:bg-accent focus-visible:bg-accent",
        selected &&
          "bg-accent before:absolute before:inset-y-0 before:left-0 before:w-0.5 before:bg-primary"
      )}
      onClick={onSelect}
    >
      <NotificationKindIcon
        presentation={presentation}
        className="mt-0.5 size-8 [&_svg]:size-4"
      />
      <span className="flex min-w-0 flex-1 flex-col gap-0.5">
        <span className="flex items-center gap-2">
          <span className="truncate text-xs text-muted-foreground">
            {presentation.category}
          </span>
          <time
            className="ml-auto shrink-0 text-2xs text-faint"
            dateTime={notification.created_at}
          >
            {formatTimeAgo(notification.created_at)}
          </time>
        </span>
        <span
          className={cn(
            "line-clamp-2 text-sm leading-5 break-words",
            unread ? "font-semibold text-foreground" : "text-muted-foreground"
          )}
        >
          {notificationListTitle(notification)}
        </span>
      </span>
      {unread ? (
        <span
          role="img"
          aria-label="未读"
          className="mt-2 size-2 shrink-0 rounded-full bg-primary"
        />
      ) : null}
    </button>
  )
}

function InboxToolbarButton({
  label,
  className,
  disabled,
  onClick,
  children,
}: {
  label: string
  className?: string
  disabled?: boolean
  onClick: () => void
  children: React.ReactNode
}) {
  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            aria-label={label}
            className={cn("text-muted-foreground", className)}
            disabled={disabled}
            onClick={onClick}
          />
        }
      >
        {children}
      </TooltipTrigger>
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  )
}

function NotificationDetail({
  notification,
}: {
  notification: MyNotification
}) {
  const presentation = notificationPresentation(notification)
  return (
    <article>
      <header className="flex items-start gap-3 border-b px-4 py-4 sm:px-6 sm:py-5">
        <NotificationKindIcon
          presentation={presentation}
          className="size-10 [&_svg]:size-5"
        />
        <div className="min-w-0 flex-1">
          <p className="text-xs text-muted-foreground">
            {presentation.category}
          </p>
          <h2 className="mt-0.5 font-heading text-md leading-snug font-semibold tracking-title break-words sm:text-lg">
            {presentation.title}
          </h2>
          <p className="mt-1 text-xs text-faint">
            来自：{presentation.sender}
            <span aria-hidden="true" className="mx-1.5">
              ·
            </span>
            <time
              dateTime={notification.created_at}
              title={formatDateTime(notification.created_at)}
            >
              {formatTimeAgo(notification.created_at)}
            </time>
          </p>
        </div>
      </header>
      <div className="flex flex-col gap-5 px-4 py-5 sm:px-6">
        <NotificationDetailBody notification={notification} />
      </div>
    </article>
  )
}

function NotificationDetailBody({
  notification,
}: {
  notification: MyNotification
}) {
  switch (notification.kind) {
    case "content_tip":
      return <ContentTipNotificationDetail notification={notification} />
    case "member_gift":
      return <MemberGiftNotificationDetail notification={notification} />
    case "workgroup_contribution":
      return (
        <WorkgroupContributionNotificationDetail notification={notification} />
      )
    case "ratio_watch":
      return <RatioWatchNotificationDetail notification={notification} />
    case "ratio_appeal":
      return <RatioAppealNotificationDetail notification={notification} />
    case "hnr":
      return <HNRNotificationDetail notification={notification} />
    case "hnr_appeal":
      return <HNRAppealNotificationDetail notification={notification} />
    default:
      return <TorrentReviewNotificationDetail notification={notification} />
  }
}

function DetailText({ children }: { children: React.ReactNode }) {
  return (
    <p className="text-md leading-7 break-words whitespace-pre-wrap">
      {children}
    </p>
  )
}

function DetailFacts({ children }: { children: React.ReactNode }) {
  return (
    <dl className="grid gap-px overflow-hidden rounded-lg border bg-border sm:grid-cols-2 sm:[&>:last-child:nth-child(odd)]:col-span-2">
      {children}
    </dl>
  )
}

function DetailFact({
  label,
  emphasis = false,
  children,
}: {
  label: string
  emphasis?: boolean
  children: React.ReactNode
}) {
  return (
    <div className="flex min-w-0 flex-col gap-1 bg-card px-3.5 py-3">
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd
        className={cn(
          "text-sm font-medium break-words",
          emphasis && "font-mono text-lg font-semibold tabular-nums"
        )}
      >
        {children}
      </dd>
    </div>
  )
}

function DetailNote({
  label,
  children,
}: {
  label: string
  children: React.ReactNode
}) {
  return (
    <div className="rounded-lg bg-muted px-4 py-3">
      <p className="text-xs text-muted-foreground">{label}</p>
      <p className="mt-1 text-sm leading-6 break-words whitespace-pre-wrap">
        {children}
      </p>
    </div>
  )
}

function DetailActions({ children }: { children: React.ReactNode }) {
  return <div className="flex flex-wrap gap-2">{children}</div>
}

function TorrentReviewNotificationDetail({
  notification,
}: {
  notification: MyNotification
}) {
  const published = notification.outcome === "published"
  return (
    <>
      <DetailText>
        {notification.reason ?? "审核结果详情暂时不可用。"}
      </DetailText>
      <DetailActions>
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
      </DetailActions>
    </>
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
    <>
      <DetailFacts>
        <DetailFact label="到账魔力值" emphasis>
          <span className="text-warning-foreground">{amount}</span>
        </DetailFact>
        <DetailFact label="打赏成员">
          {senderDisplayName}
          {senderNumericID ? ` #${senderNumericID}` : ""}
          {senderUsername ? ` · @${senderUsername}` : ""}
        </DetailFact>
      </DetailFacts>
      <DetailNote
        label={contentTipTargetLabel(notification.content_tip_target_kind)}
      >
        {targetTitle}
      </DetailNote>
      <DetailActions>
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
      </DetailActions>
    </>
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
    <>
      <DetailFacts>
        <DetailFact label="到账魔力值" emphasis>
          <span className="text-warning-foreground">{amount}</span>
        </DetailFact>
        <DetailFact label="赠送成员">
          {senderDisplayName}
          {senderNumericID ? ` #${senderNumericID}` : ""}
          {senderUsername ? ` · @${senderUsername}` : ""}
        </DetailFact>
      </DetailFacts>
      {notification.member_gift_message ? (
        <DetailNote label="留言">{notification.member_gift_message}</DetailNote>
      ) : null}
      <DetailActions>
        <Link
          to="/account/economy"
          className={buttonVariants({ variant: "outline", size: "sm" })}
        >
          查看魔力值账本
        </Link>
      </DetailActions>
    </>
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
    <>
      <DetailText>
        {notification.workgroup_reason ?? "请关注本期工作组贡献进度。"}
      </DetailText>
      <DetailFacts>
        <DetailFact label="贡献周期">
          {formatWorkgroupPeriod(notification.workgroup_period_starts_at)}
        </DetailFact>
        <DetailFact label="贡献项目">
          {metric ? contributionMetricLabel(metric) : "工作组贡献"}
        </DetailFact>
        <DetailFact label="提醒时进度">
          {current} / {target}
        </DetailFact>
        <DetailFact label="证据状态">
          {notification.workgroup_evidence_state === "complete"
            ? "证据完整"
            : "仍在采集"}
        </DetailFact>
        {notification.workgroup_miss_count ? (
          <DetailFact label="未达标标记">
            {notification.workgroup_miss_count} /{" "}
            {notification.workgroup_allowed_misses ?? 3}
          </DetailFact>
        ) : null}
      </DetailFacts>
      <p className="text-xs leading-5 text-muted-foreground">
        {notification.workgroup_disciplinary_action
          ? notification.workgroup_disciplinary_action === "membership_ended"
            ? `这是已结束自然月的不可变结算结果；未达标已超过 ${notification.workgroup_allowed_misses ?? 3} 次，系统已结束转种组资格。`
            : `这是已结束自然月的不可变结算结果；前 ${notification.workgroup_allowed_misses ?? 3} 次未达标只做标记。`
          : "这是一条人工进度提醒，不会自动变更你的成员资格。页面显示的是发送提醒时冻结的数值。"}
      </p>
      <DetailActions>
        <Link
          to="/workgroups"
          className={buttonVariants({ variant: "outline", size: "sm" })}
        >
          查看工作组贡献
        </Link>
      </DetailActions>
    </>
  )
}

function HNRAppealNotificationDetail({
  notification,
}: {
  notification: MyNotification
}) {
  const approved = notification.hnr_status === "appeal_approved"
  return (
    <>
      <DetailText>
        {approved
          ? "管理员已批准这条申诉，对应 H&R 义务已经豁免；其他独立限制仍按各自规则处理。"
          : "管理员已核对并驳回这条申诉，对应 H&R 义务仍需继续做种达标。"}
      </DetailText>
      <DetailNote label="处理意见">
        {notification.hnr_appeal_response ??
          "请前往 H&R 页面查看当前处理结果。"}
      </DetailNote>
      <HNRDetailActions torrentId={notification.torrent_id} />
    </>
  )
}

function HNRDetailActions({ torrentId }: { torrentId?: number | null }) {
  return (
    <DetailActions>
      <Link
        to="/account/hnr"
        className={buttonVariants({ variant: "outline", size: "sm" })}
      >
        查看 H&amp;R
      </Link>
      {torrentId ? (
        <Link
          to={`/torrents/${torrentId}`}
          className={buttonVariants({ variant: "ghost", size: "sm" })}
        >
          查看种子
        </Link>
      ) : null}
    </DetailActions>
  )
}

function HNRNotificationDetail({
  notification,
}: {
  notification: MyNotification
}) {
  return (
    <>
      <DetailText>
        {hnrNotificationDescription(notification.hnr_status)}
      </DetailText>
      <DetailFacts>
        <DetailFact label="宽限期截止">
          {notification.hnr_grace_ends_at
            ? formatDateTime(notification.hnr_grace_ends_at)
            : "—"}
        </DetailFact>
      </DetailFacts>
      <HNRDetailActions torrentId={notification.torrent_id} />
    </>
  )
}

function RatioAppealNotificationDetail({
  notification,
}: {
  notification: MyNotification
}) {
  return (
    <>
      <DetailText>
        管理员已核对并驳回本期分享率申诉，本期考核继续生效。
      </DetailText>
      <DetailNote label="处理意见">
        {notification.ratio_appeal_response ?? "请前往考核页面查看处理结果。"}
      </DetailNote>
      <DetailActions>
        <Link
          to="/account/ratio"
          className={buttonVariants({ variant: "outline", size: "sm" })}
        >
          查看分享率考核
        </Link>
      </DetailActions>
    </>
  )
}

function RatioWatchNotificationDetail({
  notification,
}: {
  notification: MyNotification
}) {
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
    <>
      <DetailText>
        {ratioWatchNotificationDescription(
          notification.ratio_watch_status,
          targetRatio
        )}
      </DetailText>
      <DetailFacts>
        <DetailFact label="当时分享率">
          <span className="font-mono tabular-nums">{currentRatio}</span>
        </DetailFact>
        <DetailFact label="恢复目标">
          <span className="font-mono tabular-nums">{targetRatio}</span>
        </DetailFact>
        <DetailFact label="下载限制线">
          <span className="font-mono tabular-nums">{restrictionRatio}</span>
        </DetailFact>
        <DetailFact label="观察期截止">
          {notification.deadline_at
            ? formatDateTime(notification.deadline_at)
            : "—"}
        </DetailFact>
      </DetailFacts>
      <DetailActions>
        <Link
          to="/account/ratio"
          className={buttonVariants({ variant: "outline", size: "sm" })}
        >
          查看分享率考核
        </Link>
      </DetailActions>
    </>
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
    if (notification.workgroup_disciplinary_action === "membership_ended") {
      return `${workgroupKindLabel(notification.workgroup_kind)}资格已结束`
    }
    if (notification.workgroup_disciplinary_action === "marked") {
      return `${workgroupKindLabel(notification.workgroup_kind)}月度未达标标记`
    }
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
      className="gap-0 py-0 md:grid md:h-[640px] md:grid-cols-[minmax(0,21rem)_minmax(0,1fr)] lg:grid-cols-[minmax(0,23rem)_minmax(0,1fr)]"
      aria-label="正在加载消息"
      aria-busy="true"
    >
      <div className="flex flex-col md:border-r">
        <div className="border-b px-3 py-2.5">
          <Skeleton className="h-8 w-32 rounded-lg" />
        </div>
        {Array.from({ length: 5 }, (_, index) => (
          <div
            key={index}
            className="flex gap-3 border-b px-4 py-3 last:border-b-0"
          >
            <Skeleton className="size-8 shrink-0 rounded-lg" />
            <div className="flex flex-1 flex-col gap-2">
              <Skeleton className="h-3 w-16" />
              <Skeleton className="h-4 w-full" />
            </div>
          </div>
        ))}
      </div>
      <div className="hidden flex-col gap-3 p-6 md:flex">
        <Skeleton className="h-6 w-2/3" />
        <Skeleton className="h-4 w-full" />
        <Skeleton className="h-4 w-4/5" />
      </div>
    </Card>
  )
}

function notificationPageNumber(value: string | null) {
  if (!value || !/^\d+$/.test(value)) return 1
  const parsed = Number(value)
  if (!Number.isSafeInteger(parsed) || parsed < 1) return 1
  return Math.min(parsed, maximumNotificationPage)
}

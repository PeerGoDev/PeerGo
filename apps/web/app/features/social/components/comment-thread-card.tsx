import * as React from "react"
import { Link } from "react-router"
import {
  ArrowDownIcon,
  ArrowUpIcon,
  ChevronDownIcon,
  ChevronUpIcon,
  CircleAlertIcon,
  FlagIcon,
  FlameIcon,
  LogInIcon,
  MessageCircleIcon,
  MessageSquareIcon,
  PencilIcon,
  RefreshCwIcon,
  ReplyIcon,
  SendIcon,
  SparklesIcon,
  Trash2Icon,
  XIcon,
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
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "~/components/ui/card"
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "~/components/ui/empty"
import {
  Field,
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLabel,
} from "~/components/ui/field"
import { Skeleton } from "~/components/ui/skeleton"
import { Spinner } from "~/components/ui/spinner"
import { Textarea } from "~/components/ui/textarea"
import { useWebSession } from "~/features/auth/api/session.mutations"
import { useCapabilities } from "~/features/authz/api/capabilities.queries"
import {
  type Comment,
  type CommentSort,
  type CommentTarget,
  useComments,
  useCreateComment,
  useDeleteComment,
  useUpdateComment,
} from "~/features/social/api/comments.queries"
import { OffsetPagination } from "~/shared/components/offset-pagination"
import { UserAvatar } from "~/shared/components/user-avatar"
import { CommentReportDialog } from "~/features/social/components/comment-report-dialog"
import { ContentTipDialog } from "~/features/economy/components/content-tip-dialog"
import { formatRelativeTime } from "~/features/torrent/model/format"
import { ApiProblemError, requestErrorDescription } from "~/shared/api/problem"
import {
  formatCompactDateTime,
  formatDateTime,
} from "~/shared/formatters/date-time"
import { cn } from "~/lib/utils"

const commentPageSize = 20
const maxCommentCharacters = 2_000
const collapsedCommentLength = 180
const collapsedReplyPreviewCount = 3

type ReplyTarget = Pick<Comment, "id" | "author"> & { rootId: string }

export function CommentThreadCard({
  target,
  description,
  composerPlaceholder,
  appearance = "default",
}: {
  target: CommentTarget
  description: string
  composerPlaceholder: string
  appearance?: "default" | "torrent" | "announcement" | "social"
}) {
  const torrentAppearance = appearance === "torrent"
  const announcementAppearance = appearance === "announcement"
  const socialAppearance = appearance === "social"
  const titleId = React.useId()
  const composerFieldId = React.useId()
  const [offset, setOffset] = React.useState(0)
  const [socialCommentSort, setSocialCommentSort] =
    React.useState<CommentSort>("newest")
  const [announcementCommentSort, setAnnouncementCommentSort] = React.useState<
    "newest" | "oldest"
  >("newest")
  const [draft, setDraft] = React.useState("")
  const [replyDraft, setReplyDraft] = React.useState("")
  const [replyTarget, setReplyTarget] = React.useState<ReplyTarget>()
  const [editingCommentId, setEditingCommentId] = React.useState<string>()
  const [deleteTarget, setDeleteTarget] = React.useState<Comment>()
  const [reportTarget, setReportTarget] = React.useState<Comment>()
  const [expandedSocialThreads, setExpandedSocialThreads] = React.useState(
    () => new Set<string>()
  )
  const [anchorCommentId] = React.useState(() =>
    typeof window === "undefined"
      ? undefined
      : window.location.hash.match(/^#comment-(.+)$/)?.[1]
  )
  const [searchingAnchor, setSearchingAnchor] = React.useState(
    Boolean(anchorCommentId)
  )
  const createRequestId = React.useRef<string>(undefined)
  const replyRequestId = React.useRef<string>(undefined)

  const comments = useComments(
    target,
    commentPageSize,
    offset,
    socialAppearance ? socialCommentSort : undefined
  )
  const session = useWebSession()
  const capabilities = useCapabilities(session.data?.user.id)
  const createComment = useCreateComment(target)
  const createReply = useCreateComment(target)
  const updateComment = useUpdateComment(target)
  const deleteComment = useDeleteComment(target)

  const capabilityActions = React.useMemo(
    () => new Set(capabilities.data?.items.map((item) => item.action) ?? []),
    [capabilities.data?.items]
  )
  const canCreate = capabilityActions.has(
    target.kind === "torrent"
      ? "torrent.comment.create.self"
      : target.kind === "post"
        ? "social.post.comment.create.self"
        : "announcement.comment.create.self"
  )
  const canUpdateOwn = capabilityActions.has("comment.update.self")
  const canDeleteOwn = capabilityActions.has("comment.delete.self")
  const canReport = capabilityActions.has("comment.report.create.self")

  React.useEffect(() => {
    const total = socialAppearance
      ? comments.data?.thread_total
      : comments.data?.total
    if (total === undefined || offset === 0 || offset < total) {
      return
    }
    setOffset(lastPageOffset(total, commentPageSize))
  }, [
    comments.data?.thread_total,
    comments.data?.total,
    offset,
    socialAppearance,
  ])

  React.useEffect(() => {
    if (
      !socialAppearance ||
      !searchingAnchor ||
      !anchorCommentId ||
      !comments.data
    ) {
      return
    }
    const anchoredComment = comments.data.items.find(
      (comment) => comment.id === anchorCommentId
    )
    if (anchoredComment) {
      const rootId = anchoredComment.root_comment_id ?? anchoredComment.id
      setExpandedSocialThreads((current) => addSetValue(current, rootId))
      setSearchingAnchor(false)
      globalThis.requestAnimationFrame(() => {
        document
          .getElementById(`comment-${anchorCommentId}`)
          ?.scrollIntoView({ behavior: "smooth", block: "center" })
      })
      return
    }
    const threadTotal = comments.data.thread_total ?? 0
    if (offset + commentPageSize < threadTotal) {
      setOffset(offset + commentPageSize)
    } else {
      setSearchingAnchor(false)
    }
  }, [
    anchorCommentId,
    comments.data,
    offset,
    searchingAnchor,
    socialAppearance,
  ])

  function resetCreateAttempt() {
    createRequestId.current = undefined
    createComment.reset()
  }

  function changeDraft(value: string) {
    setDraft(value)
    resetCreateAttempt()
  }

  function beginReply(comment: Comment) {
    const rootId = comment.root_comment_id ?? comment.id
    setReplyTarget({ id: comment.id, author: comment.author, rootId })
    if (socialAppearance) {
      setExpandedSocialThreads((current) => addSetValue(current, rootId))
    }
    setEditingCommentId(undefined)
    updateComment.reset()
    if (socialAppearance) {
      setReplyDraft("")
      replyRequestId.current = undefined
      createReply.reset()
    } else {
      resetCreateAttempt()
    }
  }

  function cancelReply() {
    setReplyTarget(undefined)
    if (socialAppearance) {
      setReplyDraft("")
      replyRequestId.current = undefined
      createReply.reset()
    } else {
      resetCreateAttempt()
    }
  }

  function changeReplyDraft(value: string) {
    setReplyDraft(value)
    replyRequestId.current = undefined
    createReply.reset()
  }

  async function submitComment(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!session.data || !canCreate || !validCommentBody(draft)) {
      return
    }
    createRequestId.current ??= globalThis.crypto.randomUUID()
    const totalBeforeCreate = comments.data?.total ?? 0
    try {
      await createComment.mutateAsync({
        csrfToken: session.data.csrf_token,
        idempotencyKey: createRequestId.current,
        body: draft,
        parentCommentId: socialAppearance ? undefined : replyTarget?.id,
      })
      setDraft("")
      setReplyTarget(undefined)
      createRequestId.current = undefined
      if (socialAppearance) {
        setSocialCommentSort("newest")
        setOffset(0)
      } else {
        setOffset(lastPageOffset(totalBeforeCreate + 1, commentPageSize))
      }
    } catch {
      // Keep the request UUID while the unchanged draft remains retryable.
    }
  }

  async function submitSocialReply(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (
      !session.data ||
      !canCreate ||
      !replyTarget ||
      !validCommentBody(replyDraft)
    ) {
      return
    }
    replyRequestId.current ??= globalThis.crypto.randomUUID()
    try {
      await createReply.mutateAsync({
        csrfToken: session.data.csrf_token,
        idempotencyKey: replyRequestId.current,
        body: replyDraft,
        parentCommentId: replyTarget.id,
      })
      setReplyDraft("")
      setExpandedSocialThreads((current) =>
        addSetValue(current, replyTarget.rootId)
      )
      setReplyTarget(undefined)
      replyRequestId.current = undefined
    } catch {
      // Keep the request UUID while the unchanged reply remains retryable.
    }
  }

  async function confirmDelete() {
    if (!session.data || !deleteTarget || !canDeleteOwn) {
      return
    }
    try {
      await deleteComment.mutateAsync({
        commentId: deleteTarget.id,
        csrfToken: session.data.csrf_token,
        expectedVersion: deleteTarget.version,
      })
      setDeleteTarget(undefined)
    } catch {
      // Keep the dialog open so the stable problem can be shown and retried.
    }
  }

  function renderComment(comment: Comment) {
    const owned = session.data?.user.id === comment.author.id
    const replyTo =
      comment.reply_to ??
      comments.data?.items.find(
        (candidate) => candidate.id === comment.parent_comment_id
      )?.author
    return (
      <CommentRow
        key={comment.id}
        comment={comment}
        replyTo={replyTo ?? undefined}
        owned={owned}
        canReply={canCreate}
        canUpdate={owned && canUpdateOwn}
        canDelete={owned && canDeleteOwn}
        canReport={!owned && canReport}
        cardAppearance={announcementAppearance}
        absoluteTime={torrentAppearance || announcementAppearance}
        torrentAppearance={torrentAppearance}
        socialAppearance={socialAppearance}
        editing={editingCommentId === comment.id}
        replying={socialAppearance && replyTarget?.id === comment.id}
        replyDraft={replyDraft}
        replyPending={createReply.isPending}
        replyError={createReply.error}
        updatePending={updateComment.isPending}
        updateError={updateComment.error}
        csrfToken={session.data?.csrf_token}
        currentUserId={session.data?.user.id}
        onReply={() => beginReply(comment)}
        onReplyDraftChange={changeReplyDraft}
        onCancelReply={cancelReply}
        onSubmitReply={submitSocialReply}
        onBeginEdit={() => {
          setEditingCommentId(comment.id)
          setReplyTarget(undefined)
          setReplyDraft("")
          replyRequestId.current = undefined
          createReply.reset()
          updateComment.reset()
        }}
        onCancelEdit={() => {
          setEditingCommentId(undefined)
          updateComment.reset()
        }}
        onUpdate={async (body) => {
          if (!session.data) return
          try {
            await updateComment.mutateAsync({
              commentId: comment.id,
              csrfToken: session.data.csrf_token,
              expectedVersion: comment.version,
              body,
            })
            setEditingCommentId(undefined)
          } catch {
            // Keep the inline editor open for a safe correction.
          }
        }}
        onDelete={() => {
          deleteComment.reset()
          setDeleteTarget(comment)
        }}
        onReport={() => setReportTarget(comment)}
      />
    )
  }

  return (
    <Card
      id="comments"
      aria-labelledby={titleId}
      className={cn(
        torrentAppearance && "gap-0 py-0",
        announcementAppearance &&
          "mt-8 gap-0 rounded-none border-x-0 border-t border-b-0 bg-transparent py-0 pt-6 shadow-none ring-0",
        socialAppearance &&
          "gap-0 rounded-none border-0 bg-transparent py-0 shadow-none ring-0"
      )}
    >
      <CardHeader
        className={cn(
          torrentAppearance ? "p-6 pb-4" : "border-b",
          announcementAppearance && "border-0 p-0 pb-4",
          socialAppearance && "sr-only"
        )}
      >
        <CardTitle
          id={titleId}
          role={announcementAppearance ? "heading" : undefined}
          aria-level={announcementAppearance ? 3 : undefined}
          className={cn(
            torrentAppearance && "text-2xl leading-none font-semibold",
            announcementAppearance && "text-lg leading-7 font-semibold"
          )}
        >
          <span className="flex items-center gap-2">
            <MessageSquareIcon
              className={cn(
                torrentAppearance && "size-5",
                announcementAppearance && "size-5"
              )}
            />
            评论
            {(torrentAppearance || announcementAppearance) &&
            comments.data?.total ? (
              <span
                className={cn(
                  torrentAppearance &&
                    "text-sm font-normal text-muted-foreground"
                )}
              >
                ({comments.data.total.toLocaleString("zh-CN")})
              </span>
            ) : null}
          </span>
        </CardTitle>
        {announcementAppearance && comments.data?.total ? (
          <CardAction className="flex items-center gap-1">
            <Button
              type="button"
              variant={
                announcementCommentSort === "newest" ? "default" : "outline"
              }
              size="xs"
              className="h-7 px-2 text-xs"
              onClick={() => setAnnouncementCommentSort("newest")}
            >
              <ArrowDownIcon data-icon="inline-start" />
              最新
            </Button>
            <Button
              type="button"
              variant={
                announcementCommentSort === "oldest" ? "default" : "outline"
              }
              size="xs"
              className="h-7 px-2 text-xs"
              onClick={() => setAnnouncementCommentSort("oldest")}
            >
              <ArrowUpIcon data-icon="inline-start" />
              最早
            </Button>
          </CardAction>
        ) : null}
        {!torrentAppearance && !announcementAppearance && !socialAppearance ? (
          <CardDescription>{description}</CardDescription>
        ) : null}
        {!torrentAppearance &&
        !announcementAppearance &&
        !socialAppearance &&
        comments.data ? (
          <CardAction>
            <Badge variant="outline">
              {comments.data.total.toLocaleString("zh-CN")} 条
            </Badge>
          </CardAction>
        ) : null}
      </CardHeader>
      <CardContent
        className={cn(
          torrentAppearance
            ? "flex flex-col gap-4 px-6 pb-6"
            : "flex flex-col gap-5",
          announcementAppearance && "gap-6 p-0",
          socialAppearance && "gap-4 p-0"
        )}
      >
        <div
          className={cn(
            announcementAppearance && "rounded-lg border bg-card p-4 shadow-sm"
          )}
        >
          <CommentComposer
            fieldId={composerFieldId}
            placeholder={composerPlaceholder}
            sessionState={
              session.isPending
                ? "loading"
                : session.isError
                  ? "error"
                  : session.data
                    ? capabilities.isPending
                      ? "loading"
                      : capabilities.isError
                        ? "error"
                        : canCreate
                          ? "ready"
                          : "denied"
                    : "anonymous"
            }
            draft={draft}
            replyTarget={socialAppearance ? undefined : replyTarget}
            pending={createComment.isPending}
            error={createComment.error}
            compact={
              torrentAppearance || announcementAppearance || socialAppearance
            }
            torrentAppearance={torrentAppearance}
            announcementAppearance={announcementAppearance}
            socialAppearance={socialAppearance}
            onDraftChange={changeDraft}
            onCancelReply={cancelReply}
            onSubmit={submitComment}
          />
        </div>

        <section
          aria-label="评论列表"
          className={cn(
            torrentAppearance ? "" : "border-t pt-1",
            announcementAppearance && "order-first border-0 pt-0",
            socialAppearance && "border-0 pt-0"
          )}
        >
          {comments.isPending ? <CommentListSkeleton /> : null}
          {comments.isError ? (
            <Alert variant="destructive" className="mt-4">
              <CircleAlertIcon />
              <AlertTitle>评论暂时无法读取</AlertTitle>
              <AlertDescription>
                {requestErrorDescription(
                  comments.error,
                  "评论请求未能完成，请稍后重新加载。"
                )}
              </AlertDescription>
              <AlertAction>
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  onClick={() => void comments.refetch()}
                >
                  <RefreshCwIcon data-icon="inline-start" />
                  重试
                </Button>
              </AlertAction>
            </Alert>
          ) : null}
          {comments.data?.items.length === 0 &&
          (torrentAppearance || announcementAppearance) ? (
            <p className="py-4 text-center text-muted-foreground">
              暂无评论，来发表第一条评论吧
            </p>
          ) : null}
          {comments.data?.items.length === 0 &&
          !torrentAppearance &&
          !announcementAppearance &&
          !socialAppearance ? (
            <Empty className="mt-4">
              <EmptyHeader>
                <EmptyMedia variant="icon">
                  <MessageSquareIcon />
                </EmptyMedia>
                <EmptyTitle>还没有评论</EmptyTitle>
                <EmptyDescription>登录后可以发表第一条评论。</EmptyDescription>
              </EmptyHeader>
            </Empty>
          ) : null}
          {comments.data && socialAppearance ? (
            <div className="mb-1 flex flex-wrap items-center justify-between gap-2">
              <p className="text-sm text-muted-foreground">
                共 {comments.data.total.toLocaleString("zh-CN")} 条评论
              </p>
              <div
                role="group"
                aria-label="评论排序"
                className="flex items-center rounded-md border bg-background p-0.5"
              >
                {(
                  [
                    ["hot", "热门", FlameIcon],
                    ["newest", "最新", ArrowDownIcon],
                    ["oldest", "最早", ArrowUpIcon],
                  ] as const
                ).map(([value, label, Icon]) => (
                  <Button
                    key={value}
                    type="button"
                    variant={
                      socialCommentSort === value ? "secondary" : "ghost"
                    }
                    size="sm"
                    className="h-7 gap-1 px-2 text-xs shadow-none"
                    disabled={comments.isFetching}
                    aria-pressed={socialCommentSort === value}
                    onClick={() => {
                      setSocialCommentSort(value)
                      setOffset(0)
                    }}
                  >
                    <Icon className="size-3" />
                    {label}
                  </Button>
                ))}
              </div>
            </div>
          ) : null}
          {comments.data?.items.length === 0 && socialAppearance ? (
            <Empty className="mt-16 min-h-32 rounded-none border-0 p-0 py-8">
              <EmptyHeader className="w-full max-w-none gap-0">
                <EmptyMedia className="mb-2 text-muted-foreground/60 [&_svg]:size-8">
                  <MessageCircleIcon />
                </EmptyMedia>
                <EmptyDescription className="w-full text-base leading-6">
                  暂无评论，来发表第一条评论吧
                </EmptyDescription>
              </EmptyHeader>
            </Empty>
          ) : null}
          {comments.data && comments.data.items.length > 0 ? (
            <div
              className={cn(
                torrentAppearance
                  ? "flex flex-col gap-5 border-t pt-5"
                  : "divide-y",
                announcementAppearance && "flex flex-col gap-5 border-0 pt-0",
                socialAppearance && "border-t-0"
              )}
            >
              {socialAppearance
                ? socialCommentThreads(comments.data.items).map((thread) => {
                    const expanded = expandedSocialThreads.has(thread.root.id)
                    const visibleReplies = expanded
                      ? thread.replies
                      : thread.replies.slice(0, collapsedReplyPreviewCount)
                    return (
                      <div key={thread.root.id}>
                        {renderComment(thread.root)}
                        {visibleReplies.map((reply) => renderComment(reply))}
                        {thread.replies.length > collapsedReplyPreviewCount ? (
                          <Button
                            type="button"
                            variant="ghost"
                            size="sm"
                            className="mb-2 ml-11 h-7 gap-1 px-2 text-xs text-muted-foreground"
                            aria-expanded={expanded}
                            onClick={() =>
                              setExpandedSocialThreads((current) =>
                                toggleSetValue(current, thread.root.id)
                              )
                            }
                          >
                            {expanded ? (
                              <ChevronUpIcon className="size-3.5" />
                            ) : (
                              <ChevronDownIcon className="size-3.5" />
                            )}
                            {expanded
                              ? "收起回复"
                              : `查看全部 ${thread.replies.length.toLocaleString("zh-CN")} 条回复`}
                          </Button>
                        ) : null}
                      </div>
                    )
                  })
                : orderedComments(
                    comments.data.items,
                    announcementAppearance ? announcementCommentSort : "oldest"
                  ).map((comment) => renderComment(comment))}
            </div>
          ) : null}
          {comments.data ? (
            <OffsetPagination
              total={
                socialAppearance
                  ? (comments.data.thread_total ?? 0)
                  : comments.data.total
              }
              limit={comments.data.limit}
              offset={comments.data.offset}
              onOffsetChange={(nextOffset) => {
                setEditingCommentId(undefined)
                setOffset(nextOffset)
              }}
              ariaLabel="评论分页"
              buttonVariant="ghost"
              className={cn(
                torrentAppearance ? "border-t pt-3" : "border-t py-3",
                announcementAppearance && "mt-4",
                socialAppearance && "border-0 pt-4"
              )}
            />
          ) : null}
        </section>
      </CardContent>

      <AlertDialog
        open={Boolean(deleteTarget)}
        onOpenChange={(open) => {
          if (!open && !deleteComment.isPending) {
            setDeleteTarget(undefined)
            deleteComment.reset()
          }
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogMedia className="text-destructive">
              <CircleAlertIcon />
            </AlertDialogMedia>
            <AlertDialogTitle>删除这条评论？</AlertDialogTitle>
            <AlertDialogDescription>
              正文会被移除，但楼层会保留为“作者已删除”，已有回复不会丢失。
            </AlertDialogDescription>
          </AlertDialogHeader>
          {deleteComment.isError ? (
            <Alert variant="destructive">
              <CircleAlertIcon />
              <AlertTitle>评论未能删除</AlertTitle>
              <AlertDescription>
                {commentErrorMessage(deleteComment.error, "delete")}
              </AlertDescription>
            </Alert>
          ) : null}
          <AlertDialogFooter>
            <AlertDialogCancel disabled={deleteComment.isPending}>
              取消
            </AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              disabled={deleteComment.isPending}
              onClick={() => void confirmDelete()}
            >
              {deleteComment.isPending ? (
                <Spinner data-icon="inline-start" />
              ) : null}
              确认删除
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      {reportTarget && session.data ? (
        <CommentReportDialog
          key={reportTarget.id}
          comment={reportTarget}
          csrfToken={session.data.csrf_token}
          onOpenChange={(open) => {
            if (!open) setReportTarget(undefined)
          }}
        />
      ) : null}
    </Card>
  )
}

function CommentComposer({
  fieldId,
  placeholder,
  sessionState,
  draft,
  replyTarget,
  pending,
  error,
  compact,
  torrentAppearance,
  announcementAppearance,
  socialAppearance,
  onDraftChange,
  onCancelReply,
  onSubmit,
}: {
  fieldId: string
  placeholder: string
  sessionState: "loading" | "error" | "anonymous" | "denied" | "ready"
  draft: string
  replyTarget?: ReplyTarget
  pending: boolean
  error: Error | null
  compact: boolean
  torrentAppearance: boolean
  announcementAppearance: boolean
  socialAppearance: boolean
  onDraftChange: (value: string) => void
  onCancelReply: () => void
  onSubmit: (event: React.FormEvent<HTMLFormElement>) => void
}) {
  if (sessionState === "loading") {
    return <Skeleton className="h-28 w-full" aria-label="正在准备评论框" />
  }
  if (sessionState === "anonymous") {
    if (announcementAppearance) {
      return (
        <p className="py-1 text-center text-sm text-muted-foreground">
          <Link to="/login" className="text-primary hover:underline">
            登录
          </Link>{" "}
          后才能发表评论
        </p>
      )
    }
    return (
      <div className="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-dashed px-4 py-3">
        <p className="text-sm text-muted-foreground">登录后参与评论与回复。</p>
        <Link to="/login" className={buttonVariants({ size: "sm" })}>
          <LogInIcon data-icon="inline-start" />
          登录
        </Link>
      </div>
    )
  }
  if (sessionState === "error") {
    return (
      <p className="rounded-lg bg-muted px-4 py-3 text-sm text-muted-foreground">
        暂时无法确认登录或评论权限，公开评论仍可继续阅读。
      </p>
    )
  }
  if (sessionState === "denied") {
    return (
      <p className="rounded-lg bg-muted px-4 py-3 text-sm text-muted-foreground">
        当前账户暂时不能发表评论。
      </p>
    )
  }

  const count = characterCount(draft)
  const invalid = count > maxCommentCharacters
  if (socialAppearance) {
    return (
      <form onSubmit={onSubmit}>
        <FieldGroup className="gap-2">
          {replyTarget ? (
            <div className="flex items-center gap-2 rounded-lg bg-muted px-3 py-2 text-sm">
              <ReplyIcon className="text-muted-foreground" />
              <span className="min-w-0 flex-1 truncate">
                回复 <strong>@{replyTarget.author.display_name}</strong>
              </span>
              <Button
                type="button"
                variant="ghost"
                size="icon-xs"
                aria-label="取消回复"
                disabled={pending}
                onClick={onCancelReply}
              >
                <XIcon />
              </Button>
            </div>
          ) : null}
          <div className="flex items-end gap-2">
            <Field data-invalid={invalid || undefined} className="flex-1">
              <FieldLabel htmlFor={fieldId} className="sr-only">
                {replyTarget ? "写回复" : "发表评论"}
              </FieldLabel>
              <Textarea
                id={fieldId}
                value={draft}
                rows={2}
                maxLength={maxCommentCharacters + 1}
                aria-invalid={invalid || undefined}
                placeholder={
                  replyTarget
                    ? `回复 @${replyTarget.author.display_name}…`
                    : placeholder
                }
                disabled={pending}
                className="min-h-[60px] resize-none"
                onChange={(event) => onDraftChange(event.target.value)}
              />
              {invalid ? (
                <FieldError>评论不能超过 2000 个字符。</FieldError>
              ) : null}
              {error ? (
                <FieldError>{commentErrorMessage(error, "create")}</FieldError>
              ) : null}
            </Field>
            <Button
              type="submit"
              size="sm"
              className="h-10 w-[60px]"
              disabled={pending || !validCommentBody(draft)}
            >
              {pending ? <Spinner data-icon="inline-start" /> : null}
              {replyTarget ? "回复" : "发送"}
            </Button>
          </div>
        </FieldGroup>
      </form>
    )
  }
  return (
    <form onSubmit={onSubmit}>
      <FieldGroup className={compact ? "gap-0" : undefined}>
        {replyTarget ? (
          <div className="flex items-center gap-2 rounded-lg bg-muted px-3 py-2 text-sm">
            <ReplyIcon className="text-muted-foreground" />
            <span className="min-w-0 flex-1 truncate">
              回复 <strong>@{replyTarget.author.display_name}</strong>
            </span>
            <Button
              type="button"
              variant="ghost"
              size="icon-xs"
              aria-label="取消回复"
              disabled={pending}
              onClick={onCancelReply}
            >
              <XIcon />
            </Button>
          </div>
        ) : null}
        <Field data-invalid={invalid || undefined}>
          <FieldLabel
            htmlFor={fieldId}
            className={compact ? "sr-only" : undefined}
          >
            {replyTarget ? "写回复" : "发表评论"}
          </FieldLabel>
          <Textarea
            id={fieldId}
            value={draft}
            rows={3}
            maxLength={maxCommentCharacters + 1}
            aria-invalid={invalid || undefined}
            placeholder={
              replyTarget
                ? `回复 @${replyTarget.author.display_name}…`
                : placeholder
            }
            disabled={pending}
            className={cn(
              torrentAppearance && "min-h-[78px]",
              compact && !torrentAppearance && "min-h-[100px] resize-none"
            )}
            onChange={(event) => onDraftChange(event.target.value)}
          />
          <div className="flex flex-wrap items-center justify-between gap-2">
            <FieldDescription className={compact ? "text-xs" : undefined}>
              {compact ? null : "纯文本 · "}
              {compact
                ? `${count}/${maxCommentCharacters}`
                : `${count.toLocaleString("zh-CN")}/${maxCommentCharacters.toLocaleString("zh-CN")}`}
            </FieldDescription>
            <Button
              type="submit"
              size="sm"
              className={torrentAppearance ? "w-20" : undefined}
              disabled={pending || !validCommentBody(draft)}
            >
              {pending ? <Spinner data-icon="inline-start" /> : null}
              {compact && !torrentAppearance && !pending ? (
                <SendIcon data-icon="inline-start" />
              ) : null}
              {replyTarget ? "回复" : "发表评论"}
            </Button>
          </div>
          {invalid ? <FieldError>评论不能超过 2000 个字符。</FieldError> : null}
          {error ? (
            <FieldError>{commentErrorMessage(error, "create")}</FieldError>
          ) : null}
        </Field>
      </FieldGroup>
    </form>
  )
}

function CommentRow({
  comment,
  replyTo,
  owned,
  canReply,
  canUpdate,
  canDelete,
  canReport,
  cardAppearance = false,
  absoluteTime = false,
  torrentAppearance = false,
  socialAppearance = false,
  editing,
  replying,
  replyDraft,
  replyPending,
  replyError,
  updatePending,
  updateError,
  csrfToken,
  currentUserId,
  onReply,
  onReplyDraftChange,
  onCancelReply,
  onSubmitReply,
  onBeginEdit,
  onCancelEdit,
  onUpdate,
  onDelete,
  onReport,
}: {
  comment: Comment
  replyTo?: Comment["author"]
  owned: boolean
  canReply: boolean
  canUpdate: boolean
  canDelete: boolean
  canReport: boolean
  cardAppearance?: boolean
  absoluteTime?: boolean
  torrentAppearance?: boolean
  socialAppearance?: boolean
  editing: boolean
  replying: boolean
  replyDraft: string
  replyPending: boolean
  replyError: Error | null
  updatePending: boolean
  updateError: Error | null
  csrfToken?: string
  currentUserId?: string
  onReply: () => void
  onReplyDraftChange: (value: string) => void
  onCancelReply: () => void
  onSubmitReply: (event: React.FormEvent<HTMLFormElement>) => void
  onBeginEdit: () => void
  onCancelEdit: () => void
  onUpdate: (body: string) => Promise<void>
  onDelete: () => void
  onReport: () => void
}) {
  const isReply = Boolean(comment.parent_comment_id)
  const visible = comment.state === "visible"
  const actions = (
    <span
      className={cn(
        "flex flex-wrap items-center gap-1",
        socialAppearance ? "mt-2" : !torrentAppearance && "sm:ml-auto"
      )}
    >
      {visible && canReply ? (
        <Button
          type="button"
          variant="ghost"
          size="xs"
          className={cn(
            socialAppearance && "h-7 px-2",
            torrentAppearance && "h-auto px-0 py-0 text-xs font-normal"
          )}
          onClick={onReply}
        >
          {torrentAppearance ? null : <ReplyIcon data-icon="inline-start" />}
          回复
        </Button>
      ) : null}
      {visible && canUpdate ? (
        <Button
          type="button"
          variant="ghost"
          size="xs"
          className={cn(
            socialAppearance && "h-7 px-2",
            torrentAppearance && "h-auto px-0 py-0 text-xs font-normal"
          )}
          disabled={updatePending}
          onClick={onBeginEdit}
        >
          {torrentAppearance ? null : <PencilIcon data-icon="inline-start" />}
          编辑
        </Button>
      ) : null}
      {visible && canDelete ? (
        <Button
          type="button"
          variant="ghost"
          size="xs"
          className={cn(
            socialAppearance && "h-7 px-2",
            torrentAppearance && "h-auto px-0 py-0 text-xs font-normal"
          )}
          disabled={updatePending}
          onClick={onDelete}
        >
          {torrentAppearance ? null : <Trash2Icon data-icon="inline-start" />}
          删除
        </Button>
      ) : null}
      {visible && canReport ? (
        <Button
          type="button"
          variant="ghost"
          size="xs"
          className={cn(
            socialAppearance && "h-7 px-2",
            torrentAppearance && "h-auto px-0 py-0 text-xs font-normal"
          )}
          disabled={updatePending}
          onClick={onReport}
        >
          {torrentAppearance ? null : <FlagIcon data-icon="inline-start" />}
          举报
        </Button>
      ) : null}
      {visible && !owned ? (
        <ContentTipDialog
          target={{
            kind: "comment",
            commentId: comment.id,
            title: comment.body,
          }}
          userId={currentUserId}
          csrfToken={csrfToken}
          buttonVariant="ghost"
          buttonSize="xs"
          className={cn(
            socialAppearance && "h-7 px-2",
            torrentAppearance && "h-auto px-0 py-0 text-xs font-normal"
          )}
        />
      ) : null}
    </span>
  )
  return (
    <article
      id={`comment-${comment.id}`}
      className={cn(
        "scroll-mt-24 transition-colors target:rounded-lg target:bg-primary/5 target:ring-1 target:ring-primary/20",
        isReply
          ? socialAppearance
            ? "ml-8 flex gap-3 border-l-2 border-muted px-3 py-3 sm:ml-11"
            : cn(
                "ml-8 flex gap-3 border-l-2 px-3 sm:ml-11",
                torrentAppearance ? "py-0" : "py-4"
              )
          : cn("flex gap-3", torrentAppearance ? "py-0" : "py-4"),
        cardAppearance && "rounded-lg border bg-muted/50 px-4 shadow-sm",
        cardAppearance && isReply && "bg-muted/30"
      )}
    >
      <Link
        to={`/user/${encodeURIComponent(comment.author.username)}`}
        className="h-fit shrink-0 rounded-full focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
      >
        <UserAvatar
          username={comment.author.username}
          displayName={comment.author.display_name}
          colorSeed={comment.author.id}
          online={comment.author.online}
          size={torrentAppearance ? "default" : isReply ? "sm" : "default"}
          className={cn(torrentAppearance && (isReply ? "size-8" : "size-10"))}
        />
      </Link>
      <div className="min-w-0 flex-1">
        <header className="flex flex-wrap items-center gap-x-2 gap-y-1">
          <span className="flex max-w-full min-w-0 flex-wrap items-center gap-1.5">
            <Link
              to={`/user/${encodeURIComponent(comment.author.username)}`}
              className={cn(
                "min-w-0 truncate font-semibold hover:underline hover:underline-offset-4",
                comment.author.administrator
                  ? "bg-gradient-to-r from-fuchsia-500 via-primary to-amber-500 bg-clip-text text-transparent hover:opacity-80"
                  : comment.author.vip
                    ? "text-amber-600 hover:text-amber-700 dark:text-amber-400 dark:hover:text-amber-300"
                    : "hover:text-primary"
              )}
            >
              {comment.author.display_name}
            </Link>
            {comment.author.administrator ? (
              <Badge className="h-4 shrink-0 border-0 bg-gradient-to-r from-fuchsia-500/15 via-primary/15 to-amber-500/15 px-1.5 text-[10px] font-semibold text-fuchsia-700 shadow-none dark:text-fuchsia-300">
                管理员
              </Badge>
            ) : null}
            {comment.author.vip ? (
              <Badge className="h-4 shrink-0 border border-amber-400/40 bg-amber-400/10 px-1.5 text-[10px] font-bold text-amber-700 shadow-none dark:text-amber-300">
                VIP
              </Badge>
            ) : null}
            {(comment.author.medals ?? []).map((medal) => (
              <span
                key={medal.id}
                className="flex size-4 shrink-0 items-center justify-center"
                title={medal.name}
                aria-label={`勋章：${medal.name}`}
              >
                {medal.image_path ? (
                  <img
                    src={medal.image_path}
                    alt=""
                    className="max-h-full max-w-full object-contain"
                  />
                ) : (
                  <SparklesIcon
                    className="size-3.5 text-amber-500"
                    aria-hidden="true"
                  />
                )}
              </span>
            ))}
          </span>
          {isReply ? (
            <span className="text-xs text-muted-foreground">
              {replyTo ? `回复 @${replyTo.display_name}` : "回复"}
            </span>
          ) : null}
          <time
            dateTime={comment.created_at}
            title={formatDateTime(comment.created_at)}
            className="text-xs text-muted-foreground"
          >
            {absoluteTime
              ? formatCompactDateTime(comment.created_at)
              : formatRelativeTime(comment.created_at)}
          </time>
          {comment.edited_at ? (
            <span className="text-xs text-muted-foreground">已编辑</span>
          ) : null}
          {owned && !torrentAppearance ? (
            <Badge variant="outline">我</Badge>
          ) : null}
          {socialAppearance ? null : actions}
        </header>

        {editing && visible && csrfToken ? (
          <CommentEditForm
            fieldId={`edit-comment-${comment.id}`}
            initialBody={comment.body}
            pending={updatePending}
            error={updateError}
            onCancel={onCancelEdit}
            onSubmit={onUpdate}
          />
        ) : (
          <CommentBody comment={comment} collapsible={socialAppearance} />
        )}
        {socialAppearance && !editing ? actions : null}
        {socialAppearance && replying && visible && !editing ? (
          <InlineReplyComposer
            comment={comment}
            draft={replyDraft}
            pending={replyPending}
            error={replyError}
            onDraftChange={onReplyDraftChange}
            onCancel={onCancelReply}
            onSubmit={onSubmitReply}
          />
        ) : null}
      </div>
    </article>
  )
}

function InlineReplyComposer({
  comment,
  draft,
  pending,
  error,
  onDraftChange,
  onCancel,
  onSubmit,
}: {
  comment: Comment
  draft: string
  pending: boolean
  error: Error | null
  onDraftChange: (value: string) => void
  onCancel: () => void
  onSubmit: (event: React.FormEvent<HTMLFormElement>) => void
}) {
  const count = characterCount(draft)
  const invalid = count > maxCommentCharacters
  const fieldId = `reply-comment-${comment.id}`
  return (
    <form
      className="mt-2 rounded-lg border bg-muted/30 p-3"
      onSubmit={onSubmit}
    >
      <Field data-invalid={invalid || undefined} className="gap-2">
        <div className="flex items-center justify-between gap-2 text-xs text-muted-foreground">
          <FieldLabel
            htmlFor={fieldId}
            className="flex min-w-0 items-center gap-1.5 font-normal"
          >
            <ReplyIcon className="size-3.5 shrink-0" />
            <span className="truncate">
              回复 <strong>@{comment.author.display_name}</strong>
            </span>
          </FieldLabel>
          <Button
            type="button"
            variant="ghost"
            size="icon-xs"
            aria-label="取消回复"
            disabled={pending}
            onClick={onCancel}
          >
            <XIcon />
          </Button>
        </div>
        <Textarea
          id={fieldId}
          value={draft}
          rows={3}
          maxLength={maxCommentCharacters + 1}
          aria-invalid={invalid || undefined}
          placeholder={`回复 @${comment.author.display_name}…`}
          disabled={pending}
          autoFocus
          className="min-h-[76px] resize-y bg-background"
          onChange={(event) => onDraftChange(event.target.value)}
        />
        <div className="flex items-center justify-between gap-3">
          <FieldDescription className="text-xs">
            {count}/{maxCommentCharacters}
          </FieldDescription>
          <div className="flex items-center gap-2">
            <Button
              type="button"
              variant="ghost"
              size="sm"
              disabled={pending}
              onClick={onCancel}
            >
              取消
            </Button>
            <Button
              type="submit"
              size="sm"
              disabled={pending || !validCommentBody(draft)}
            >
              {pending ? <Spinner data-icon="inline-start" /> : null}
              回复
            </Button>
          </div>
        </div>
        {invalid ? <FieldError>回复不能超过 2000 个字符。</FieldError> : null}
        {error ? (
          <FieldError>{commentErrorMessage(error, "create")}</FieldError>
        ) : null}
      </Field>
    </form>
  )
}

function CommentEditForm({
  fieldId,
  initialBody,
  pending,
  error,
  onCancel,
  onSubmit,
}: {
  fieldId: string
  initialBody: string
  pending: boolean
  error: Error | null
  onCancel: () => void
  onSubmit: (body: string) => Promise<void>
}) {
  const [body, setBody] = React.useState(initialBody)
  const count = characterCount(body)
  const invalid = count > maxCommentCharacters
  return (
    <form
      className="mt-2"
      onSubmit={(event) => {
        event.preventDefault()
        if (validCommentBody(body)) void onSubmit(body)
      }}
    >
      <Field data-invalid={invalid || undefined}>
        <FieldLabel className="sr-only" htmlFor={fieldId}>
          编辑评论
        </FieldLabel>
        <Textarea
          id={fieldId}
          value={body}
          rows={3}
          maxLength={maxCommentCharacters + 1}
          aria-invalid={invalid || undefined}
          disabled={pending}
          onChange={(event) => setBody(event.target.value)}
        />
        <div className="flex flex-wrap items-center justify-between gap-2">
          <FieldDescription>
            {count.toLocaleString("zh-CN")}/
            {maxCommentCharacters.toLocaleString("zh-CN")}
          </FieldDescription>
          <div className="flex items-center gap-2">
            <Button
              type="button"
              variant="ghost"
              size="sm"
              disabled={pending}
              onClick={onCancel}
            >
              取消
            </Button>
            <Button
              type="submit"
              size="sm"
              disabled={
                pending ||
                !validCommentBody(body) ||
                body.trim() === initialBody.trim()
              }
            >
              {pending ? <Spinner data-icon="inline-start" /> : null}
              保存
            </Button>
          </div>
        </div>
        {invalid ? <FieldError>评论不能超过 2000 个字符。</FieldError> : null}
        {error ? (
          <FieldError>{commentErrorMessage(error, "update")}</FieldError>
        ) : null}
      </Field>
    </form>
  )
}

function CommentBody({
  comment,
  collapsible = false,
}: {
  comment: Comment
  collapsible?: boolean
}) {
  const [expanded, setExpanded] = React.useState(false)
  const characters = Array.from(comment.body)
  const shouldCollapse =
    collapsible && characters.length > collapsedCommentLength
  const visibleBody =
    shouldCollapse && !expanded
      ? characters.slice(0, collapsedCommentLength).join("")
      : comment.body

  React.useEffect(() => setExpanded(false), [comment.body])

  if (comment.state === "author_deleted") {
    return (
      <p className="mt-2 text-sm text-muted-foreground italic">
        该评论已由作者删除。
      </p>
    )
  }
  if (comment.state === "moderator_hidden") {
    return (
      <p className="mt-2 text-sm text-muted-foreground italic">
        该评论已被管理人员隐藏。
      </p>
    )
  }
  return (
    <div className="mt-2 flex flex-col gap-1">
      <p className="text-sm leading-relaxed break-words whitespace-pre-wrap">
        {visibleBody}
        {shouldCollapse && !expanded ? (
          <span className="text-muted-foreground">...</span>
        ) : null}
        {shouldCollapse ? (
          <Button
            type="button"
            variant="link"
            size="xs"
            className="ml-1 h-auto p-0 align-baseline text-xs"
            onClick={() => setExpanded((value) => !value)}
          >
            {expanded ? "收起" : "展开"}
          </Button>
        ) : null}
      </p>
      {comment.body_format === "legacy_bbcode" ? (
        <span className="w-fit text-xs text-muted-foreground">
          旧版格式内容
        </span>
      ) : null}
    </div>
  )
}

function CommentListSkeleton() {
  return (
    <div
      className="flex flex-col gap-4 py-4"
      aria-label="正在加载评论"
      aria-busy="true"
    >
      {Array.from({ length: 3 }, (_, index) => (
        <div key={index} className="flex gap-3">
          <Skeleton className="size-8 rounded-full" />
          <div className="flex flex-1 flex-col gap-2">
            <Skeleton className="h-4 w-36" />
            <Skeleton className="h-12 w-full" />
          </div>
        </div>
      ))}
    </div>
  )
}

function validCommentBody(value: string) {
  const count = characterCount(value.trim())
  return count > 0 && count <= maxCommentCharacters
}

function socialCommentThreads(comments: Comment[]) {
  const byID = new Map(comments.map((comment) => [comment.id, comment]))
  const threads: Array<{ root: Comment; replies: Comment[] }> = []
  const byRootID = new Map<string, (typeof threads)[number]>()

  for (const comment of comments) {
    if (comment.parent_comment_id) continue
    const thread = { root: comment, replies: [] as Comment[] }
    threads.push(thread)
    byRootID.set(comment.id, thread)
  }

  for (const comment of comments) {
    if (!comment.parent_comment_id) continue
    const rootID =
      comment.root_comment_id ?? resolveVisibleRootID(comment, byID)
    const thread = rootID ? byRootID.get(rootID) : undefined
    if (thread) {
      thread.replies.push(comment)
      continue
    }
    // Keep a malformed rolling-deploy response visible. The current API
    // always includes the owning root, so this is only a compatibility guard.
    const fallback = { root: comment, replies: [] as Comment[] }
    threads.push(fallback)
    byRootID.set(comment.id, fallback)
  }

  for (const thread of threads) {
    thread.replies.sort((left, right) =>
      left.created_at.localeCompare(right.created_at)
    )
  }
  return threads
}

function resolveVisibleRootID(
  comment: Comment,
  byID: Map<string, Comment>
): string | undefined {
  const visited = new Set<string>([comment.id])
  let current = comment
  while (current.parent_comment_id) {
    if (visited.has(current.parent_comment_id)) return undefined
    visited.add(current.parent_comment_id)
    const parent = byID.get(current.parent_comment_id)
    if (!parent) return undefined
    current = parent
  }
  return current.id
}

function addSetValue(current: Set<string>, value: string) {
  if (current.has(value)) return current
  const next = new Set(current)
  next.add(value)
  return next
}

function toggleSetValue(current: Set<string>, value: string) {
  const next = new Set(current)
  if (next.has(value)) {
    next.delete(value)
  } else {
    next.add(value)
  }
  return next
}

// The API returns comments in chronological order and keeps replies adjacent
// to their parent. Sort top-level threads as units so choosing “最新” never
// moves a reply above the comment it belongs to.
function orderedComments(comments: Comment[], order: "newest" | "oldest") {
  const commentIds = new Set(comments.map((comment) => comment.id))
  const commentsByParent = new Map<string, Comment[]>()
  const roots: Comment[] = []
  for (const comment of comments) {
    // Pagination may place a reply on a page without its parent. Treat that
    // reply as a visible root for this page rather than silently dropping it.
    if (
      !comment.parent_comment_id ||
      !commentIds.has(comment.parent_comment_id)
    ) {
      roots.push(comment)
      continue
    }
    const replies = commentsByParent.get(comment.parent_comment_id) ?? []
    replies.push(comment)
    commentsByParent.set(comment.parent_comment_id, replies)
  }
  roots.sort((left, right) =>
    order === "newest"
      ? right.created_at.localeCompare(left.created_at)
      : left.created_at.localeCompare(right.created_at)
  )
  for (const replies of commentsByParent.values()) {
    replies.sort((left, right) =>
      left.created_at.localeCompare(right.created_at)
    )
  }

  const ordered: Comment[] = []
  const visited = new Set<string>()
  function appendThread(comment: Comment) {
    if (visited.has(comment.id)) return
    visited.add(comment.id)
    ordered.push(comment)
    for (const reply of commentsByParent.get(comment.id) ?? []) {
      appendThread(reply)
    }
  }
  for (const root of roots) appendThread(root)
  // Malformed historic cycles must remain visible instead of disappearing.
  for (const comment of comments) appendThread(comment)
  return ordered
}

function characterCount(value: string) {
  return Array.from(value).length
}

function lastPageOffset(total: number, limit: number) {
  return Math.max(0, Math.floor((Math.max(total, 1) - 1) / limit) * limit)
}

function commentErrorMessage(
  error: Error,
  action: "create" | "update" | "delete"
) {
  if (!(error instanceof ApiProblemError)) {
    return "网络连接异常，请稍后重试。"
  }
  switch (error.code) {
    case "csrf_invalid":
    case "session_required":
      return "登录状态已经变化，请刷新页面后重试。"
    case "comment_parent_not_found":
      return "原评论已经删除或不能继续回复，请刷新评论区。"
    case "comment_version_conflict":
      return "评论已经发生变化，请刷新评论区后再操作。"
    case "comment_thread_locked":
      return "该内容的评论区目前不接受新评论。"
    case "comment_target_not_found":
    case "torrent_not_found":
    case "announcement_not_found":
      return "该内容已经停止公开，暂时不能继续评论。"
    case "idempotency_conflict":
      return "这次提交与先前请求冲突，请修改正文后重试。"
    default:
      return action === "create"
        ? "评论未能发表，请稍后重试。"
        : action === "update"
          ? "评论未能保存，请稍后重试。"
          : "评论未能删除，请稍后重试。"
  }
}

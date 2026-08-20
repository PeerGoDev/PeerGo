import * as React from "react"
import { Link } from "react-router"
import {
  CircleAlertIcon,
  HeartIcon,
  MessageCircleIcon,
  MoreHorizontalIcon,
  PencilIcon,
  Repeat2Icon,
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
import { Alert, AlertDescription, AlertTitle } from "~/components/ui/alert"
import { Button } from "~/components/ui/button"
import { Separator } from "~/components/ui/separator"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "~/components/ui/dropdown-menu"
import { Spinner } from "~/components/ui/spinner"
import { Textarea } from "~/components/ui/textarea"
import { ContentTipDialog } from "~/features/economy/components/content-tip-dialog"
import {
  type SocialPost,
  useDeleteSocialPost,
  useUpdateSocialPost,
} from "~/features/social/api/posts.queries"
import { formatRelativeTime } from "~/features/torrent/model/format"
import { UserAvatar } from "~/shared/components/user-avatar"
import { ApiProblemError } from "~/shared/api/problem"
import { formatDateTime } from "~/shared/formatters/date-time"
import { cn } from "~/lib/utils"

const collapsedPostLength = 300

export function SocialPostCard({
  post,
  currentUserId,
  csrfToken,
  linkToDetail = true,
  compact = false,
  onDeleted,
}: {
  post: SocialPost
  currentUserId?: string
  csrfToken?: string
  linkToDetail?: boolean
  compact?: boolean
  onDeleted?: () => void
}) {
  const [editing, setEditing] = React.useState(false)
  const [draft, setDraft] = React.useState(post.content)
  const [confirmDelete, setConfirmDelete] = React.useState(false)
  const updatePost = useUpdateSocialPost(post.id)
  const deletePost = useDeleteSocialPost()
  const isOwner = currentUserId === post.author.id

  async function saveEdit() {
    if (!csrfToken || !draft.trim()) return
    try {
      await updatePost.mutateAsync({
        content: draft.trim(),
        expectedVersion: post.version,
        csrfToken,
      })
      setEditing(false)
    } catch {
      // Keep the editor open with the stable problem available below.
    }
  }

  async function removePost() {
    if (!csrfToken) return
    try {
      await deletePost.mutateAsync({
        postId: post.id,
        expectedVersion: post.version,
        csrfToken,
      })
      setConfirmDelete(false)
      onDeleted?.()
    } catch {
      // Keep the confirmation open for a safe retry.
    }
  }

  const content = editing ? (
    <div className="mt-3 flex flex-col gap-3">
      <Textarea
        value={draft}
        onChange={(event) => setDraft(event.target.value)}
        maxLength={2_000}
        className="min-h-28 resize-y"
        aria-label="编辑动态正文"
      />
      {updatePost.error ? (
        <ProblemAlert title="编辑失败" error={updatePost.error} />
      ) : null}
      <div className="flex justify-end gap-2">
        <Button
          variant="ghost"
          size="sm"
          onClick={() => {
            setDraft(post.content)
            setEditing(false)
            updatePost.reset()
          }}
        >
          取消
        </Button>
        <Button
          size="sm"
          onClick={saveEdit}
          disabled={!draft.trim() || updatePost.isPending}
        >
          {updatePost.isPending ? <Spinner data-icon="inline-start" /> : null}
          保存
        </Button>
      </div>
    </div>
  ) : (
    <PostContent content={post.content} compact={compact} />
  )

  return (
    <article
      className={cn(
        "relative overflow-hidden rounded-lg border bg-card",
        compact ? "p-3" : "p-4"
      )}
    >
      <div className="flex items-start justify-between gap-3">
        <div className="flex min-w-0 items-center gap-3">
          <Link to={`/user/${encodeURIComponent(post.author.username)}`}>
            <UserAvatar
              username={post.author.username}
              displayName={post.author.display_name}
              className="size-10"
            />
          </Link>
          <div className="min-w-0">
            <Link
              to={`/user/${encodeURIComponent(post.author.username)}`}
              className="block truncate text-base hover:text-primary hover:underline hover:underline-offset-4"
            >
              {post.author.display_name}
            </Link>
            <div className="flex items-center gap-2 text-xs text-muted-foreground">
              <span title={formatDateTime(post.created_at)}>
                {formatRelativeTime(post.created_at)}
              </span>
              {post.edited_at ? <span>· 已编辑</span> : null}
            </div>
          </div>
        </div>

        {isOwner ? (
          <DropdownMenu>
            <DropdownMenuTrigger
              render={
                <Button variant="ghost" size="icon-sm" aria-label="动态操作" />
              }
            >
              <MoreHorizontalIcon data-icon="inline-start" />
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="w-32">
              <DropdownMenuItem
                onClick={() => {
                  setDraft(post.content)
                  setEditing(true)
                }}
              >
                <PencilIcon />
                编辑
              </DropdownMenuItem>
              <DropdownMenuItem
                variant="destructive"
                onClick={() => setConfirmDelete(true)}
              >
                <Trash2Icon />
                删除
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        ) : null}
      </div>

      {content}

      <div className="mt-3">
        <Separator />
        <div className="mt-3 flex items-center justify-between">
          <div className="flex items-center gap-1">
            <UnavailablePostAction label="点赞" title="动态点赞尚未开放">
              <HeartIcon data-icon="inline-start" />
            </UnavailablePostAction>
            {linkToDetail ? (
              <Button
                render={<Link to={`/social/post/${post.id}`} />}
                nativeButton={false}
                variant="ghost"
                size="default"
                className="h-9 gap-1.5 border-0 px-3"
              >
                <MessageCircleIcon data-icon="inline-start" />
                {post.comment_count > 0 ? post.comment_count : "评论"}
              </Button>
            ) : (
              <span className="flex h-9 shrink-0 items-center gap-1.5 px-3 text-sm whitespace-nowrap text-muted-foreground">
                <MessageCircleIcon className="size-4" />
                {post.comment_count > 0 ? post.comment_count : "评论"}
              </span>
            )}
            <UnavailablePostAction label="转发" title="动态转发尚未开放">
              <Repeat2Icon data-icon="inline-start" />
            </UnavailablePostAction>
            {!isOwner ? (
              <ContentTipDialog
                target={{ kind: "post", postId: post.id, title: post.content }}
                userId={currentUserId}
                csrfToken={csrfToken}
                buttonVariant="ghost"
                buttonSize="default"
                className="h-9 gap-1.5 border-0 px-3"
              />
            ) : null}
          </div>
          <UnavailablePostAction
            label="更多互动"
            title="更多互动尚未开放"
            iconOnly
          >
            <MoreHorizontalIcon data-icon="inline-start" />
          </UnavailablePostAction>
        </div>
      </div>

      <AlertDialog open={confirmDelete} onOpenChange={setConfirmDelete}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogMedia className="text-destructive">
              <CircleAlertIcon />
            </AlertDialogMedia>
            <AlertDialogTitle>删除动态</AlertDialogTitle>
            <AlertDialogDescription>
              删除后动态将不再公开，且不能恢复。评论审核证据仍会保留。
            </AlertDialogDescription>
          </AlertDialogHeader>
          {deletePost.error ? (
            <ProblemAlert title="删除失败" error={deletePost.error} />
          ) : null}
          <AlertDialogFooter>
            <AlertDialogCancel disabled={deletePost.isPending}>
              取消
            </AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              onClick={removePost}
              disabled={deletePost.isPending}
            >
              {deletePost.isPending ? (
                <Spinner data-icon="inline-start" />
              ) : null}
              删除
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </article>
  )
}

function PostContent({
  content,
  compact,
}: {
  content: string
  compact: boolean
}) {
  const [expanded, setExpanded] = React.useState(false)
  const characters = Array.from(content)
  const collapsible = characters.length > collapsedPostLength
  const visibleContent =
    collapsible && !expanded
      ? characters.slice(0, collapsedPostLength).join("")
      : content

  React.useEffect(() => setExpanded(false), [content])

  return (
    <p
      className={cn(
        "leading-6 break-words whitespace-pre-wrap",
        compact ? "mt-2 text-sm" : "mt-3 text-base"
      )}
    >
      {renderPostContent(visibleContent)}
      {collapsible && !expanded ? (
        <span className="text-muted-foreground">...</span>
      ) : null}
      {collapsible ? (
        <Button
          type="button"
          variant="link"
          size="sm"
          className="ml-1 h-auto p-0 align-baseline"
          onClick={() => setExpanded((value) => !value)}
        >
          {expanded ? "收起" : "展开全文"}
        </Button>
      ) : null}
    </p>
  )
}

function UnavailablePostAction({
  label,
  title,
  iconOnly = false,
  children,
}: {
  label: string
  title: string
  iconOnly?: boolean
  children: React.ReactNode
}) {
  return (
    <Button
      type="button"
      variant="ghost"
      size={iconOnly ? "icon-sm" : "default"}
      className={iconOnly ? "border-0" : "h-9 gap-1.5 border-0 px-3"}
      aria-label={iconOnly ? label : undefined}
      title={title}
      aria-disabled="true"
    >
      {children}
      {iconOnly ? null : label}
    </Button>
  )
}

function renderPostContent(content: string) {
  const parts = content.split(
    /(https?:\/\/[^\s]+|\/torrents\/[1-9]\d*|#[\w\u3400-\u9fff]+|@[a-zA-Z0-9_]+)/g
  )
  return parts.map((part, index) => {
    if (part.startsWith("http://") || part.startsWith("https://")) {
      return (
        <a
          key={`${part}-${index}`}
          href={part}
          target="_blank"
          rel="noreferrer"
          className="text-info hover:underline"
        >
          {part}
        </a>
      )
    }
    if (part.startsWith("/torrents/")) {
      return (
        <Link
          key={`${part}-${index}`}
          to={part}
          className="text-info hover:underline"
        >
          {part}
        </Link>
      )
    }
    if (part.startsWith("#")) {
      return (
        <span key={`${part}-${index}`} className="text-info">
          {part}
        </span>
      )
    }
    if (part.startsWith("@")) {
      return (
        <Link
          key={`${part}-${index}`}
          to={`/user/${encodeURIComponent(part.slice(1))}`}
          className="text-info hover:underline"
        >
          {part}
        </Link>
      )
    }
    return part
  })
}

function ProblemAlert({ title, error }: { title: string; error: Error }) {
  return (
    <Alert variant="destructive">
      <AlertTitle>{title}</AlertTitle>
      <AlertDescription>
        {error instanceof ApiProblemError
          ? error.message
          : "暂时无法完成操作，请稍后重试。"}
      </AlertDescription>
    </Alert>
  )
}

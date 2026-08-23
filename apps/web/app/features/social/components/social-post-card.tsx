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
  GiftIcon,
  PinIcon,
  SparklesIcon,
  UserPlusIcon,
  UserCheckIcon,
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
import { Badge } from "~/components/ui/badge"
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
  useClaimSocialRedPacket,
  useSocialFollow,
  useSocialPollVote,
  useSocialPostLike,
  useSocialPostRepost,
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
  const likePost = useSocialPostLike()
  const repostPost = useSocialPostRepost()
  const follow = useSocialFollow()
  const vote = useSocialPollVote()
  const claimPacket = useClaimSocialRedPacket()
  const isOwner = currentUserId === post.author.id
  const topics = post.topics ?? []
  const media = post.media ?? []

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
          {post.board ? (
            <Badge variant="outline" className="border-primary/20 text-primary">
              {post.board.name}
            </Badge>
          ) : null}
        </div>

        <div className="flex items-center gap-1">
          {!isOwner && csrfToken ? (
            <Button
              type="button"
              variant="ghost"
              size="sm"
              onClick={() =>
                follow.mutate({
                  username: post.author.username,
                  active: !post.author.followed_by_me,
                  csrfToken,
                })
              }
              disabled={follow.isPending}
            >
              {post.author.followed_by_me ? (
                <UserCheckIcon data-icon="inline-start" />
              ) : (
                <UserPlusIcon data-icon="inline-start" />
              )}
              {post.author.followed_by_me ? "已关注" : "关注"}
            </Button>
          ) : null}
          {isOwner ? (
            <DropdownMenu>
              <DropdownMenuTrigger
                render={
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    aria-label="动态操作"
                  />
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
      </div>

      {content}

      {topics.length > 0 ? (
        <div className="mt-2 flex flex-wrap gap-1.5">
          {topics.map((topic) => (
            <Badge key={topic} variant="secondary">
              #{topic}
            </Badge>
          ))}
        </div>
      ) : null}

      {media.length > 0 ? (
        <div
          className={cn(
            "mt-3 grid gap-2 overflow-hidden rounded-md",
            media.length === 1 ? "grid-cols-1" : "grid-cols-2"
          )}
        >
          {media.map((item) => (
            <a
              key={item.id}
              href={item.url}
              target="_blank"
              rel="noreferrer"
              className="overflow-hidden rounded-md bg-muted"
            >
              <img
                src={item.url}
                alt="动态图片"
                loading="lazy"
                className={cn(
                  "w-full object-cover",
                  media.length === 1 ? "max-h-[420px]" : "aspect-video"
                )}
              />
            </a>
          ))}
        </div>
      ) : null}

      {post.poll ? (
        <div className="mt-3 space-y-2 rounded-md border bg-muted/20 p-3">
          <p className="text-sm font-medium">{post.poll.question}</p>
          {post.poll.options.map((option) => {
            const percent =
              post.poll && post.poll.total_votes > 0
                ? Math.round((option.vote_count * 100) / post.poll.total_votes)
                : 0
            const selected = post.poll?.selected_option_id === option.id
            return (
              <Button
                key={option.id}
                type="button"
                variant={selected ? "secondary" : "outline"}
                className="relative w-full justify-between overflow-hidden"
                disabled={!csrfToken || post.poll?.closed || vote.isPending}
                onClick={() =>
                  csrfToken &&
                  vote.mutate({
                    postId: post.id,
                    optionId: option.id,
                    csrfToken,
                  })
                }
              >
                <span
                  className="absolute inset-y-0 left-0 bg-primary/10"
                  style={{ width: `${percent}%` }}
                />
                <span className="relative">{option.label}</span>
                <span className="relative text-muted-foreground">
                  {percent}%
                </span>
              </Button>
            )
          })}
          <p className="text-xs text-muted-foreground">
            {post.poll.total_votes} 人参与{post.poll.closed ? " · 已结束" : ""}
          </p>
        </div>
      ) : null}

      {post.red_packet ? (
        <div className="mt-3 flex items-center justify-between rounded-md border border-primary/20 bg-primary/5 p-3">
          <div className="flex items-center gap-3">
            <span className="flex size-9 items-center justify-center rounded-full bg-primary text-primary-foreground">
              <GiftIcon className="size-4" />
            </span>
            <div>
              <p className="text-sm font-medium">魔力值红包</p>
              <p className="text-xs text-muted-foreground">
                剩余 {post.red_packet.remaining_amount} /{" "}
                {post.red_packet.total_amount} ·{" "}
                {post.red_packet.remaining_claims} 份
              </p>
            </div>
          </div>
          {post.red_packet.claimed_by_me ? (
            <Badge variant="secondary">
              已领 {post.red_packet.my_claim_amount}
            </Badge>
          ) : !isOwner ? (
            <Button
              type="button"
              size="sm"
              onClick={() =>
                csrfToken &&
                claimPacket.mutate({
                  postId: post.id,
                  csrfToken,
                  idempotencyKey: crypto.randomUUID(),
                })
              }
              disabled={
                !csrfToken ||
                post.red_packet.remaining_claims === 0 ||
                claimPacket.isPending
              }
            >
              {claimPacket.isPending ? (
                <Spinner data-icon="inline-start" />
              ) : null}
              领取
            </Button>
          ) : null}
        </div>
      ) : null}

      {post.pinned || post.featured ? (
        <div className="mt-2 flex gap-1">
          {post.pinned ? (
            <Badge variant="outline">
              <PinIcon />
              置顶
            </Badge>
          ) : null}
          {post.featured ? (
            <Badge variant="outline">
              <SparklesIcon />
              精华
            </Badge>
          ) : null}
        </div>
      ) : null}

      <div className="mt-3">
        <Separator />
        <div className="mt-3 flex items-center justify-between">
          <div className="flex items-center gap-1">
            <Button
              type="button"
              variant="ghost"
              size="default"
              className={cn(
                "h-9 gap-1.5 border-0 px-3",
                post.liked_by_me && "text-primary"
              )}
              onClick={() =>
                csrfToken &&
                likePost.mutate({
                  postId: post.id,
                  active: !post.liked_by_me,
                  csrfToken,
                })
              }
              disabled={!csrfToken || likePost.isPending}
            >
              <HeartIcon data-icon="inline-start" />
              {(post.like_count ?? 0) > 0 ? post.like_count : "点赞"}
            </Button>
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
            <Button
              type="button"
              variant="ghost"
              size="default"
              className={cn(
                "h-9 gap-1.5 border-0 px-3",
                post.reposted_by_me && "text-primary"
              )}
              onClick={() =>
                csrfToken &&
                repostPost.mutate({
                  postId: post.id,
                  active: !post.reposted_by_me,
                  csrfToken,
                })
              }
              disabled={!csrfToken || repostPost.isPending}
            >
              <Repeat2Icon data-icon="inline-start" />
              {(post.repost_count ?? 0) > 0 ? post.repost_count : "转发"}
            </Button>
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

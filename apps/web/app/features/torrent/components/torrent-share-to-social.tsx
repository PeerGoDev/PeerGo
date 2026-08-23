import * as React from "react"
import { CheckCircle2Icon, CircleAlertIcon, Share2Icon } from "lucide-react"
import { Link } from "react-router"

import { Alert, AlertDescription, AlertTitle } from "~/components/ui/alert"
import { Button, buttonVariants } from "~/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "~/components/ui/dialog"
import { Field, FieldDescription, FieldLabel } from "~/components/ui/field"
import { Spinner } from "~/components/ui/spinner"
import { Textarea } from "~/components/ui/textarea"
import { useWebSession } from "~/features/auth/api/session.mutations"
import { useCapabilities } from "~/features/authz/api/capabilities.queries"
import { useCreateSocialPost } from "~/features/social/api/posts.queries"
import { useTorrentSwarm } from "~/features/torrent/api/torrent.queries"
import { TorrentCoverImage } from "~/features/torrent/components/torrent-cover-image"
import { formatTorrentSize } from "~/features/torrent/model/format"
import { ApiProblemError } from "~/shared/api/problem"

const maxShareReasonCharacters = 500
const maxSocialPostCharacters = 2_000

export function TorrentShareToSocial({
  torrentId,
  title,
  subtitle,
  sizeBytes,
  screenshotCount,
}: {
  torrentId: number
  title: string
  subtitle: string
  sizeBytes: number
  screenshotCount: number
}) {
  const [open, setOpen] = React.useState(false)
  const [reason, setReason] = React.useState("")
  const requestId = React.useRef<string>(undefined)
  const fieldId = React.useId()
  const session = useWebSession()
  const capabilities = useCapabilities(session.data?.user.id)
  const swarm = useTorrentSwarm(torrentId)
  const createPost = useCreateSocialPost()
  const actions = React.useMemo(
    () => new Set(capabilities.data?.items.map((item) => item.action) ?? []),
    [capabilities.data?.items]
  )
  const canPost = actions.has("social.post.create.self")
  const reasonCount = Array.from(reason).length
  const postContent = buildSharePost(reason, title, torrentId)
  const postCount = Array.from(postContent).length
  const valid =
    reasonCount <= maxShareReasonCharacters &&
    postCount <= maxSocialPostCharacters &&
    canPost

  function changeOpen(nextOpen: boolean) {
    if (!nextOpen && createPost.isPending) return
    setOpen(nextOpen)
    if (!nextOpen) {
      setReason("")
      requestId.current = undefined
      createPost.reset()
    }
  }

  function changeReason(value: string) {
    setReason(value)
    requestId.current = undefined
    createPost.reset()
  }

  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!session.data || !valid) return
    requestId.current ??= globalThis.crypto.randomUUID()
    try {
      await createPost.mutateAsync({
        content: postContent,
        boardId: "resources",
        csrfToken: session.data.csrf_token,
        idempotencyKey: requestId.current,
      })
    } catch {
      // Preserve the idempotency key so an unchanged share can be retried safely.
    }
  }

  return (
    <Dialog open={open} onOpenChange={changeOpen}>
      <DialogTrigger render={<Button variant="outline" size="sm" />}>
        <Share2Icon data-icon="inline-start" />
        分享到动态圈
      </DialogTrigger>
      <DialogContent
        className="gap-4 border p-6 sm:max-w-md"
        overlayClassName="bg-black/60 backdrop-blur-sm"
        showCloseButton={false}
      >
        <DialogHeader className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-0">
          <span className="row-span-2 rounded-full bg-primary/10 p-2 text-primary">
            <Share2Icon className="size-6" />
          </span>
          <DialogTitle className="text-lg font-semibold">
            分享到动态圈
          </DialogTitle>
          <DialogDescription>分享这个种子给你的朋友们</DialogDescription>
        </DialogHeader>

        {createPost.isSuccess ? (
          <Alert>
            <CheckCircle2Icon />
            <AlertTitle>分享成功</AlertTitle>
            <AlertDescription>这条种子推荐已经发布到动态圈。</AlertDescription>
          </Alert>
        ) : !session.data && !session.isPending ? (
          <Alert>
            <CircleAlertIcon />
            <AlertTitle>登录后才能分享</AlertTitle>
            <AlertDescription>
              登录后可以添加推荐理由并发布到动态圈。
            </AlertDescription>
          </Alert>
        ) : session.isPending || capabilities.isPending ? (
          <div className="flex min-h-32 items-center justify-center text-muted-foreground">
            <Spinner />
            <span className="ml-2">正在准备分享…</span>
          </div>
        ) : !canPost ? (
          <Alert>
            <CircleAlertIcon />
            <AlertTitle>当前账户不能发布动态</AlertTitle>
            <AlertDescription>
              你仍然可以复制当前页面地址手动分享。
            </AlertDescription>
          </Alert>
        ) : (
          <form id="torrent-share-form" className="min-w-0" onSubmit={submit}>
            <div className="mb-4 rounded-lg border bg-muted/30 p-3">
              <div className="flex gap-3">
                {screenshotCount > 0 ? (
                  <img
                    src={`/api/v1/torrents/${encodeURIComponent(torrentId)}/screenshots/0`}
                    alt=""
                    className="h-20 w-16 shrink-0 rounded object-cover"
                  />
                ) : (
                  <div className="h-20 w-16 shrink-0 overflow-hidden rounded bg-muted">
                    <TorrentCoverImage
                      torrentId={torrentId}
                      title={title}
                      available={false}
                      fallbackClassName="[&_svg]:size-6"
                    />
                  </div>
                )}
                <div className="min-w-0 flex-1">
                  <div className="truncate font-medium">{title}</div>
                  {subtitle ? (
                    <div className="mt-0.5 truncate text-sm text-muted-foreground">
                      {subtitle}
                    </div>
                  ) : null}
                  <div className="mt-2 flex items-center gap-2 text-xs text-muted-foreground">
                    <span>{formatTorrentSize(sizeBytes)}</span>
                    <span>·</span>
                    <span>{shareSwarmCount(swarm.data?.seeders)} 做种</span>
                    <span>·</span>
                    <span>{shareSwarmCount(swarm.data?.leechers)} 下载</span>
                  </div>
                </div>
              </div>
            </div>

            <Field
              data-invalid={reasonCount > maxShareReasonCharacters || undefined}
              className="gap-0"
            >
              <FieldLabel htmlFor={fieldId} className="mb-2">
                说点什么（可选）
              </FieldLabel>
              <Textarea
                id={fieldId}
                value={reason}
                rows={3}
                maxLength={maxShareReasonCharacters + 1}
                placeholder="推荐理由、观后感..."
                className="h-[78px] min-h-[78px] resize-none"
                disabled={createPost.isPending}
                onChange={(event) => changeReason(event.target.value)}
              />
              <FieldDescription className="mt-1! text-right text-xs leading-4">
                {reasonCount}/{maxShareReasonCharacters}
              </FieldDescription>
            </Field>

            {createPost.error ? (
              <Alert variant="destructive" className="mt-4">
                <CircleAlertIcon />
                <AlertTitle>分享失败</AlertTitle>
                <AlertDescription>
                  {createPost.error instanceof ApiProblemError
                    ? createPost.error.message
                    : "暂时无法发布到动态圈，请稍后重试。"}
                </AlertDescription>
              </Alert>
            ) : null}
          </form>
        )}

        <DialogFooter className="mx-0 mb-0 grid w-full grid-cols-2 gap-3 border-0 bg-transparent p-0 sm:grid sm:grid-cols-[194px_192px] sm:justify-end">
          {createPost.isSuccess ? (
            <Button
              type="button"
              className="col-span-2"
              onClick={() => changeOpen(false)}
            >
              完成
            </Button>
          ) : !session.data && !session.isPending ? (
            <Link to="/login" className={`${buttonVariants()} col-span-2`}>
              登录
            </Link>
          ) : canPost ? (
            <>
              <Button
                type="button"
                variant="outline"
                className="min-w-0"
                disabled={createPost.isPending}
                onClick={() => changeOpen(false)}
              >
                取消
              </Button>
              <Button
                type="submit"
                form="torrent-share-form"
                className="min-w-0"
                disabled={!valid || createPost.isPending}
              >
                {createPost.isPending ? (
                  <Spinner data-icon="inline-start" />
                ) : null}
                {createPost.isPending ? "分享中..." : "分享"}
              </Button>
            </>
          ) : null}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function buildSharePost(reason: string, title: string, torrentId: number) {
  const recommendation = reason.trim()
  return [recommendation, `分享种子：${title}`, `/torrents/${torrentId}`]
    .filter(Boolean)
    .join("\n\n")
}

function shareSwarmCount(value: number | undefined) {
  return value === undefined ? "—" : value.toLocaleString("zh-CN")
}

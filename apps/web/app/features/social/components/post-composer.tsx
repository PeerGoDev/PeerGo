import * as React from "react"
import {
  AtSignIcon,
  BarChart3Icon,
  FileTextIcon,
  GiftIcon,
  GlobeIcon,
  HashIcon,
  ImageIcon,
} from "lucide-react"

import { Alert, AlertDescription, AlertTitle } from "~/components/ui/alert"
import { Button } from "~/components/ui/button"
import { Separator } from "~/components/ui/separator"
import { Spinner } from "~/components/ui/spinner"
import { Textarea } from "~/components/ui/textarea"
import { useCreateSocialPost } from "~/features/social/api/posts.queries"
import { ApiProblemError } from "~/shared/api/problem"

const maxPostCharacters = 2_000

export function PostComposer({
  csrfToken,
  canPost,
}: {
  csrfToken: string
  canPost: boolean
}) {
  const [content, setContent] = React.useState("")
  const requestId = React.useRef<string>(undefined)
  const createPost = useCreateSocialPost()
  const count = Array.from(content).length
  const valid = count > 0 && count <= maxPostCharacters && canPost

  function changeContent(value: string) {
    if (Array.from(value).length <= maxPostCharacters) {
      setContent(value)
      requestId.current = undefined
      createPost.reset()
    }
  }

  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!valid) return
    requestId.current ??= globalThis.crypto.randomUUID()
    try {
      await createPost.mutateAsync({
        content: content.trim(),
        csrfToken,
        idempotencyKey: requestId.current,
      })
      setContent("")
      requestId.current = undefined
    } catch {
      // Keep content and the idempotency key for an exact safe retry.
    }
  }

  return (
    <form onSubmit={submit} className="rounded-lg border bg-card p-4">
      <div className="relative h-[106px]">
        <Textarea
          value={content}
          onChange={(event) => changeContent(event.target.value)}
          placeholder="分享你的想法..."
          aria-label="动态正文"
          className="min-h-[100px] resize-none rounded-none border-0 bg-transparent p-0 text-sm shadow-none focus-visible:ring-0 dark:bg-transparent"
          disabled={createPost.isPending || !canPost}
        />
      </div>

      {createPost.error ? (
        <Alert variant="destructive" className="mt-3">
          <AlertTitle>发布失败</AlertTitle>
          <AlertDescription>
            {createPost.error instanceof ApiProblemError
              ? createPost.error.message
              : "暂时无法发布动态，请稍后重试。"}
          </AlertDescription>
        </Alert>
      ) : null}

      <div className="mt-3">
        <Separator />
        <div className="mt-3 flex min-w-0 items-center justify-between gap-2">
          <div className="flex min-w-0 items-center gap-0.5 text-muted-foreground sm:gap-1">
            <UnavailableComposerAction
              label="添加图片 (0/9)"
              title="图片动态将在媒体对象存储接通后开放"
            >
              <ImageIcon data-icon="inline-start" />
            </UnavailableComposerAction>
            <Button
              type="button"
              variant="ghost"
              size="icon-sm"
              aria-label="添加话题"
              title="添加话题"
              onClick={() => changeContent(`${content}#`)}
              disabled={createPost.isPending || !canPost}
            >
              <HashIcon data-icon="inline-start" />
            </Button>
            <Button
              type="button"
              variant="ghost"
              size="icon-sm"
              aria-label="@提及"
              title="@提及"
              onClick={() => changeContent(`${content}@`)}
              disabled={createPost.isPending || !canPost}
            >
              <AtSignIcon data-icon="inline-start" />
            </Button>
            <UnavailableComposerAction
              label="添加投票"
              title="动态投票尚未开放"
            >
              <BarChart3Icon data-icon="inline-start" />
            </UnavailableComposerAction>
            <UnavailableComposerAction label="发红包" title="动态红包尚未开放">
              <GiftIcon data-icon="inline-start" />
            </UnavailableComposerAction>
            <UnavailableComposerAction label="草稿箱" title="动态草稿尚未开放">
              <FileTextIcon data-icon="inline-start" />
            </UnavailableComposerAction>
          </div>
          <div className="flex shrink-0 items-center gap-1 sm:gap-2">
            <span className="hidden text-xs text-muted-foreground sm:inline">
              {count}/{maxPostCharacters}
            </span>
            <span className="hidden items-center gap-1 text-xs text-muted-foreground sm:flex">
              <GlobeIcon className="size-3.5" />
              公开
            </span>
            <Button
              type="submit"
              size="sm"
              className="px-3 sm:px-3"
              disabled={!valid || createPost.isPending}
            >
              {createPost.isPending ? (
                <Spinner data-icon="inline-start" />
              ) : null}
              {createPost.isPending ? "发布中..." : "发布"}
            </Button>
          </div>
        </div>
      </div>
    </form>
  )
}

function UnavailableComposerAction({
  label,
  title,
  children,
}: {
  label: string
  title: string
  children: React.ReactNode
}) {
  return (
    <Button
      type="button"
      variant="ghost"
      size="icon-sm"
      aria-label={label}
      title={title}
      aria-disabled="true"
    >
      {children}
    </Button>
  )
}

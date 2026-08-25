import * as React from "react"
import {
  AtSignIcon,
  BarChart3Icon,
  FileTextIcon,
  GiftIcon,
  GlobeIcon,
  HashIcon,
  ImageIcon,
  PlusIcon,
  Trash2Icon,
  XIcon,
} from "lucide-react"

import { Alert, AlertDescription, AlertTitle } from "~/components/ui/alert"
import { Badge } from "~/components/ui/badge"
import { Button } from "~/components/ui/button"
import { Input } from "~/components/ui/input"
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "~/components/ui/select"
import { Separator } from "~/components/ui/separator"
import { Spinner } from "~/components/ui/spinner"
import { Textarea } from "~/components/ui/textarea"
import {
  type CreateSocialPollRequest,
  type CreateSocialRedPacketRequest,
  type SocialBoard,
  useCreateSocialPost,
  useUploadSocialMedia,
} from "~/features/social/api/posts.queries"
import { ApiProblemError } from "~/shared/api/problem"

const maxPostCharacters = 2_000
const draftKey = "peergo:social:draft:v1"

type LocalMedia = { id: string; url: string; previewUrl: string }

export function PostComposer({
  csrfToken,
  canPost,
  canPostRestrictedBoards,
  boards,
}: {
  csrfToken: string
  canPost: boolean
  canPostRestrictedBoards: boolean
  boards: SocialBoard[]
}) {
  const postingBoards = boards.filter(
    (board) => board.allow_member_posts || canPostRestrictedBoards
  )
  const [content, setContent] = React.useState("")
  const [boardId, setBoardId] = React.useState(postingBoards[0]?.id ?? "")
  const [media, setMedia] = React.useState<LocalMedia[]>([])
  const [pollOpen, setPollOpen] = React.useState(false)
  const [pollQuestion, setPollQuestion] = React.useState("")
  const [pollOptions, setPollOptions] = React.useState(["", ""])
  const [packetOpen, setPacketOpen] = React.useState(false)
  const [packetAmount, setPacketAmount] = React.useState("")
  const [packetClaims, setPacketClaims] = React.useState("")
  const [draftSaved, setDraftSaved] = React.useState(false)
  const requestId = React.useRef<string>(undefined)
  const fileInput = React.useRef<HTMLInputElement>(null)
  const createPost = useCreateSocialPost()
  const uploadMedia = useUploadSocialMedia()
  const count = Array.from(content).length
  const poll = buildPoll(pollOpen, pollQuestion, pollOptions)
  const redPacket = buildRedPacket(packetOpen, packetAmount, packetClaims)
  const valid =
    count > 0 &&
    count <= maxPostCharacters &&
    canPost &&
    Boolean(boardId) &&
    (!pollOpen || Boolean(poll)) &&
    (!packetOpen || Boolean(redPacket))

  React.useEffect(() => {
    try {
      const saved = JSON.parse(localStorage.getItem(draftKey) ?? "null") as {
        content?: string
        boardId?: string
        pollQuestion?: string
        pollOptions?: string[]
        packetAmount?: string
        packetClaims?: string
      } | null
      if (!saved) return
      setContent(saved.content ?? "")
      if (
        saved.boardId &&
        postingBoards.some((item) => item.id === saved.boardId)
      ) {
        setBoardId(saved.boardId)
      }
      if (saved.pollQuestion || saved.pollOptions?.some(Boolean)) {
        setPollOpen(true)
        setPollQuestion(saved.pollQuestion ?? "")
        setPollOptions(saved.pollOptions?.slice(0, 6) ?? ["", ""])
      }
      if (saved.packetAmount || saved.packetClaims) {
        setPacketOpen(true)
        setPacketAmount(saved.packetAmount ?? "")
        setPacketClaims(saved.packetClaims ?? "")
      }
    } catch {
      localStorage.removeItem(draftKey)
    }
  }, [])

  React.useEffect(() => {
    if (!boardId && postingBoards[0]) setBoardId(postingBoards[0].id)
  }, [boardId, postingBoards])

  function changeContent(value: string) {
    if (Array.from(value).length <= maxPostCharacters) {
      setContent(value)
      setDraftSaved(false)
      requestId.current = undefined
      createPost.reset()
    }
  }

  function saveDraft() {
    localStorage.setItem(
      draftKey,
      JSON.stringify({
        content,
        boardId,
        pollQuestion: pollOpen ? pollQuestion : "",
        pollOptions: pollOpen ? pollOptions : [],
        packetAmount: packetOpen ? packetAmount : "",
        packetClaims: packetOpen ? packetClaims : "",
      })
    )
    setDraftSaved(true)
  }

  async function addImages(files: FileList | null) {
    if (!files) return
    const selected = Array.from(files).slice(0, 9 - media.length)
    for (const file of selected) {
      const uploaded = await uploadMedia.mutateAsync({ file, csrfToken })
      setMedia((current) => [
        ...current,
        {
          id: uploaded.id,
          url: uploaded.url,
          previewUrl: URL.createObjectURL(file),
        },
      ])
    }
    if (fileInput.current) fileInput.current.value = ""
  }

  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!valid) return
    requestId.current ??= globalThis.crypto.randomUUID()
    try {
      await createPost.mutateAsync({
        content: content.trim(),
        boardId,
        mediaIds: media.map((item) => item.id),
        poll,
        redPacket,
        csrfToken,
        idempotencyKey: requestId.current,
      })
      media.forEach((item) => URL.revokeObjectURL(item.previewUrl))
      setContent("")
      setMedia([])
      setPollOpen(false)
      setPollQuestion("")
      setPollOptions(["", ""])
      setPacketOpen(false)
      setPacketAmount("")
      setPacketClaims("")
      setDraftSaved(false)
      localStorage.removeItem(draftKey)
      requestId.current = undefined
    } catch {
      // Keep all inputs and the idempotency key for an exact retry.
    }
  }

  return (
    <form onSubmit={submit} className="rounded-lg border bg-card p-3">
      <div className="mb-2 flex items-center gap-2 text-sm text-muted-foreground">
        <span>发布到</span>
        <Select
          items={postingBoards.map((board) => ({
            label: board.allow_member_posts
              ? board.name
              : `${board.name}（管理团队）`,
            value: board.id,
          }))}
          value={boardId}
          onValueChange={(value) => value && setBoardId(value)}
        >
          <SelectTrigger size="xs" aria-label="发布板块">
            <SelectValue />
          </SelectTrigger>
          <SelectContent align="start">
            <SelectGroup>
              {postingBoards.map((board) => (
                <SelectItem key={board.id} value={board.id}>
                  {board.allow_member_posts
                    ? board.name
                    : `${board.name}（管理团队）`}
                </SelectItem>
              ))}
            </SelectGroup>
          </SelectContent>
        </Select>
        {draftSaved ? <Badge variant="secondary">草稿已保存</Badge> : null}
      </div>

      <Textarea
        value={content}
        onChange={(event) => changeContent(event.target.value)}
        placeholder="分享你的想法..."
        aria-label="动态正文"
        className="min-h-[68px] resize-none rounded-none border-0 bg-transparent p-0 text-sm shadow-none focus-visible:ring-0 dark:bg-transparent"
        disabled={createPost.isPending || !canPost}
      />

      {media.length > 0 ? (
        <div className="mt-3 grid grid-cols-3 gap-2">
          {media.map((item) => (
            <div
              key={item.id}
              className="group relative aspect-video overflow-hidden rounded-md bg-muted"
            >
              <img
                src={item.previewUrl}
                alt="待发布图片"
                className="size-full object-cover"
              />
              <Button
                type="button"
                variant="secondary"
                size="icon-xs"
                className="absolute top-1 right-1 opacity-90"
                aria-label="移除图片"
                onClick={() => {
                  URL.revokeObjectURL(item.previewUrl)
                  setMedia((current) =>
                    current.filter((candidate) => candidate.id !== item.id)
                  )
                }}
              >
                <XIcon data-icon="inline-start" />
              </Button>
            </div>
          ))}
        </div>
      ) : null}

      {pollOpen ? (
        <div className="mt-3 space-y-2 rounded-md border bg-muted/30 p-3">
          <div className="flex items-center justify-between">
            <span className="text-sm font-medium">投票</span>
            <Button
              type="button"
              variant="ghost"
              size="icon-xs"
              aria-label="移除投票"
              onClick={() => setPollOpen(false)}
            >
              <Trash2Icon data-icon="inline-start" />
            </Button>
          </div>
          <Input
            value={pollQuestion}
            onChange={(event) => setPollQuestion(event.target.value)}
            maxLength={120}
            placeholder="投票问题"
            aria-label="投票问题"
          />
          {pollOptions.map((option, index) => (
            <Input
              key={index}
              value={option}
              onChange={(event) =>
                setPollOptions((items) =>
                  items.map((item, position) =>
                    position === index ? event.target.value : item
                  )
                )
              }
              maxLength={80}
              placeholder={`选项 ${index + 1}`}
              aria-label={`投票选项 ${index + 1}`}
            />
          ))}
          {pollOptions.length < 6 ? (
            <Button
              type="button"
              variant="ghost"
              size="sm"
              onClick={() => setPollOptions((items) => [...items, ""])}
            >
              <PlusIcon data-icon="inline-start" />
              添加选项
            </Button>
          ) : null}
        </div>
      ) : null}

      {packetOpen ? (
        <div className="mt-3 grid gap-2 rounded-md border border-primary/20 bg-primary/5 p-3 sm:grid-cols-2">
          <div className="flex items-center justify-between sm:col-span-2">
            <span className="text-sm font-medium">魔力值红包</span>
            <Button
              type="button"
              variant="ghost"
              size="icon-xs"
              aria-label="移除红包"
              onClick={() => setPacketOpen(false)}
            >
              <Trash2Icon data-icon="inline-start" />
            </Button>
          </div>
          <Input
            type="number"
            min={1}
            max={1000000}
            value={packetAmount}
            onChange={(event) => setPacketAmount(event.target.value)}
            placeholder="总魔力值"
            aria-label="红包总魔力值"
          />
          <Input
            type="number"
            min={1}
            max={100}
            value={packetClaims}
            onChange={(event) => setPacketClaims(event.target.value)}
            placeholder="份数"
            aria-label="红包份数"
          />
          <p className="text-xs text-muted-foreground sm:col-span-2">
            发布时立即从余额锁定，领取完之前由系统托管。
          </p>
        </div>
      ) : null}

      {createPost.isError || uploadMedia.isError ? (
        <Alert variant="destructive" className="mt-3">
          <AlertTitle>发布失败</AlertTitle>
          <AlertDescription>
            {(createPost.error ?? uploadMedia.error) instanceof ApiProblemError
              ? (createPost.error ?? uploadMedia.error)?.message
              : "暂时无法发布动态，请稍后重试。"}
          </AlertDescription>
        </Alert>
      ) : null}

      <div className="mt-2">
        <Separator />
        <div className="mt-2 flex min-w-0 items-center justify-between gap-2">
          <div className="flex min-w-0 items-center gap-0.5 text-muted-foreground sm:gap-1">
            <input
              ref={fileInput}
              type="file"
              accept="image/jpeg,image/png,image/webp"
              multiple
              className="sr-only"
              onChange={(event) => void addImages(event.target.files)}
            />
            <Button
              type="button"
              variant="ghost"
              size="icon-sm"
              aria-label={`添加图片 (${media.length}/9)`}
              title={`添加图片 (${media.length}/9)`}
              onClick={() => fileInput.current?.click()}
              disabled={media.length >= 9 || uploadMedia.isPending || !canPost}
            >
              {uploadMedia.isPending ? (
                <Spinner data-icon="inline-start" />
              ) : (
                <ImageIcon data-icon="inline-start" />
              )}
            </Button>
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
            <Button
              type="button"
              variant={pollOpen ? "secondary" : "ghost"}
              size="icon-sm"
              aria-label="添加投票"
              title="添加投票"
              onClick={() => setPollOpen((value) => !value)}
              disabled={createPost.isPending || !canPost || packetOpen}
            >
              <BarChart3Icon data-icon="inline-start" />
            </Button>
            <Button
              type="button"
              variant={packetOpen ? "secondary" : "ghost"}
              size="icon-sm"
              aria-label="发红包"
              title="发红包"
              onClick={() => setPacketOpen((value) => !value)}
              disabled={createPost.isPending || !canPost || pollOpen}
            >
              <GiftIcon data-icon="inline-start" />
            </Button>
            <Button
              type="button"
              variant="ghost"
              size="icon-sm"
              aria-label="保存草稿"
              title="保存草稿"
              onClick={saveDraft}
              disabled={!content && !pollOpen && !packetOpen}
            >
              <FileTextIcon data-icon="inline-start" />
            </Button>
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
              className="px-3"
              disabled={!valid || createPost.isPending || uploadMedia.isPending}
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

function buildPoll(
  open: boolean,
  question: string,
  options: string[]
): CreateSocialPollRequest | undefined {
  if (!open) return undefined
  const normalized = options.map((value) => value.trim()).filter(Boolean)
  if (!question.trim() || normalized.length < 2) return undefined
  return { question: question.trim(), options: normalized }
}

function buildRedPacket(
  open: boolean,
  amount: string,
  claims: string
): CreateSocialRedPacketRequest | undefined {
  if (!open) return undefined
  const totalAmount = Number(amount)
  const claimCount = Number(claims)
  if (
    !Number.isSafeInteger(totalAmount) ||
    !Number.isSafeInteger(claimCount) ||
    totalAmount < claimCount ||
    claimCount < 1
  )
    return undefined
  return { total_amount: totalAmount, claim_count: claimCount }
}

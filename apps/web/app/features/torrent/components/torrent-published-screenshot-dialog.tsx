import * as React from "react"
import { useQuery } from "@tanstack/react-query"
import {
  ArrowLeftIcon,
  ArrowRightIcon,
  CircleAlertIcon,
  ImagePlusIcon,
  Trash2Icon,
} from "lucide-react"

import { Alert, AlertDescription, AlertTitle } from "~/components/ui/alert"
import { Button } from "~/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "~/components/ui/dialog"
import {
  Field,
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLabel,
} from "~/components/ui/field"
import { Input } from "~/components/ui/input"
import { Spinner } from "~/components/ui/spinner"
import { Textarea } from "~/components/ui/textarea"
import {
  useSubmitPublishedTorrentScreenshotChange,
  type PublishedTorrentScreenshotChange,
} from "~/features/torrent/api/torrent-screenshot-change.mutations"
import {
  torrentDetailQueryOptions,
  type MyTorrentSubmissionPage,
} from "~/features/torrent/api/torrent.queries"
import { requestErrorDescription } from "~/shared/api/problem"

type Submission = MyTorrentSubmissionPage["items"][number]
type EditorItem =
  | { key: string; kind: "existing"; index: number; preview: string }
  | { key: string; kind: "upload"; file: File; preview: string }

const maxScreenshots = 6
const maxScreenshotBytes = 2 * 1024 * 1024
const acceptedTypes = new Set(["image/jpeg", "image/png", "image/webp"])

export function TorrentPublishedScreenshotDialog({
  submission,
  userId,
  csrfToken,
  onOpenChange,
  onSubmitted,
}: {
  submission: Submission
  userId: string
  csrfToken: string
  onOpenChange: (open: boolean) => void
  onSubmitted: (result: PublishedTorrentScreenshotChange) => void
}) {
  const detail = useQuery(torrentDetailQueryOptions(submission.id))
  const mutation = useSubmitPublishedTorrentScreenshotChange(userId)
  const [items, setItems] = React.useState<EditorItem[]>([])
  const [reason, setReason] = React.useState("")
  const [inputError, setInputError] = React.useState("")
  const requestId = React.useRef<string | undefined>(undefined)
  const previewUrls = React.useRef(new Set<string>())
  const initialized = React.useRef(false)
  const reasonLength = Array.from(reason.trim()).length

  React.useEffect(() => {
    if (!detail.data || initialized.current) return
    initialized.current = true
    setItems(
      Array.from({ length: detail.data.screenshot_count }, (_, index) => ({
        key: `existing-${index}`,
        kind: "existing" as const,
        index,
        preview: `/api/v1/torrents/${submission.id}/screenshots/${index}`,
      }))
    )
  }, [detail.data, submission.id])

  React.useEffect(
    () => () => {
      previewUrls.current.forEach((url) => URL.revokeObjectURL(url))
    },
    []
  )

  function addFiles(files: FileList | null) {
    if (!files) return
    const next = Array.from(files)
    if (items.length + next.length > maxScreenshots) {
      setInputError("截图总数不能超过 6 张。")
      return
    }
    const invalid = next.find(
      (file) => !acceptedTypes.has(file.type) || file.size > maxScreenshotBytes
    )
    if (invalid) {
      setInputError("仅支持不超过 2 MiB 的 JPEG、PNG 或 WebP 原图。")
      return
    }
    setInputError("")
    requestId.current = undefined
    const additions = next.map((file) => {
      const preview = URL.createObjectURL(file)
      previewUrls.current.add(preview)
      return {
        key: globalThis.crypto.randomUUID(),
        kind: "upload" as const,
        file,
        preview,
      }
    })
    setItems((current) => [...current, ...additions])
  }

  function move(index: number, offset: -1 | 1) {
    const target = index + offset
    if (target < 0 || target >= items.length) return
    setItems((current) => {
      const next = [...current]
      ;[next[index], next[target]] = [next[target], next[index]]
      return next
    })
    requestId.current = undefined
  }

  function remove(index: number) {
    const item = items[index]
    if (item?.kind === "upload") {
      URL.revokeObjectURL(item.preview)
      previewUrls.current.delete(item.preview)
    }
    setItems((current) => current.filter((_, position) => position !== index))
    requestId.current = undefined
  }

  async function submit() {
    if (reasonLength > 1000 || items.length > 6) return
    const uploads = items
      .filter(
        (item): item is Extract<EditorItem, { kind: "upload" }> =>
          item.kind === "upload"
      )
      .map((item) => item.file)
    let uploadIndex = 0
    const manifest = items.map((item) =>
      item.kind === "existing"
        ? { source: "existing" as const, index: item.index }
        : { source: "upload" as const, index: uploadIndex++ }
    )
    requestId.current ??= globalThis.crypto.randomUUID()
    const result = await mutation.mutateAsync({
      torrentId: submission.id,
      expectedVersion: submission.version,
      manifest,
      uploads,
      reason: reason.trim(),
      csrfToken,
      idempotencyKey: requestId.current,
    })
    onSubmitted(result)
  }

  return (
    <Dialog
      open
      onOpenChange={(open) => !mutation.isPending && onOpenChange(open)}
    >
      <DialogContent className="max-h-[calc(100dvh-2rem)] overflow-y-auto sm:max-w-4xl">
        <DialogHeader>
          <DialogTitle>修改已发布种子的截图</DialogTitle>
          <DialogDescription>
            #{submission.id} {submission.title}
            。提交后原图集继续公开，审核通过时才整体切换。
          </DialogDescription>
        </DialogHeader>

        {detail.isError ? (
          <Alert variant="destructive">
            <CircleAlertIcon />
            <AlertTitle>当前截图暂时无法读取</AlertTitle>
            <AlertDescription>
              {requestErrorDescription(detail.error, "请关闭后重新尝试。")}
            </AlertDescription>
          </Alert>
        ) : null}
        {mutation.isError ? (
          <Alert variant="destructive">
            <CircleAlertIcon />
            <AlertTitle>截图修改未提交</AlertTitle>
            <AlertDescription>
              {requestErrorDescription(mutation.error, "请核对图片后重试。")}
            </AlertDescription>
          </Alert>
        ) : null}

        {detail.isPending ? (
          <div className="flex min-h-40 items-center justify-center gap-2 text-sm text-muted-foreground">
            <Spinner /> 正在读取当前图集
          </div>
        ) : (
          <FieldGroup>
            <Field>
              <FieldLabel htmlFor="published-screenshot-files">
                有序截图集
              </FieldLabel>
              <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
                {items.map((item, index) => (
                  <div
                    key={item.key}
                    className="overflow-hidden rounded-lg border bg-muted/20"
                  >
                    <div className="aspect-video bg-muted">
                      <img
                        src={item.preview}
                        alt={`候选截图 ${index + 1}`}
                        className="size-full object-cover"
                      />
                    </div>
                    <div className="flex items-center gap-1 p-2">
                      <span className="mr-auto text-xs font-medium">
                        {index === 0 ? "封面" : `第 ${index + 1} 张`} ·{" "}
                        {item.kind === "existing" ? "现有" : "新增"}
                      </span>
                      <Button
                        type="button"
                        size="icon-xs"
                        variant="ghost"
                        aria-label="向前移动"
                        disabled={index === 0}
                        onClick={() => move(index, -1)}
                      >
                        <ArrowLeftIcon />
                      </Button>
                      <Button
                        type="button"
                        size="icon-xs"
                        variant="ghost"
                        aria-label="向后移动"
                        disabled={index === items.length - 1}
                        onClick={() => move(index, 1)}
                      >
                        <ArrowRightIcon />
                      </Button>
                      <Button
                        type="button"
                        size="icon-xs"
                        variant="ghost"
                        aria-label="删除截图"
                        onClick={() => remove(index)}
                      >
                        <Trash2Icon />
                      </Button>
                    </div>
                  </div>
                ))}
              </div>
              <Input
                id="published-screenshot-files"
                type="file"
                accept="image/jpeg,image/png,image/webp"
                multiple
                disabled={items.length >= maxScreenshots || mutation.isPending}
                onChange={(event) => {
                  addFiles(event.target.files)
                  event.currentTarget.value = ""
                }}
              />
              <FieldDescription>
                最多 6 张，每张原图不超过 2
                MiB；第一张作为封面。可以排序、删除旧图并添加新图。
              </FieldDescription>
              {inputError ? <FieldError>{inputError}</FieldError> : null}
            </Field>
            <Field data-invalid={reasonLength > 1000}>
              <FieldLabel htmlFor="published-screenshot-reason">
                修改说明
              </FieldLabel>
              <Textarea
                id="published-screenshot-reason"
                value={reason}
                rows={3}
                maxLength={1000}
                placeholder="可留空；系统会自动记录修改说明"
                onChange={(event) => {
                  setReason(event.target.value)
                  requestId.current = undefined
                  mutation.reset()
                }}
              />
              <FieldDescription>
                {reasonLength} / 1000；留空时由系统自动记录
              </FieldDescription>
              {reasonLength > 1000 ? (
                <FieldError>修改说明不能超过 1000 个字符。</FieldError>
              ) : null}
            </Field>
          </FieldGroup>
        )}

        <DialogFooter>
          <Button
            type="button"
            variant="outline"
            disabled={mutation.isPending}
            onClick={() => onOpenChange(false)}
          >
            取消
          </Button>
          <Button
            type="button"
            disabled={
              detail.isPending ||
              detail.isError ||
              mutation.isPending ||
              reasonLength > 1000
            }
            onClick={() => void submit()}
          >
            {mutation.isPending ? (
              <Spinner />
            ) : (
              <ImagePlusIcon data-icon="inline-start" />
            )}
            提交截图审核
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

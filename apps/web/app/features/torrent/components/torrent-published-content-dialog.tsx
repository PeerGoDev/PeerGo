import * as React from "react"
import {
  CircleAlertIcon,
  FileLock2Icon,
  RefreshCwIcon,
  SendIcon,
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
  type PublishedTorrentContentChange,
  useSubmitPublishedTorrentContentChange,
} from "~/features/torrent/api/torrent-content-change.mutations"
import {
  type MyTorrentSubmissionPage,
  type TorrentPublicContent,
  type TorrentPublicDetail,
  useTorrentContent,
  useTorrentDetail,
} from "~/features/torrent/api/torrent.queries"
import { TorrentMarkdownEditor } from "~/features/torrent/components/torrent-markdown-editor"
import type { components } from "~/generated/api"
import { requestErrorDescription } from "~/shared/api/problem"

type MySubmission = MyTorrentSubmissionPage["items"][number]
type ExternalIdentifier = components["schemas"]["TorrentExternalIdentifier"]
type Provider = ExternalIdentifier["provider"]

const externalProviders: { provider: Provider; label: string; hint: string }[] =
  [
    { provider: "imdb", label: "IMDb", hint: "例如 tt1234567" },
    { provider: "tmdb", label: "TMDB", hint: "纯数字编号" },
    { provider: "douban", label: "豆瓣", hint: "纯数字编号" },
    { provider: "bangumi", label: "Bangumi", hint: "纯数字编号" },
    { provider: "steam", label: "Steam", hint: "纯数字 App ID" },
  ]

export function TorrentPublishedContentDialog({
  submission,
  userId,
  csrfToken,
  onOpenChange,
  onSubmitted,
}: {
  submission: MySubmission
  userId: string
  csrfToken: string
  onOpenChange: (open: boolean) => void
  onSubmitted: (result: PublishedTorrentContentChange) => void
}) {
  const detail = useTorrentDetail(submission.id)
  const content = useTorrentContent(submission.id)

  return (
    <Dialog open onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[calc(100dvh-2rem)] overflow-y-auto sm:max-w-4xl">
        <DialogHeader>
          <DialogTitle>修改已发布内容</DialogTitle>
          <DialogDescription>
            编辑种子简介、MediaInfo 和外部资料编号；提交后由种审人员核对。
          </DialogDescription>
        </DialogHeader>

        {detail.isPending || content.isPending ? (
          <div className="flex min-h-48 items-center justify-center gap-2 text-sm text-muted-foreground">
            <Spinner /> 正在读取当前公开内容
          </div>
        ) : detail.isError ||
          content.isError ||
          !detail.data ||
          !content.data ? (
          <Alert variant="destructive">
            <CircleAlertIcon />
            <AlertTitle>当前公开内容暂时无法读取</AlertTitle>
            <AlertDescription>
              {requestErrorDescription(
                detail.error ?? content.error,
                "请检查网络连接后重试。"
              )}
            </AlertDescription>
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={() =>
                void Promise.all([detail.refetch(), content.refetch()])
              }
            >
              <RefreshCwIcon data-icon="inline-start" />
              重试
            </Button>
          </Alert>
        ) : (
          <PublishedContentForm
            submission={submission}
            detail={detail.data}
            content={content.data}
            userId={userId}
            csrfToken={csrfToken}
            onOpenChange={onOpenChange}
            onSubmitted={onSubmitted}
          />
        )}
      </DialogContent>
    </Dialog>
  )
}

function PublishedContentForm({
  submission,
  detail,
  content,
  userId,
  csrfToken,
  onOpenChange,
  onSubmitted,
}: {
  submission: MySubmission
  detail: TorrentPublicDetail
  content: TorrentPublicContent
  userId: string
  csrfToken: string
  onOpenChange: (open: boolean) => void
  onSubmitted: (result: PublishedTorrentContentChange) => void
}) {
  const descriptionId = React.useId()
  const mediaInfoId = React.useId()
  const reasonId = React.useId()
  const [description, setDescription] = React.useState(content.description)
  const [mediaInfo, setMediaInfo] = React.useState(content.media_info)
  const [reason, setReason] = React.useState("")
  const [externalValues, setExternalValues] = React.useState<
    Record<Provider, string>
  >(() => externalIdentifierValues(detail.external_identifiers))
  const [formError, setFormError] = React.useState("")
  const requestId = React.useRef<string | undefined>(undefined)
  const mutation = useSubmitPublishedTorrentContentChange(userId)

  function resetAttempt() {
    requestId.current = undefined
    setFormError("")
    if (mutation.isError) mutation.reset()
  }

  async function handleSubmit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const normalizedDescription = description.trim()
    const normalizedMediaInfo = mediaInfo.trim()
    const normalizedReason = reason.trim()
    const identifiers = externalIdentifiers(externalValues)
    const validationError = validateContentChange(
      normalizedDescription,
      normalizedMediaInfo,
      normalizedReason,
      identifiers
    )
    if (validationError) {
      setFormError(validationError)
      return
    }
    if (
      normalizedDescription === content.description.trim() &&
      normalizedMediaInfo === content.media_info.trim() &&
      identifiersEqual(identifiers, detail.external_identifiers)
    ) {
      setFormError("请先修改简介、MediaInfo 或外部资料编号。")
      return
    }

    setFormError("")
    requestId.current ??= globalThis.crypto.randomUUID()
    try {
      const result = await mutation.mutateAsync({
        torrentId: submission.id,
        csrfToken,
        idempotencyKey: requestId.current,
        body: {
          expected_version: submission.version,
          description: normalizedDescription,
          media_info: normalizedMediaInfo,
          external_identifiers: identifiers,
          reason: normalizedReason,
        },
      })
      onSubmitted(result)
    } catch {
      // The mutation error is rendered below. The same idempotency key remains
      // available for a safe network retry until the user edits any field.
    }
  }

  const reasonLength = Array.from(reason.trim()).length
  const mutationError = mutation.isError
    ? requestErrorDescription(mutation.error, "内容修改提交失败，请稍后重试。")
    : ""

  return (
    <form onSubmit={handleSubmit} onInput={resetAttempt} noValidate>
      <FieldGroup>
        <Alert>
          <FileLock2Icon />
          <AlertTitle>审核前不会影响线上内容</AlertTitle>
          <AlertDescription>
            当前公开版本会持续展示；审核通过后才一次性切换。原始
            .torrent、信息哈希、文件树和统计不会改变，截图另走图片附件审核。
          </AlertDescription>
        </Alert>

        {formError || mutationError ? (
          <Alert variant="destructive">
            <CircleAlertIcon />
            <AlertTitle>暂时无法提交</AlertTitle>
            <AlertDescription>{formError || mutationError}</AlertDescription>
          </Alert>
        ) : null}

        <Field>
          <FieldLabel htmlFor={descriptionId}>种子简介</FieldLabel>
          <TorrentMarkdownEditor
            id={descriptionId}
            value={description}
            onValueChange={(value) => {
              setDescription(value)
              resetAttempt()
            }}
            invalid={Boolean(formError && !description.trim())}
            disabled={mutation.isPending}
          />
        </Field>

        <Field>
          <FieldLabel htmlFor={mediaInfoId}>MediaInfo</FieldLabel>
          <Textarea
            id={mediaInfoId}
            value={mediaInfo}
            rows={9}
            spellCheck={false}
            className="font-mono text-xs"
            disabled={mutation.isPending}
            placeholder="粘贴完整 MediaInfo；非影视资源可留空"
            onChange={(event) => setMediaInfo(event.target.value)}
          />
          <FieldDescription>
            用于审核与详情页展示，不会改写种子文件。
          </FieldDescription>
        </Field>

        <fieldset className="grid gap-3 rounded-lg border p-4 sm:grid-cols-2">
          <legend className="px-1 text-sm font-medium">外部资料编号</legend>
          {externalProviders.map(({ provider, label, hint }) => (
            <Field key={provider}>
              <FieldLabel htmlFor={`content-${provider}`}>{label}</FieldLabel>
              <Input
                id={`content-${provider}`}
                value={externalValues[provider]}
                disabled={mutation.isPending}
                placeholder={hint}
                maxLength={32}
                onChange={(event) =>
                  setExternalValues((values) => ({
                    ...values,
                    [provider]: event.target.value,
                  }))
                }
              />
            </Field>
          ))}
        </fieldset>

        <Field data-invalid={reasonLength > 0 && reasonLength < 10}>
          <FieldLabel htmlFor={reasonId}>修改说明</FieldLabel>
          <Textarea
            id={reasonId}
            value={reason}
            rows={3}
            minLength={10}
            maxLength={1000}
            disabled={mutation.isPending}
            aria-invalid={reasonLength > 0 && reasonLength < 10}
            placeholder="说明修改了什么，以及审核时需要注意的依据"
            onChange={(event) => setReason(event.target.value)}
          />
          <FieldDescription>
            {reasonLength} / 1000，至少 10 个字符
          </FieldDescription>
          {reasonLength > 0 && reasonLength < 10 ? (
            <FieldError>修改说明至少需要 10 个字符。</FieldError>
          ) : null}
        </Field>
      </FieldGroup>

      <DialogFooter className="mt-6">
        <Button
          type="button"
          variant="outline"
          disabled={mutation.isPending}
          onClick={() => onOpenChange(false)}
        >
          取消
        </Button>
        <Button type="submit" disabled={mutation.isPending}>
          {mutation.isPending ? (
            <Spinner />
          ) : (
            <SendIcon data-icon="inline-start" />
          )}
          提交审核
        </Button>
      </DialogFooter>
    </form>
  )
}

function externalIdentifierValues(items: ExternalIdentifier[]) {
  const result = { imdb: "", tmdb: "", douban: "", bangumi: "", steam: "" }
  for (const item of items) result[item.provider] = item.external_id
  return result
}

function externalIdentifiers(values: Record<Provider, string>) {
  return externalProviders.flatMap(({ provider }) => {
    const externalId = values[provider].trim()
    return externalId ? [{ provider, external_id: externalId }] : []
  })
}

function identifiersEqual(
  left: ExternalIdentifier[],
  right: ExternalIdentifier[]
) {
  const normalize = (items: ExternalIdentifier[]) =>
    [...items]
      .map((item) => `${item.provider}:${item.external_id.trim()}`)
      .sort()
      .join("|")
  return normalize(left) === normalize(right)
}

function validateContentChange(
  description: string,
  mediaInfo: string,
  reason: string,
  identifiers: ExternalIdentifier[]
) {
  if (!description) return "种子简介不能为空。"
  if (new TextEncoder().encode(description).length > 4 * 1024 * 1024) {
    return "种子简介不能超过 4 MiB。"
  }
  if (new TextEncoder().encode(mediaInfo).length > 16 * 1024 * 1024) {
    return "MediaInfo 不能超过 16 MiB。"
  }
  const reasonLength = Array.from(reason).length
  if (reasonLength < 10 || reasonLength > 1000) {
    return "修改说明需要 10 至 1000 个字符。"
  }
  for (const identifier of identifiers) {
    const valid =
      identifier.provider === "imdb"
        ? /^tt\d{7,10}$/.test(identifier.external_id)
        : /^\d{1,20}$/.test(identifier.external_id)
    if (!valid) return `${identifier.provider.toUpperCase()} 编号格式不正确。`
  }
  return ""
}

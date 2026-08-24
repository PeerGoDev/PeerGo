import * as React from "react"
import { CircleAlertIcon, FileLock2Icon, SendIcon } from "lucide-react"

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
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectLabel,
  SelectTrigger,
  SelectValue,
} from "~/components/ui/select"
import { Spinner } from "~/components/ui/spinner"
import { Textarea } from "~/components/ui/textarea"
import {
  type TorrentResubmission,
  useResubmitTorrentSubmission,
} from "~/features/torrent/api/torrent-resubmission.mutations"
import {
  type PublishedTorrentMetadataRevision,
  useUpdatePublishedTorrentMetadata,
} from "~/features/torrent/api/torrent-maintenance.mutations"
import type {
  MyTorrentSubmissionPage,
  TorrentCategory,
} from "~/features/torrent/api/torrent.queries"
import {
  torrentMetadataChanged,
  torrentResubmissionFieldErrors,
  torrentResubmissionFormSchema,
  type TorrentResubmissionFormErrors,
} from "~/features/torrent/model/torrent-resubmission-form"
import { ApiProblemError } from "~/shared/api/problem"

type MySubmission = MyTorrentSubmissionPage["items"][number]

export function TorrentResubmissionDialog({
  submission,
  categories,
  userId,
  csrfToken,
  onOpenChange,
  onSubmitted,
}: {
  submission: MySubmission
  categories: TorrentCategory[]
  userId: string
  csrfToken: string
  onOpenChange: (open: boolean) => void
  onSubmitted: (result: TorrentResubmission) => void
}) {
  return (
    <TorrentEditableMetadataDialog
      mode="resubmit"
      submission={submission}
      categories={categories}
      userId={userId}
      csrfToken={csrfToken}
      onOpenChange={onOpenChange}
      onCompleted={(result) => onSubmitted(result as TorrentResubmission)}
    />
  )
}

export function TorrentPublishedMetadataDialog({
  submission,
  categories,
  userId,
  csrfToken,
  onOpenChange,
  onUpdated,
}: {
  submission: MySubmission
  categories: TorrentCategory[]
  userId: string
  csrfToken: string
  onOpenChange: (open: boolean) => void
  onUpdated: (result: PublishedTorrentMetadataRevision) => void
}) {
  return (
    <TorrentEditableMetadataDialog
      mode="published"
      submission={submission}
      categories={categories}
      userId={userId}
      csrfToken={csrfToken}
      onOpenChange={onOpenChange}
      onCompleted={(result) =>
        onUpdated(result as PublishedTorrentMetadataRevision)
      }
    />
  )
}

function TorrentEditableMetadataDialog({
  mode,
  submission,
  categories,
  userId,
  csrfToken,
  onOpenChange,
  onCompleted,
}: {
  mode: "resubmit" | "published"
  submission: MySubmission
  categories: TorrentCategory[]
  userId: string
  csrfToken: string
  onOpenChange: (open: boolean) => void
  onCompleted: (
    result: TorrentResubmission | PublishedTorrentMetadataRevision
  ) => void
}) {
  const categoryFieldId = React.useId()
  const titleFieldId = React.useId()
  const subtitleFieldId = React.useId()
  const correctionNoteFieldId = React.useId()
  const originalCategoryAvailable = categories.some(
    (category) => category.id === submission.category.id
  )
  const [categoryId, setCategoryId] = React.useState<string | null>(
    originalCategoryAvailable
      ? submission.category.id
      : (categories[0]?.id ?? null)
  )
  const [correctionNote, setCorrectionNote] = React.useState("")
  const [errors, setErrors] = React.useState<TorrentResubmissionFormErrors>({})
  const [metadataError, setMetadataError] = React.useState<string>()
  const requestId = React.useRef<string>(undefined)
  const resubmit = useResubmitTorrentSubmission(userId)
  const updatePublished = useUpdatePublishedTorrentMetadata()
  const mutation = mode === "resubmit" ? resubmit : updatePublished
  const categoryOptions = categories.map((category) => ({
    value: category.id,
    label: category.name,
  }))

  function resetAttempt() {
    requestId.current = undefined
    setMetadataError(undefined)
    if (mutation.isError) mutation.reset()
  }

  async function handleSubmit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    mutation.reset()
    const form = event.currentTarget
    const formData = new FormData(form)
    const parsed = torrentResubmissionFormSchema.safeParse({
      categoryId: categoryId ?? "",
      title: formData.get("title"),
      subtitle: formData.get("subtitle") ?? "",
      correctionNote: formData.get("correction-note"),
    })
    if (!parsed.success) {
      setErrors(torrentResubmissionFieldErrors(parsed.error))
      requestAnimationFrame(() => {
        form.querySelector<HTMLElement>("[aria-invalid='true']")?.focus()
      })
      return
    }
    if (
      !torrentMetadataChanged(
        {
          categoryId: submission.category.id,
          title: submission.title,
          subtitle: submission.subtitle,
        },
        parsed.data
      )
    ) {
      setErrors({})
      setMetadataError(
        mode === "resubmit"
          ? "请先修改分类、标题或副标题，再重新送审。"
          : "请先修改分类、标题或副标题，再保存。"
      )
      return
    }

    setErrors({})
    setMetadataError(undefined)
    requestId.current ??= globalThis.crypto.randomUUID()
    try {
      if (mode === "resubmit") {
        const result = await resubmit.mutateAsync({
          torrentId: submission.id,
          csrfToken,
          idempotencyKey: requestId.current,
          body: {
            expected_version: submission.version,
            category_id: parsed.data.categoryId,
            title: parsed.data.title,
            subtitle: parsed.data.subtitle,
            correction_note: parsed.data.correctionNote,
          },
        })
        onCompleted(result)
      } else {
        const result = await updatePublished.mutateAsync({
          torrentId: submission.id,
          csrfToken,
          idempotencyKey: requestId.current,
          body: {
            expected_version: submission.version,
            category_id: parsed.data.categoryId,
            title: parsed.data.title,
            subtitle: parsed.data.subtitle,
            reason: parsed.data.correctionNote,
          },
        })
        onCompleted(result)
      }
    } catch (error) {
      if (
        error instanceof ApiProblemError &&
        [
          "torrent_resubmission_idempotency_conflict",
          "torrent_resubmission_version_conflict",
          "torrent_resubmission_state_conflict",
          "torrent_resubmission_not_allowed",
          "torrent_metadata_update_idempotency_conflict",
          "torrent_metadata_update_version_conflict",
          "torrent_metadata_update_state_conflict",
        ].includes(error.code)
      ) {
        requestId.current = undefined
      }
    }
  }

  const errorMessage = metadataMutationErrorMessage(mode, mutation.error)
  const noteLength = Array.from(correctionNote.trim()).length
  const isResubmission = mode === "resubmit"
  const formID = isResubmission
    ? "torrent-resubmission-form"
    : "torrent-published-metadata-form"

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open && !mutation.isPending) onOpenChange(false)
      }}
    >
      <DialogContent className="max-h-[calc(100dvh-2rem)] overflow-y-auto sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>
            {isResubmission ? "整改发布信息" : "修改已发布资料"}
          </DialogTitle>
          <DialogDescription>
            {isResubmission
              ? "根据最近审核反馈修改可编辑资料，保存后进入新一轮审核。"
              : "修改本人已发布种子的基础资料，并保留不可变修改记录。"}
          </DialogDescription>
        </DialogHeader>

        <form
          id={formID}
          onSubmit={handleSubmit}
          onInput={resetAttempt}
          noValidate
        >
          <FieldGroup>
            <Alert>
              <FileLock2Icon />
              <AlertTitle>原种子内容保持不变</AlertTitle>
              <AlertDescription>
                本次只能调整分类、标题和副标题；.torrent
                文件、信息哈希、文件清单及首次提交和发布时间都不会被替换。
              </AlertDescription>
            </Alert>

            {isResubmission && submission.latest_review ? (
              <Alert variant="destructive">
                <CircleAlertIcon />
                <AlertTitle>最近审核反馈</AlertTitle>
                <AlertDescription>
                  {submission.latest_review.reason}
                </AlertDescription>
              </Alert>
            ) : null}

            {errorMessage ? (
              <Alert variant="destructive">
                <CircleAlertIcon />
                <AlertTitle>{errorMessage.title}</AlertTitle>
                <AlertDescription>{errorMessage.description}</AlertDescription>
              </Alert>
            ) : null}

            {metadataError ? (
              <Alert variant="destructive">
                <CircleAlertIcon />
                <AlertTitle>发布资料尚未修改</AlertTitle>
                <AlertDescription>{metadataError}</AlertDescription>
              </Alert>
            ) : null}

            <Field data-invalid={Boolean(errors.categoryId)}>
              <FieldLabel htmlFor={categoryFieldId}>分类</FieldLabel>
              <Select
                items={categoryOptions}
                value={categoryId}
                onValueChange={(value) => {
                  setCategoryId(value)
                  resetAttempt()
                }}
                disabled={mutation.isPending}
              >
                <SelectTrigger
                  id={categoryFieldId}
                  className="w-full"
                  aria-invalid={Boolean(errors.categoryId)}
                >
                  <SelectValue placeholder="选择分类" />
                </SelectTrigger>
                <SelectContent alignItemWithTrigger={false}>
                  <SelectGroup>
                    <SelectLabel>可用分类</SelectLabel>
                    {categoryOptions.map((category) => (
                      <SelectItem key={category.value} value={category.value}>
                        {category.label}
                      </SelectItem>
                    ))}
                  </SelectGroup>
                </SelectContent>
              </Select>
              {!originalCategoryAvailable ? (
                <FieldDescription>
                  原分类当前不可用，请选择新的分类。
                </FieldDescription>
              ) : null}
              <FieldError>{errors.categoryId}</FieldError>
            </Field>

            <Field data-invalid={Boolean(errors.title)}>
              <FieldLabel htmlFor={titleFieldId}>标题</FieldLabel>
              <Input
                id={titleFieldId}
                name="title"
                defaultValue={submission.title}
                maxLength={240}
                aria-invalid={Boolean(errors.title)}
                disabled={mutation.isPending}
              />
              <FieldError>{errors.title}</FieldError>
            </Field>

            <Field data-invalid={Boolean(errors.subtitle)}>
              <FieldLabel htmlFor={subtitleFieldId}>副标题</FieldLabel>
              <Input
                id={subtitleFieldId}
                name="subtitle"
                defaultValue={submission.subtitle}
                maxLength={300}
                aria-invalid={Boolean(errors.subtitle)}
                disabled={mutation.isPending}
              />
              <FieldDescription>
                可留空，用于补充版本或内容说明。
              </FieldDescription>
              <FieldError>{errors.subtitle}</FieldError>
            </Field>

            <Field data-invalid={Boolean(errors.correctionNote)}>
              <FieldLabel htmlFor={correctionNoteFieldId}>
                {isResubmission ? "整改说明" : "修改说明"}
              </FieldLabel>
              <Textarea
                id={correctionNoteFieldId}
                name="correction-note"
                value={correctionNote}
                onChange={(event) =>
                  setCorrectionNote(event.currentTarget.value)
                }
                maxLength={1_000}
                rows={4}
                placeholder={
                  isResubmission
                    ? "可留空；系统会自动记录整改说明"
                    : "可留空；系统会自动记录修改说明"
                }
                aria-invalid={Boolean(errors.correctionNote)}
                disabled={mutation.isPending}
              />
              <FieldDescription>
                当前 {noteLength.toLocaleString("zh-CN")} /
                1000；留空时由系统自动记录。
              </FieldDescription>
              <FieldError>{errors.correctionNote}</FieldError>
            </Field>
          </FieldGroup>
        </form>

        <DialogFooter>
          <Button
            type="button"
            variant="outline"
            onClick={() => onOpenChange(false)}
            disabled={mutation.isPending}
          >
            取消
          </Button>
          <Button type="submit" form={formID} disabled={mutation.isPending}>
            {mutation.isPending ? (
              <Spinner data-icon="inline-start" />
            ) : (
              <SendIcon data-icon="inline-start" />
            )}
            {mutation.isPending
              ? isResubmission
                ? "正在重新送审"
                : "正在保存"
              : isResubmission
                ? "保存并重新送审"
                : "保存修改"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function metadataMutationErrorMessage(
  mode: "resubmit" | "published",
  error: unknown
) {
  if (!(error instanceof ApiProblemError)) {
    return error
      ? {
          title: mode === "resubmit" ? "重新送审失败" : "修改发布资料失败",
          description: "网络连接可能不稳定，请保留当前内容并稍后重试。",
        }
      : undefined
  }
  const messages: Record<string, { title: string; description: string }> = {
    torrent_resubmission_version_conflict: {
      title: "提交记录已经变化",
      description: "请关闭窗口并刷新记录，确认最新审核状态后再操作。",
    },
    torrent_resubmission_state_conflict: {
      title: "当前状态不能重新送审",
      description: "这条记录可能已经进入新一轮审核，请刷新页面确认。",
    },
    torrent_resubmission_not_allowed: {
      title: "本次驳回不能直接整改",
      description: "该审核结果不能只修改发布资料，请按反馈重新发布资源。",
    },
    torrent_resubmission_unchanged: {
      title: "发布资料没有变化",
      description: "请修改分类、标题或副标题后再重新送审。",
    },
    torrent_resubmission_category_unavailable: {
      title: "所选分类当前不可用",
      description: "请重新读取分类并选择其他可用分类。",
    },
    torrent_metadata_update_version_conflict: {
      title: "发布资料已经变化",
      description: "请关闭窗口并刷新记录，确认最新版本后再操作。",
    },
    torrent_metadata_update_state_conflict: {
      title: "当前状态不能修改",
      description: "只有仍处于已发布状态的本人种子可以直接修改资料。",
    },
    torrent_metadata_update_unchanged: {
      title: "发布资料没有变化",
      description: "请先修改分类、标题或副标题。",
    },
    torrent_metadata_update_category_unavailable: {
      title: "所选分类当前不可用",
      description: "请重新读取分类并选择其他可用分类。",
    },
    verified_email_required: {
      title: "需要先验证邮箱",
      description: "验证当前账户的邮箱后才能重新送审。",
    },
    csrf_invalid: {
      title: "页面验证信息已失效",
      description: "请刷新页面后重新填写整改内容。",
    },
  }
  return (
    messages[error.code] ?? {
      title: error.message,
      description: "请检查填写内容；如果问题仍然存在，请刷新页面后重试。",
    }
  )
}

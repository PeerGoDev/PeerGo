import * as React from "react"
import {
  CalendarClockIcon,
  CircleAlertIcon,
  SendIcon,
  Undo2Icon,
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
  type AnnouncementPublicationAction,
  type ManagedAnnouncement,
  type ManagedAnnouncementSummary,
  useChangeManagedAnnouncementPublication,
} from "~/features/staff/api/announcement-administration.queries"
import {
  type AnnouncementPublicationFormField,
  announcementPublicationFormSchema,
  publicationActionLabel,
} from "~/features/staff/model/announcement-form"
import { ApiProblemError } from "~/shared/api/problem"
import { formatDateTime } from "~/shared/formatters/date-time"

type FormErrors = Partial<
  Record<AnnouncementPublicationFormField | "form", string>
>

export function AnnouncementPublicationDialog({
  announcement,
  action,
  csrfToken,
  onOpenChange,
  onSaved,
}: {
  announcement: ManagedAnnouncementSummary
  action: AnnouncementPublicationAction
  csrfToken: string
  onOpenChange: (open: boolean) => void
  onSaved: (
    announcement: ManagedAnnouncement,
    action: AnnouncementPublicationAction
  ) => void
}) {
  const mutation = useChangeManagedAnnouncementPublication()
  const [errors, setErrors] = React.useState<FormErrors>({})
  const isPending = mutation.isPending

  function handleOpenChange(open: boolean) {
    if (!open && isPending) {
      return
    }
    onOpenChange(open)
  }

  async function handleSubmit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    mutation.reset()
    const formElement = event.currentTarget
    const form = new FormData(formElement)
    const result = announcementPublicationFormSchema.safeParse({
      action,
      scheduledFor: String(form.get("scheduledFor") ?? ""),
      reason: String(form.get("reason") ?? ""),
    })
    if (!result.success) {
      const nextErrors: FormErrors = {}
      for (const issue of result.error.issues) {
        const field = issue.path[0]
        if (
          typeof field === "string" &&
          !nextErrors[field as AnnouncementPublicationFormField]
        ) {
          nextErrors[field as AnnouncementPublicationFormField] = issue.message
        }
      }
      setErrors(nextErrors)
      requestAnimationFrame(() => {
        formElement.querySelector<HTMLElement>("[aria-invalid='true']")?.focus()
      })
      return
    }
    setErrors({})
    try {
      const updated = await mutation.mutateAsync({
        csrfToken,
        announcementId: announcement.id,
        body: {
          action,
          expected_version: announcement.version,
          scheduled_for:
            action === "schedule"
              ? new Date(result.data.scheduledFor).toISOString()
              : undefined,
          reason: result.data.reason,
        },
      })
      onSaved(updated, action)
    } catch {
      // The typed API problem stays in the dialog so the operator can retry.
    }
  }

  const destructive = action === "withdraw"
  const copy = publicationCopy(action, announcement)

  return (
    <Dialog open onOpenChange={handleOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <div className="flex items-center gap-2">
            {actionIcon(action)}
            <DialogTitle>{publicationActionLabel(action)}</DialogTitle>
          </div>
          <DialogDescription>{copy.description}</DialogDescription>
        </DialogHeader>

        <form
          id="announcement-publication-form"
          onSubmit={handleSubmit}
          noValidate
        >
          <div className="flex flex-col gap-4">
            <div className="rounded-lg border bg-muted/30 p-3">
              <p className="font-medium break-words">{announcement.title}</p>
              <p className="mt-1 text-xs text-muted-foreground">
                状态第 {announcement.version} 版 · 正文第
                {announcement.revision_number} 稿
              </p>
              {announcement.scheduled_for ? (
                <p className="mt-2 text-xs text-muted-foreground">
                  当前排期：{formatDateTime(announcement.scheduled_for)}
                </p>
              ) : null}
            </div>

            {mutation.isError ? (
              <Alert variant="destructive">
                <CircleAlertIcon />
                <AlertTitle>{publicationErrorTitle(mutation.error)}</AlertTitle>
                <AlertDescription>
                  {publicationErrorDescription(mutation.error)}
                </AlertDescription>
              </Alert>
            ) : null}

            {destructive ? (
              <Alert variant="destructive">
                <Undo2Icon />
                <AlertTitle>公开入口将立即停止访问</AlertTitle>
                <AlertDescription>
                  公告及其评论目标不会被删除，后续仍可在保留历史的前提下重新发布。
                </AlertDescription>
              </Alert>
            ) : null}

            <FieldGroup>
              {action === "schedule" ? (
                <Field data-invalid={Boolean(errors.scheduledFor)}>
                  <FieldLabel htmlFor="announcement-scheduled-for">
                    预约发布时间
                  </FieldLabel>
                  <Input
                    id="announcement-scheduled-for"
                    name="scheduledFor"
                    type="datetime-local"
                    min={minimumScheduleInputValue()}
                    disabled={isPending}
                    aria-invalid={Boolean(errors.scheduledFor)}
                    autoFocus
                  />
                  <FieldDescription>
                    时间按当前设备时区输入；到点后公开查询会自动切换到该修订。
                  </FieldDescription>
                  <FieldError
                    errors={
                      errors.scheduledFor
                        ? [{ message: errors.scheduledFor }]
                        : []
                    }
                  />
                </Field>
              ) : null}

              <Field data-invalid={Boolean(errors.reason)}>
                <FieldLabel htmlFor="announcement-publication-reason">
                  操作理由
                </FieldLabel>
                <Textarea
                  id="announcement-publication-reason"
                  name="reason"
                  rows={4}
                  maxLength={500}
                  placeholder={copy.reasonPlaceholder}
                  disabled={isPending}
                  aria-invalid={Boolean(errors.reason)}
                  autoFocus={action !== "schedule"}
                />
                <FieldDescription>
                  完整理由会安全保存，审计记录仅保留必要摘要。
                </FieldDescription>
                <FieldError
                  errors={errors.reason ? [{ message: errors.reason }] : []}
                />
              </Field>
            </FieldGroup>
          </div>
        </form>

        <DialogFooter>
          <Button
            type="button"
            variant="outline"
            disabled={isPending}
            onClick={() => onOpenChange(false)}
          >
            返回
          </Button>
          <Button
            type="submit"
            form="announcement-publication-form"
            variant={destructive ? "destructive" : "default"}
            disabled={isPending}
          >
            {isPending ? <Spinner /> : actionIcon(action)}
            {isPending ? "正在提交…" : publicationActionLabel(action)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function publicationCopy(
  action: AnnouncementPublicationAction,
  announcement: ManagedAnnouncementSummary
) {
  switch (action) {
    case "publish_now":
      return {
        description: `将已预览的第 ${announcement.revision_number} 稿立即设为公开版本。`,
        reasonPlaceholder: "可留空；系统会自动记录发布依据",
      }
    case "schedule":
      return {
        description: `为已预览的第 ${announcement.revision_number} 稿设置生效时间；到点前现有公开版本保持不变。`,
        reasonPlaceholder: "可留空；系统会自动记录排期依据",
      }
    case "cancel_schedule":
      return {
        description: "把尚未到点的预约修订退回草稿，不影响当前公开版本。",
        reasonPlaceholder: "可留空；系统会自动记录取消排期原因",
      }
    case "withdraw":
      return {
        description:
          "停止这篇公告的公开访问；公告地址、修订和评论历史均会保留。",
        reasonPlaceholder: "可留空；系统会自动记录撤回依据",
      }
  }
}

function actionIcon(action: AnnouncementPublicationAction) {
  switch (action) {
    case "schedule":
      return <CalendarClockIcon className="size-4" />
    case "publish_now":
      return <SendIcon className="size-4" />
    case "cancel_schedule":
    case "withdraw":
      return <Undo2Icon className="size-4" />
  }
}

function minimumScheduleInputValue() {
  const value = new Date(Date.now() + 60_000)
  const offset = value.getTimezoneOffset() * 60_000
  return new Date(value.getTime() - offset).toISOString().slice(0, 16)
}

function publicationErrorTitle(error: Error) {
  if (error instanceof ApiProblemError) {
    switch (error.code) {
      case "announcement_version_conflict":
        return "公告已被其他操作更新"
      case "announcement_publication_conflict":
        return "当前状态不接受这项操作"
      case "announcement_change_denied":
        return "当前职责不能执行这项操作"
      case "invalid_announcement":
        return "发布参数未通过服务端校验"
    }
  }
  return "发布状态变更失败"
}

function publicationErrorDescription(error: Error) {
  if (
    error instanceof ApiProblemError &&
    (error.code === "announcement_version_conflict" ||
      error.code === "announcement_publication_conflict")
  ) {
    return "后台列表正在读取最新版本。关闭对话框后，请基于新状态重新发起操作。"
  }
  return "本次发布状态没有改变；请核对权限、时间与变更理由后重试。"
}

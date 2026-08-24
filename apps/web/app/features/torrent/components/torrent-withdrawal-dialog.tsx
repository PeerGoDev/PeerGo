import * as React from "react"
import { CircleAlertIcon, LoaderCircleIcon, Trash2Icon } from "lucide-react"

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
import {
  Field,
  FieldDescription,
  FieldError,
  FieldLabel,
} from "~/components/ui/field"
import { Textarea } from "~/components/ui/textarea"
import { useSubmitTorrentWithdrawal } from "~/features/torrent/api/torrent-withdrawal.mutations"
import type { MyTorrentSubmissionPage } from "~/features/torrent/api/torrent.queries"
import { requestErrorDescription } from "~/shared/api/problem"

type Submission = MyTorrentSubmissionPage["items"][number]

export function TorrentWithdrawalDialog({
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
  onSubmitted: (request: { torrent_title: string }) => void
}) {
  const reasonId = React.useId()
  const [reason, setReason] = React.useState("")
  const requestId = React.useRef<string | undefined>(undefined)
  const mutation = useSubmitTorrentWithdrawal(userId)
  const reasonLength = Array.from(reason.trim()).length
  const reasonInvalid = reasonLength > 1000

  async function submit() {
    if (reasonLength > 1000) return
    requestId.current ??= globalThis.crypto.randomUUID()
    try {
      const result = await mutation.mutateAsync({
        torrentId: submission.id,
        csrfToken,
        idempotencyKey: requestId.current,
        body: { expected_version: submission.version, reason: reason.trim() },
      })
      onSubmitted(result)
    } catch {
      // Keep the exact reason and idempotency key for a safe network retry.
    }
  }

  return (
    <AlertDialog
      open
      onOpenChange={(open) => {
        if (!open && !mutation.isPending) onOpenChange(false)
      }}
    >
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogMedia>
            <Trash2Icon />
          </AlertDialogMedia>
          <AlertDialogTitle>申请撤回种子</AlertDialogTitle>
          <AlertDialogDescription>
            #{submission.id} · {submission.title}
          </AlertDialogDescription>
        </AlertDialogHeader>

        <Alert variant="destructive">
          <CircleAlertIcon />
          <AlertTitle>提交后会立即停止公开</AlertTitle>
          <AlertDescription>
            公开详情、下载入口和 Tracker
            准入会先关闭。管理员批准后只写入删除墓碑，原始种子、截图、账本与审核证据不会直接物理删除；驳回则恢复发布。
          </AlertDescription>
        </Alert>

        <Field data-invalid={reasonInvalid || mutation.isError || undefined}>
          <FieldLabel htmlFor={reasonId}>撤回理由</FieldLabel>
          <Textarea
            id={reasonId}
            value={reason}
            maxLength={1000}
            rows={4}
            disabled={mutation.isPending}
            aria-invalid={reasonInvalid || mutation.isError || undefined}
            placeholder="可留空；系统会自动记录撤回理由"
            onChange={(event) => {
              setReason(event.target.value)
              requestId.current = undefined
              if (mutation.isError) mutation.reset()
            }}
          />
          <FieldDescription>
            {reasonLength} / 1000；留空时由系统自动记录
          </FieldDescription>
          {reasonInvalid ? (
            <FieldError>撤回理由不能超过 1000 个字符。</FieldError>
          ) : null}
          {mutation.isError ? (
            <FieldError>
              {requestErrorDescription(
                mutation.error,
                "撤回申请未提交，请重新载入发布记录后重试。"
              )}
            </FieldError>
          ) : null}
        </Field>

        <AlertDialogFooter>
          <AlertDialogCancel disabled={mutation.isPending}>
            取消
          </AlertDialogCancel>
          <AlertDialogAction
            variant="destructive"
            disabled={mutation.isPending || reasonLength > 1000}
            onClick={() => void submit()}
          >
            {mutation.isPending ? (
              <LoaderCircleIcon
                data-icon="inline-start"
                className="animate-spin"
              />
            ) : (
              <Trash2Icon data-icon="inline-start" />
            )}
            {mutation.isPending ? "正在提交…" : "确认申请撤回"}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}

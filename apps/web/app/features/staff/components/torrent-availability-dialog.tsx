import * as React from "react"
import { CircleAlertIcon, LoaderCircleIcon } from "lucide-react"

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
import {
  Field,
  FieldDescription,
  FieldError,
  FieldLabel,
} from "~/components/ui/field"
import { Textarea } from "~/components/ui/textarea"
import {
  type ManagedTorrent,
  useChangeTorrentAvailability,
} from "~/features/staff/api/torrent-administration.queries"

export function TorrentAvailabilityDialog({
  torrent,
  csrfToken,
  onOpenChange,
  onChanged,
}: {
  torrent?: ManagedTorrent
  csrfToken: string
  onOpenChange: (open: boolean) => void
  onChanged: (message: string) => void
}) {
  const mutation = useChangeTorrentAvailability()
  const changeAvailabilityRequestId = React.useRef<string | undefined>(
    undefined
  )
  const [reason, setReason] = React.useState("")
  const isDisable = torrent?.state === "published"
  const action = isDisable ? "disable" : "restore"
  const reasonLength = Array.from(reason.trim()).length
  const reasonInvalid = reasonLength > 0 && reasonLength < 10

  React.useEffect(() => {
    if (!torrent) {
      setReason("")
      mutation.reset()
      changeAvailabilityRequestId.current = undefined
    }
  }, [torrent]) // eslint-disable-line react-hooks/exhaustive-deps

  async function handleSubmit() {
    if (!torrent || reasonLength < 10 || reasonLength > 1000) {
      return
    }
    try {
      const idempotencyKey =
        changeAvailabilityRequestId.current ?? crypto.randomUUID()
      changeAvailabilityRequestId.current = idempotencyKey
      await mutation.mutateAsync({
        torrentId: torrent.id,
        csrfToken,
        idempotencyKey,
        body: {
          expected_version: torrent.version,
          action,
          reason: reason.trim(),
        },
      })
      changeAvailabilityRequestId.current = undefined
      onChanged(`种子 #${torrent.id} 已${isDisable ? "下架" : "恢复发布"}。`)
      onOpenChange(false)
    } catch {
      // The mutation error stays in the dialog so the exact command can be
      // reviewed before the operator retries with a new idempotency key.
    }
  }

  return (
    <AlertDialog open={Boolean(torrent)} onOpenChange={onOpenChange}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogMedia>
            <CircleAlertIcon />
          </AlertDialogMedia>
          <AlertDialogTitle>
            {isDisable ? "下架种子" : "恢复种子"}
          </AlertDialogTitle>
          <AlertDialogDescription>
            {isDisable
              ? "下架后，公开目录、下载入口与 Tracker 准入会按同一版本关闭；原始种子文件不会删除。"
              : "恢复前会再次核对分类状态和可验证的种子文件位置，然后重新进入公开目录与 Tracker 准入。"}
          </AlertDialogDescription>
        </AlertDialogHeader>

        <div className="rounded-lg border bg-muted/30 p-3 text-sm">
          <div className="font-medium">
            #{torrent?.id} · {torrent?.title}
          </div>
          <div className="mt-1 text-xs text-muted-foreground">
            当前状态：{isDisable ? "已发布" : "已下架"} · 版本{" "}
            {torrent?.version}
          </div>
        </div>

        <Field data-invalid={reasonInvalid || mutation.isError || undefined}>
          <FieldLabel htmlFor="torrent-availability-reason">
            操作原因
          </FieldLabel>
          <Textarea
            id="torrent-availability-reason"
            value={reason}
            maxLength={1000}
            rows={4}
            aria-invalid={reasonInvalid || mutation.isError || undefined}
            placeholder="请输入至少 10 个字符，便于后续审计与交接"
            onChange={(event) => {
              setReason(event.target.value)
              if (mutation.isError) {
                mutation.reset()
                changeAvailabilityRequestId.current = undefined
              }
            }}
          />
          <FieldDescription>{reasonLength}/1000 字符</FieldDescription>
          {reasonInvalid ? (
            <FieldError>操作原因至少需要 10 个字符。</FieldError>
          ) : null}
          {mutation.isError ? (
            <FieldError>
              操作未提交，请刷新工作台核对最新状态后重试。
            </FieldError>
          ) : null}
        </Field>

        <AlertDialogFooter>
          <AlertDialogCancel disabled={mutation.isPending}>
            取消
          </AlertDialogCancel>
          <AlertDialogAction
            variant={isDisable ? "destructive" : "actionSuccess"}
            disabled={
              mutation.isPending || reasonLength < 10 || reasonLength > 1000
            }
            onClick={() => void handleSubmit()}
          >
            {mutation.isPending ? (
              <LoaderCircleIcon
                data-icon="inline-start"
                className="animate-spin"
              />
            ) : null}
            {mutation.isPending
              ? "提交中…"
              : isDisable
                ? "确认下架"
                : "确认恢复"}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}

import * as React from "react"
import {
  CheckCircle2Icon,
  CircleAlertIcon,
  FlagIcon,
  ShieldAlertIcon,
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
  DialogTrigger,
} from "~/components/ui/dialog"
import {
  Field,
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLabel,
} from "~/components/ui/field"
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
import { useWebSession } from "~/features/auth/api/session.mutations"
import { useCapabilities } from "~/features/authz/api/capabilities.queries"
import {
  type TorrentReportReasonCode,
  useCreateTorrentReport,
} from "~/features/torrent/api/torrent-report.mutations"
import { torrentReportReasonOptions } from "~/features/torrent/model/torrent-report"
import { ApiProblemError } from "~/shared/api/problem"

const maximumDetailsCharacters = 1_000

export function TorrentReportDialog({
  torrentId,
  torrentTitle,
}: {
  torrentId: number
  torrentTitle: string
}) {
  const session = useWebSession()
  const capabilities = useCapabilities(session.data?.user.id)
  const canReport =
    capabilities.data?.items.some(
      (item) => item.action === "torrent.report.create.self"
    ) ?? false
  const [open, setOpen] = React.useState(false)
  const [reasonCode, setReasonCode] =
    React.useState<TorrentReportReasonCode>("content_mismatch")
  const [details, setDetails] = React.useState("")
  const requestId = React.useRef<string>(undefined)
  const mutation = useCreateTorrentReport()
  const reasonFieldId = React.useId()
  const detailsFieldId = React.useId()
  const detailsCount = Array.from(details.trim()).length
  const detailsInvalid =
    detailsCount > maximumDetailsCharacters ||
    (reasonCode === "other" && detailsCount > 0 && detailsCount < 10)
  const canSubmit =
    Boolean(session.data) &&
    canReport &&
    !detailsInvalid &&
    (reasonCode !== "other" || detailsCount >= 10)

  if (!canReport) return null

  function resetAttempt() {
    requestId.current = undefined
    mutation.reset()
  }

  function changeOpen(nextOpen: boolean) {
    if (!nextOpen && mutation.isPending) return
    setOpen(nextOpen)
    if (!nextOpen) {
      setReasonCode("content_mismatch")
      setDetails("")
      requestId.current = undefined
      mutation.reset()
    }
  }

  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!session.data || !canSubmit) return
    requestId.current ??= globalThis.crypto.randomUUID()
    try {
      await mutation.mutateAsync({
        torrentId,
        csrfToken: session.data.csrf_token,
        idempotencyKey: requestId.current,
        body: { reason_code: reasonCode, details: details.trim() },
      })
    } catch {
      // Keep the request UUID for an exact safe retry after response loss.
    }
  }

  return (
    <Dialog open={open} onOpenChange={changeOpen}>
      <DialogTrigger
        render={<Button variant="outline" className="h-9 w-full" />}
      >
        <FlagIcon data-icon="inline-start" />
        举报种子
      </DialogTrigger>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>举报种子</DialogTitle>
          <DialogDescription className="break-all">
            #{torrentId} · {torrentTitle}
          </DialogDescription>
        </DialogHeader>

        {mutation.isSuccess ? (
          <Alert>
            <CheckCircle2Icon />
            <AlertTitle>举报已经提交</AlertTitle>
            <AlertDescription>
              后台只会看到举报类别和说明，不会在审核队列中显示你的身份。
            </AlertDescription>
          </Alert>
        ) : (
          <form id="torrent-report-form" onSubmit={submit} noValidate>
            <FieldGroup>
              <Alert>
                <ShieldAlertIcon />
                <AlertTitle>举报不会立即删除种子</AlertTitle>
                <AlertDescription>
                  多名成员的举报会聚合到同一案件；管理员确认后最多先临时下架，购买权益与原始证据不会被直接删除。
                </AlertDescription>
              </Alert>

              <Field>
                <FieldLabel htmlFor={reasonFieldId}>举报原因</FieldLabel>
                <Select
                  items={torrentReportReasonOptions}
                  value={reasonCode}
                  disabled={mutation.isPending}
                  onValueChange={(value) => {
                    if (!value) return
                    setReasonCode(value)
                    resetAttempt()
                  }}
                >
                  <SelectTrigger id={reasonFieldId} className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent alignItemWithTrigger={false}>
                    <SelectGroup>
                      <SelectLabel>问题类别</SelectLabel>
                      {torrentReportReasonOptions.map((option) => (
                        <SelectItem key={option.value} value={option.value}>
                          {option.label}
                        </SelectItem>
                      ))}
                    </SelectGroup>
                  </SelectContent>
                </Select>
              </Field>

              <Field data-invalid={detailsInvalid || undefined}>
                <FieldLabel htmlFor={detailsFieldId}>
                  详细说明{reasonCode === "other" ? "（必填）" : "（可选）"}
                </FieldLabel>
                <Textarea
                  id={detailsFieldId}
                  value={details}
                  rows={4}
                  maxLength={maximumDetailsCharacters + 1}
                  disabled={mutation.isPending}
                  aria-invalid={detailsInvalid || undefined}
                  placeholder="请描述你实际核对到的问题，不要填写密码、Tracker 地址或其他敏感信息。"
                  onChange={(event) => {
                    setDetails(event.target.value)
                    resetAttempt()
                  }}
                />
                <FieldDescription>
                  {detailsCount.toLocaleString("zh-CN")}/
                  {maximumDetailsCharacters.toLocaleString("zh-CN")}
                  {reasonCode === "other" ? " · 至少 10 个字符" : ""}
                </FieldDescription>
                {detailsInvalid ? (
                  <FieldError>
                    {detailsCount > maximumDetailsCharacters
                      ? "详细说明不能超过 1000 个字符。"
                      : "选择“其他原因”时至少需要 10 个字符。"}
                  </FieldError>
                ) : null}
              </Field>

              {mutation.isError ? (
                <Alert variant="destructive">
                  <CircleAlertIcon />
                  <AlertTitle>举报未能提交</AlertTitle>
                  <AlertDescription>
                    {torrentReportErrorMessage(mutation.error)}
                  </AlertDescription>
                </Alert>
              ) : null}
            </FieldGroup>
          </form>
        )}

        <DialogFooter>
          {mutation.isSuccess ? (
            <Button type="button" onClick={() => changeOpen(false)}>
              完成
            </Button>
          ) : (
            <>
              <Button
                type="button"
                variant="outline"
                disabled={mutation.isPending}
                onClick={() => changeOpen(false)}
              >
                取消
              </Button>
              <Button
                type="submit"
                form="torrent-report-form"
                disabled={mutation.isPending || !canSubmit}
              >
                {mutation.isPending ? (
                  <Spinner data-icon="inline-start" />
                ) : null}
                提交举报
              </Button>
            </>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function torrentReportErrorMessage(error: Error) {
  if (!(error instanceof ApiProblemError)) {
    return "网络连接异常，请稍后重试。"
  }
  switch (error.code) {
    case "torrent_report_self_denied":
      return "不能举报自己的种子；请在“我的发布”中使用撤回申请。"
    case "torrent_already_reported":
      return "你已经参与当前举报案件，请等待后台处理。"
    case "torrent_report_target_not_found":
      return "该种子已经停止公开，不能继续提交举报。"
    case "verified_email_required":
      return "请先验证当前账户邮箱。"
    case "csrf_invalid":
    case "web_session_required":
      return "登录状态已经变化，请刷新页面后重试。"
    case "torrent_report_idempotency_conflict":
      return "本次请求编号已经用于其他举报，请关闭窗口后重新发起。"
    default:
      return error.detail || "举报暂时无法提交，请稍后重试。"
  }
}

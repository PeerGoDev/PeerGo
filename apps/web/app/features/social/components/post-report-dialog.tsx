import * as React from "react"
import { CheckCircle2Icon, CircleAlertIcon, FlagIcon } from "lucide-react"

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
  type SocialPost,
  type SocialPostReportReasonCode,
  useCreateSocialPostReport,
} from "~/features/social/api/posts.queries"
import { postReportReasonOptions } from "~/features/social/model/post-moderation"
import { ApiProblemError } from "~/shared/api/problem"

const maxReportDetailsCharacters = 500

export function PostReportDialog({
  post,
  csrfToken,
  onOpenChange,
}: {
  post: SocialPost
  csrfToken: string
  onOpenChange: (open: boolean) => void
}) {
  const reasonFieldId = React.useId()
  const detailsFieldId = React.useId()
  const [reasonCode, setReasonCode] =
    React.useState<SocialPostReportReasonCode>("spam")
  const [details, setDetails] = React.useState("")
  const requestId = React.useRef<string>(undefined)
  const report = useCreateSocialPostReport()
  const detailsCount = Array.from(details).length
  const detailsInvalid = detailsCount > maxReportDetailsCharacters

  function resetAttempt() {
    requestId.current = undefined
    report.reset()
  }

  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (detailsInvalid) {
      return
    }
    requestId.current ??= globalThis.crypto.randomUUID()
    try {
      await report.mutateAsync({
        postId: post.id,
        csrfToken,
        idempotencyKey: requestId.current,
        reasonCode,
        details,
      })
    } catch {
      // Keep the request UUID while unchanged input remains safely retryable.
    }
  }

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open && !report.isPending) {
          onOpenChange(false)
        }
      }}
    >
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <FlagIcon />
            举报动态
          </DialogTitle>
          <DialogDescription>
            举报 @{post.author.display_name}{" "}
            的这条动态。审核人员只会看到问题类别和补充说明，不会在队列中看到举报人身份。
          </DialogDescription>
        </DialogHeader>

        {report.isSuccess ? (
          <Alert>
            <CheckCircle2Icon />
            <AlertTitle>举报已提交</AlertTitle>
            <AlertDescription>
              该问题已进入动态专属审核案件；无需重复提交。
            </AlertDescription>
          </Alert>
        ) : (
          <form id="post-report-form" onSubmit={submit} noValidate>
            <FieldGroup>
              <Field>
                <FieldLabel htmlFor={reasonFieldId}>问题类别</FieldLabel>
                <Select
                  items={postReportReasonOptions}
                  value={reasonCode}
                  onValueChange={(value) => {
                    if (!value) return
                    setReasonCode(value)
                    resetAttempt()
                  }}
                  disabled={report.isPending}
                >
                  <SelectTrigger id={reasonFieldId} className="w-full">
                    <SelectValue placeholder="选择问题类别" />
                  </SelectTrigger>
                  <SelectContent alignItemWithTrigger={false}>
                    <SelectGroup>
                      <SelectLabel>动态问题</SelectLabel>
                      {postReportReasonOptions.map((reason) => (
                        <SelectItem key={reason.value} value={reason.value}>
                          {reason.label}
                        </SelectItem>
                      ))}
                    </SelectGroup>
                  </SelectContent>
                </Select>
              </Field>

              <Field data-invalid={detailsInvalid || undefined}>
                <FieldLabel htmlFor={detailsFieldId}>
                  补充说明（可选）
                </FieldLabel>
                <Textarea
                  id={detailsFieldId}
                  value={details}
                  rows={4}
                  maxLength={maxReportDetailsCharacters + 1}
                  aria-invalid={detailsInvalid || undefined}
                  placeholder="简要说明需要审核的内容，请勿填写与问题无关的个人信息。"
                  disabled={report.isPending}
                  onChange={(event) => {
                    setDetails(event.target.value)
                    resetAttempt()
                  }}
                />
                <FieldDescription>
                  {detailsCount.toLocaleString("zh-CN")}/
                  {maxReportDetailsCharacters.toLocaleString("zh-CN")} ·
                  仅供站务审核使用
                </FieldDescription>
                {detailsInvalid ? (
                  <FieldError>补充说明不能超过 500 个字符。</FieldError>
                ) : null}
              </Field>

              {report.isError ? (
                <Alert variant="destructive">
                  <CircleAlertIcon />
                  <AlertTitle>举报未能提交</AlertTitle>
                  <AlertDescription>
                    {postReportErrorMessage(report.error)}
                  </AlertDescription>
                </Alert>
              ) : null}
            </FieldGroup>
          </form>
        )}

        <DialogFooter>
          <Button
            type="button"
            variant="outline"
            disabled={report.isPending}
            onClick={() => onOpenChange(false)}
          >
            {report.isSuccess ? "关闭" : "取消"}
          </Button>
          {!report.isSuccess ? (
            <Button
              type="submit"
              form="post-report-form"
              disabled={report.isPending || detailsInvalid}
            >
              {report.isPending ? <Spinner data-icon="inline-start" /> : null}
              提交举报
            </Button>
          ) : null}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function postReportErrorMessage(error: Error) {
  if (!(error instanceof ApiProblemError)) {
    return "网络连接异常，请稍后重试。"
  }
  switch (error.code) {
    case "csrf_invalid":
    case "session_required":
      return "登录状态已经变化，请刷新页面后重试。"
    case "social_post_already_reported":
      return "你已经举报过这条动态，无需重复提交。"
    case "social_post_report_target_not_found":
      return "这条动态已经删除、隐藏或不再公开。"
    case "social_post_report_self_denied":
      return "不能举报自己的动态；你可以直接编辑或删除它。"
    case "social_post_report_denied":
      return "当前账户暂时不能使用动态举报功能。"
    case "idempotency_conflict":
      return "举报内容已经变化，请关闭窗口后重新发起。"
    default:
      return "举报暂时无法保存，请稍后重试。"
  }
}

import * as React from "react"
import { CircleAlertIcon, ShieldCheckIcon } from "lucide-react"

import { Alert, AlertDescription, AlertTitle } from "~/components/ui/alert"
import { Badge } from "~/components/ui/badge"
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
  type PendingTorrentReview,
  type TorrentReviewDecisionRequest,
  type TorrentReviewDecisionResult,
  useDecideTorrentReview,
} from "~/features/staff/api/torrent-review.queries"
import { formatBytes } from "~/shared/formatters/bytes"
import { ApiProblemError } from "~/shared/api/problem"

type ReviewDecision = TorrentReviewDecisionRequest["decision"]
type ReviewReasonCode = TorrentReviewDecisionRequest["reason_code"]

const maximumReasonCharacters = 1_000
const decisionOptions: Array<{ value: ReviewDecision; label: string }> = [
  { value: "approve", label: "通过并发布" },
  { value: "reject", label: "驳回并要求处理" },
]
const rejectionReasonOptions: Array<{
  value: Exclude<ReviewReasonCode, "meets_requirements">
  label: string
}> = [
  { value: "metadata_incomplete", label: "元数据不完整" },
  { value: "duplicate_or_superseded", label: "重复或已有替代版本" },
  { value: "content_policy_violation", label: "不符合内容政策" },
  { value: "quality_requirements_not_met", label: "未达到质量要求" },
  { value: "uploader_action_required", label: "需要发布者处理" },
  { value: "other", label: "其他原因" },
]

export function TorrentReviewDecisionDialog({
  torrent,
  csrfToken,
  onOpenChange,
  onDecided,
}: {
  torrent: PendingTorrentReview
  csrfToken: string
  onOpenChange: (open: boolean) => void
  onDecided: (result: TorrentReviewDecisionResult) => void
}) {
  const decisionFieldId = React.useId()
  const reasonCodeFieldId = React.useId()
  const reasonFieldId = React.useId()
  const [decision, setDecision] = React.useState<ReviewDecision>("approve")
  const [rejectionReasonCode, setRejectionReasonCode] = React.useState<
    Exclude<ReviewReasonCode, "meets_requirements">
  >("metadata_incomplete")
  const [reason, setReason] = React.useState("")
  const requestId = React.useRef<string>(undefined)
  const decide = useDecideTorrentReview()
  const reasonCount = Array.from(reason.trim()).length
  const reasonInvalid = reasonCount > maximumReasonCharacters

  function resetAttempt() {
    requestId.current = undefined
    decide.reset()
  }

  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (reasonCount > maximumReasonCharacters) {
      return
    }
    requestId.current ??= globalThis.crypto.randomUUID()
    try {
      const result = await decide.mutateAsync({
        torrentId: torrent.id,
        csrfToken,
        idempotencyKey: requestId.current,
        body: {
          expected_version: torrent.version,
          decision,
          reason_code:
            decision === "approve" ? "meets_requirements" : rejectionReasonCode,
          reason: reason.trim(),
        },
      })
      onDecided(result)
    } catch {
      // Preserve the exact reviewed values and idempotency key for a safe retry.
    }
  }

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open && !decide.isPending) onOpenChange(false)
      }}
    >
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>审核种子</DialogTitle>
          <DialogDescription>
            对当前审核批次作出决定；保存时会重新检查状态、权限与种子文件可用性。
          </DialogDescription>
        </DialogHeader>

        <form id="torrent-review-decision-form" onSubmit={submit} noValidate>
          <FieldGroup>
            <div className="flex flex-col gap-1 rounded-md border bg-muted/30 p-3">
              <div className="flex flex-wrap items-center gap-2">
                <Badge variant="secondary">{torrent.category_name}</Badge>
                <span className="font-medium">{torrent.title}</span>
              </div>
              <p className="text-xs text-muted-foreground">
                {formatBytes(torrent.total_size_bytes)} ·{" "}
                {torrent.file_count.toLocaleString("zh-CN")} 个文件 · 上传者{" "}
                {torrent.uploader_display_name}
              </p>
            </div>

            <Alert>
              <ShieldCheckIcon />
              <AlertTitle>决定只作用于当前审核批次</AlertTitle>
              <AlertDescription>
                通过后会加入 Tracker
                发布队列；驳回只向上传者记录反馈，不修改原始 .torrent 文件。
              </AlertDescription>
            </Alert>

            <Field>
              <FieldLabel htmlFor={decisionFieldId}>审核决定</FieldLabel>
              <Select
                items={decisionOptions}
                value={decision}
                onValueChange={(value) => {
                  if (!value) return
                  setDecision(value)
                  resetAttempt()
                }}
                disabled={decide.isPending}
              >
                <SelectTrigger id={decisionFieldId} className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent alignItemWithTrigger={false}>
                  <SelectGroup>
                    <SelectLabel>审核决定</SelectLabel>
                    {decisionOptions.map((option) => (
                      <SelectItem key={option.value} value={option.value}>
                        {option.label}
                      </SelectItem>
                    ))}
                  </SelectGroup>
                </SelectContent>
              </Select>
            </Field>

            {decision === "reject" ? (
              <Field>
                <FieldLabel htmlFor={reasonCodeFieldId}>驳回类别</FieldLabel>
                <Select
                  items={rejectionReasonOptions}
                  value={rejectionReasonCode}
                  onValueChange={(value) => {
                    if (!value) return
                    setRejectionReasonCode(value)
                    resetAttempt()
                  }}
                  disabled={decide.isPending}
                >
                  <SelectTrigger id={reasonCodeFieldId} className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent alignItemWithTrigger={false}>
                    <SelectGroup>
                      <SelectLabel>反馈类别</SelectLabel>
                      {rejectionReasonOptions.map((option) => (
                        <SelectItem key={option.value} value={option.value}>
                          {option.label}
                        </SelectItem>
                      ))}
                    </SelectGroup>
                  </SelectContent>
                </Select>
              </Field>
            ) : null}

            <Field data-invalid={reasonInvalid || undefined}>
              <FieldLabel htmlFor={reasonFieldId}>审核说明</FieldLabel>
              <Textarea
                id={reasonFieldId}
                rows={5}
                maxLength={maximumReasonCharacters + 1}
                value={reason}
                aria-invalid={reasonInvalid || undefined}
                placeholder={
                  decision === "approve"
                    ? "可留空；系统会自动记录已完成发布要求核对。"
                    : "可留空；建议向上传者说明需要修改的内容。"
                }
                disabled={decide.isPending}
                onChange={(event) => {
                  setReason(event.target.value)
                  resetAttempt()
                }}
              />
              <FieldDescription>
                {reasonCount.toLocaleString("zh-CN")}/
                {maximumReasonCharacters.toLocaleString("zh-CN")} ·
                上传者会在自己的审核记录中看到该说明
              </FieldDescription>
              {reasonInvalid ? (
                <FieldError>审核说明不能超过 1000 个字符。</FieldError>
              ) : null}
            </Field>

            {decide.isError ? (
              <Alert variant="destructive">
                <CircleAlertIcon />
                <AlertTitle>审核决定未能保存</AlertTitle>
                <AlertDescription>
                  {torrentReviewErrorMessage(decide.error)}
                </AlertDescription>
              </Alert>
            ) : null}
          </FieldGroup>
        </form>

        <DialogFooter>
          <Button
            type="button"
            variant="outline"
            disabled={decide.isPending}
            onClick={() => onOpenChange(false)}
          >
            取消
          </Button>
          <Button
            type="submit"
            form="torrent-review-decision-form"
            variant={decision === "reject" ? "destructive" : "default"}
            disabled={decide.isPending || reasonCount > maximumReasonCharacters}
          >
            {decide.isPending ? <Spinner data-icon="inline-start" /> : null}
            {decision === "approve" ? "确认通过" : "确认驳回"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function torrentReviewErrorMessage(error: Error) {
  if (!(error instanceof ApiProblemError)) {
    return "网络连接异常，请稍后重试。"
  }
  switch (error.code) {
    case "staff_session_required":
    case "staff_session_expired":
    case "csrf_invalid":
      return "后台会话已经变化，请重新验证后再处理。"
    case "torrent_self_review_denied":
      return "不能审核自己上传的种子，请交由另一名审核员处理。"
    case "torrent_review_version_conflict":
    case "torrent_review_state_conflict":
      return "种子状态已经变化，队列正在刷新，请重新核对。"
    case "torrent_review_category_unavailable":
      return "分类当前不可发布，请先恢复分类状态。"
    case "torrent_review_object_unavailable":
      return "种子文件当前没有已验证的存储位置，请先完成存储恢复。"
    case "torrent_review_denied":
      return "当前后台权限不能审核种子。"
    case "torrent_review_idempotency_conflict":
      return "这个审核编号已经用于其他内容，请关闭窗口后重新发起。"
    default:
      return "审核决定暂时无法保存，请稍后重试。"
  }
}

import * as React from "react"
import { CircleAlertIcon, VoteIcon } from "lucide-react"

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
  type MyTorrentReviewAssignment,
  type TorrentReviewDecisionRequest,
  type TorrentReviewVoteResult,
  useCreateTorrentReviewVote,
} from "~/features/review/api/torrent-review-voting.queries"
import { ApiProblemError, requestErrorDescription } from "~/shared/api/problem"
import { formatBytes } from "~/shared/formatters/bytes"

type ReviewDecision = TorrentReviewDecisionRequest["decision"]
type ReviewReasonCode = TorrentReviewDecisionRequest["reason_code"]

const minimumReasonCharacters = 10
const maximumReasonCharacters = 1_000
const decisionOptions: Array<{ value: ReviewDecision; label: string }> = [
  { value: "approve", label: "赞成发布" },
  { value: "reject", label: "反对发布" },
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

export function TorrentReviewVoteDialog({
  torrent,
  csrfToken,
  onOpenChange,
  onVoted,
}: {
  torrent: MyTorrentReviewAssignment
  csrfToken: string
  onOpenChange: (open: boolean) => void
  onVoted: (result: TorrentReviewVoteResult) => void
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
  const vote = useCreateTorrentReviewVote()
  const reasonCount = Array.from(reason.trim()).length
  const reasonInvalid =
    reasonCount > 0 &&
    (reasonCount < minimumReasonCharacters ||
      reasonCount > maximumReasonCharacters)

  function resetAttempt() {
    requestId.current = undefined
    vote.reset()
  }

  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (
      reasonCount < minimumReasonCharacters ||
      reasonCount > maximumReasonCharacters
    ) {
      return
    }
    requestId.current ??= globalThis.crypto.randomUUID()
    try {
      const result = await vote.mutateAsync({
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
      onVoted(result)
    } catch {
      // Keep the same vote ID and form values so an uncertain response can be
      // retried idempotently without accidentally casting a second vote.
    }
  }

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open && !vote.isPending) onOpenChange(false)
      }}
    >
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>提交种审票</DialogTitle>
          <DialogDescription>
            请独立判断；提交前不会展示赞成票和反对票的分布。
          </DialogDescription>
        </DialogHeader>

        <form id="torrent-review-vote-form" onSubmit={submit} noValidate>
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
              <VoteIcon />
              <AlertTitle>不可变实名审核票</AlertTitle>
              <AlertDescription>
                系统保存您的有效种审组资格证据。达到结案条件时自动发布或驳回；四票
                2:2 时交由管理员处理。
              </AlertDescription>
            </Alert>

            <Field>
              <FieldLabel htmlFor={decisionFieldId}>审核意见</FieldLabel>
              <Select
                items={decisionOptions}
                value={decision}
                onValueChange={(value) => {
                  if (!value) return
                  setDecision(value)
                  resetAttempt()
                }}
                disabled={vote.isPending}
              >
                <SelectTrigger id={decisionFieldId} className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent alignItemWithTrigger={false}>
                  <SelectGroup>
                    <SelectLabel>审核意见</SelectLabel>
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
                <FieldLabel htmlFor={reasonCodeFieldId}>问题类别</FieldLabel>
                <Select
                  items={rejectionReasonOptions}
                  value={rejectionReasonCode}
                  onValueChange={(value) => {
                    if (!value) return
                    setRejectionReasonCode(value)
                    resetAttempt()
                  }}
                  disabled={vote.isPending}
                >
                  <SelectTrigger id={reasonCodeFieldId} className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent alignItemWithTrigger={false}>
                    <SelectGroup>
                      <SelectLabel>问题类别</SelectLabel>
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
                minLength={minimumReasonCharacters}
                maxLength={maximumReasonCharacters + 1}
                value={reason}
                aria-invalid={reasonInvalid || undefined}
                placeholder={
                  decision === "approve"
                    ? "写明已经核对的发布要求，至少 10 个字符。"
                    : "写明发现的问题和修改建议，至少 10 个字符。"
                }
                disabled={vote.isPending}
                onChange={(event) => {
                  setReason(event.target.value)
                  resetAttempt()
                }}
              />
              <FieldDescription>
                {reasonCount.toLocaleString("zh-CN")}/
                {maximumReasonCharacters.toLocaleString("zh-CN")}
              </FieldDescription>
              {reasonInvalid ? (
                <FieldError>审核说明需要 10–1000 个字符。</FieldError>
              ) : null}
            </Field>

            {vote.isError ? (
              <Alert variant="destructive">
                <CircleAlertIcon />
                <AlertTitle>种审票未能保存</AlertTitle>
                <AlertDescription>
                  {torrentReviewVoteErrorMessage(vote.error)}
                </AlertDescription>
              </Alert>
            ) : null}
          </FieldGroup>
        </form>

        <DialogFooter>
          <Button
            type="button"
            variant="outline"
            disabled={vote.isPending}
            onClick={() => onOpenChange(false)}
          >
            取消
          </Button>
          <Button
            type="submit"
            form="torrent-review-vote-form"
            variant={decision === "reject" ? "destructive" : "default"}
            disabled={
              vote.isPending ||
              reasonCount < minimumReasonCharacters ||
              reasonCount > maximumReasonCharacters
            }
          >
            {vote.isPending ? <Spinner data-icon="inline-start" /> : null}
            {decision === "approve" ? "提交赞成票" : "提交反对票"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function torrentReviewVoteErrorMessage(error: Error) {
  if (error instanceof ApiProblemError) {
    switch (error.code) {
      case "torrent_review_already_voted":
        return "您已经参与本轮审核，队列正在刷新。"
      case "torrent_review_round_escalated":
        return "本轮已经形成 2:2，现已转管理员处理。"
      case "torrent_self_review_denied":
        return "不能审核自己上传的种子。"
      case "torrent_review_version_conflict":
      case "torrent_review_state_conflict":
        return "种子状态已经变化，队列正在刷新，请重新核对。"
      case "torrent_review_membership_required":
        return "种审组资格不存在或已经失效。"
      case "csrf_invalid":
        return "登录会话已经变化，请刷新页面后重试。"
    }
  }
  return requestErrorDescription(error, "种审票暂时无法保存，请稍后重试。")
}

import * as React from "react"
import { CircleAlertIcon, EyeOffIcon, ShieldCheckIcon } from "lucide-react"

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
  type CommentModerationCase,
  type CommentModerationDecision,
  type CommentModerationDecisionReasonCode,
  type CommentModerationDecisionResult,
  useDecideCommentModerationCase,
} from "~/features/staff/api/comment-moderation.queries"
import { commentViolationReasonOptions } from "~/features/social/model/comment-moderation"
import {
  announcementCommentTarget,
  postCommentTarget,
  torrentCommentTarget,
} from "~/features/social/api/comments.queries"
import { ApiProblemError } from "~/shared/api/problem"

const maximumNoteCharacters = 1_000

export function CommentModerationDecisionDialog({
  moderationCase,
  csrfToken,
  onOpenChange,
  onResolved,
}: {
  moderationCase: CommentModerationCase
  csrfToken: string
  onOpenChange: (open: boolean) => void
  onResolved: (result: CommentModerationDecisionResult) => void
}) {
  const decisionFieldId = React.useId()
  const reasonFieldId = React.useId()
  const noteFieldId = React.useId()
  const [decision, setDecision] =
    React.useState<CommentModerationDecision>("dismiss")
  const [violationReason, setViolationReason] =
    React.useState<
      Exclude<CommentModerationDecisionReasonCode, "no_violation">
    >("spam")
  const [note, setNote] = React.useState("")
  const requestId = React.useRef<string>(undefined)
  const decide = useDecideCommentModerationCase()
  const noteCount = Array.from(note.trim()).length
  const noteInvalid = noteCount > maximumNoteCharacters
  const canHide = moderationCase.comment.state === "visible"
  const decisionOptions: Array<{
    value: CommentModerationDecision
    label: string
  }> = [
    { value: "dismiss", label: "确认无违规并关闭" },
    ...(canHide ? [{ value: "hide_comment" as const, label: "隐藏评论" }] : []),
  ]

  function resetAttempt() {
    requestId.current = undefined
    decide.reset()
  }

  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (noteCount > maximumNoteCharacters) {
      return
    }
    requestId.current ??= globalThis.crypto.randomUUID()
    const target =
      moderationCase.target.kind === "torrent"
        ? torrentCommentTarget(moderationCase.target.torrent_id ?? 0)
        : moderationCase.target.kind === "post"
          ? postCommentTarget(moderationCase.target.post_id ?? "")
          : announcementCommentTarget(
              moderationCase.target.announcement_id ?? ""
            )
    try {
      const result = await decide.mutateAsync({
        caseId: moderationCase.id,
        target,
        csrfToken,
        idempotencyKey: requestId.current,
        body: {
          expected_case_version: moderationCase.version,
          expected_comment_version: moderationCase.comment.version,
          decision,
          reason_code:
            decision === "dismiss" ? "no_violation" : violationReason,
          note: note.trim(),
        },
      })
      onResolved(result)
    } catch {
      // Keep reviewed values and the request UUID for an exact safe retry.
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
          <DialogTitle>处置评论举报</DialogTitle>
          <DialogDescription>
            保存时会再次确认案件和评论是否已被其他管理员处理，避免覆盖最新结果。
          </DialogDescription>
        </DialogHeader>

        <form
          id="comment-moderation-decision-form"
          onSubmit={submit}
          noValidate
        >
          <FieldGroup>
            <Alert>
              <ShieldCheckIcon />
              <AlertTitle>本次只处理这一条评论</AlertTitle>
              <AlertDescription>
                只能关闭案件或隐藏这一条评论；不会联动封禁用户、删除回复或修改原内容。
              </AlertDescription>
            </Alert>

            <Field>
              <FieldLabel htmlFor={decisionFieldId}>处置结果</FieldLabel>
              <Select
                items={decisionOptions}
                value={decision}
                onValueChange={(value) => {
                  if (!value) return
                  setDecision(value)
                  if (value === "hide_comment") setViolationReason("spam")
                  resetAttempt()
                }}
                disabled={decide.isPending}
              >
                <SelectTrigger id={decisionFieldId} className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent alignItemWithTrigger={false}>
                  <SelectGroup>
                    <SelectLabel>案件处置</SelectLabel>
                    {decisionOptions.map((option) => (
                      <SelectItem key={option.value} value={option.value}>
                        {option.label}
                      </SelectItem>
                    ))}
                  </SelectGroup>
                </SelectContent>
              </Select>
              {!canHide ? (
                <FieldDescription>
                  评论当前已不是公开正文，只能核对举报后关闭案件。
                </FieldDescription>
              ) : null}
            </Field>

            {decision === "hide_comment" ? (
              <Field>
                <FieldLabel htmlFor={reasonFieldId}>违规类别</FieldLabel>
                <Select
                  items={commentViolationReasonOptions}
                  value={violationReason}
                  onValueChange={(value) => {
                    if (!value) return
                    setViolationReason(value)
                    resetAttempt()
                  }}
                  disabled={decide.isPending}
                >
                  <SelectTrigger id={reasonFieldId} className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent alignItemWithTrigger={false}>
                    <SelectGroup>
                      <SelectLabel>隐藏原因</SelectLabel>
                      {commentViolationReasonOptions.map((option) => (
                        <SelectItem key={option.value} value={option.value}>
                          {option.label}
                        </SelectItem>
                      ))}
                    </SelectGroup>
                  </SelectContent>
                </Select>
              </Field>
            ) : (
              <FieldDescription>
                关闭案件会固定记录为“无违规”，评论正文保持不变。
              </FieldDescription>
            )}

            {decision === "hide_comment" ? (
              <Alert variant="destructive">
                <EyeOffIcon />
                <AlertTitle>评论将对用户隐藏</AlertTitle>
                <AlertDescription>
                  原正文会保留在审核记录中；原楼层和已有回复不会丢失。
                </AlertDescription>
              </Alert>
            ) : null}

            <Field data-invalid={noteInvalid || undefined}>
              <FieldLabel htmlFor={noteFieldId}>内部处置说明</FieldLabel>
              <Textarea
                id={noteFieldId}
                value={note}
                rows={5}
                maxLength={maximumNoteCharacters + 1}
                aria-invalid={noteInvalid || undefined}
                placeholder="可留空；系统自动记录，或填写核对依据和处置边界。"
                disabled={decide.isPending}
                onChange={(event) => {
                  setNote(event.target.value)
                  resetAttempt()
                }}
              />
              <FieldDescription>
                {noteCount.toLocaleString("zh-CN")}/
                {maximumNoteCharacters.toLocaleString("zh-CN")} ·
                完整说明仅供后台留档，对外记录只保留必要摘要
              </FieldDescription>
              {noteInvalid ? (
                <FieldError>内部说明不能超过 1000 个字符。</FieldError>
              ) : null}
            </Field>

            {decide.isError ? (
              <Alert variant="destructive">
                <CircleAlertIcon />
                <AlertTitle>处置未能保存</AlertTitle>
                <AlertDescription>
                  {moderationDecisionErrorMessage(decide.error)}
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
            form="comment-moderation-decision-form"
            variant={decision === "hide_comment" ? "destructive" : "default"}
            disabled={decide.isPending || noteCount > maximumNoteCharacters}
          >
            {decide.isPending ? <Spinner data-icon="inline-start" /> : null}
            {decision === "hide_comment" ? "确认隐藏" : "确认关闭"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function moderationDecisionErrorMessage(error: Error) {
  if (!(error instanceof ApiProblemError)) {
    return "网络连接异常，请稍后重试。"
  }
  switch (error.code) {
    case "staff_session_required":
    case "staff_session_expired":
    case "csrf_invalid":
      return "后台会话已经变化，请重新验证后再处理。"
    case "comment_moderation_conflict_of_interest":
      return "你是评论作者或本案举报人，必须交由另一名审核员处理。"
    case "comment_moderation_case_version_conflict":
    case "comment_moderation_comment_version_conflict":
    case "comment_moderation_case_state_conflict":
      return "案件或评论已经变化，队列正在刷新，请重新核对最新版本。"
    case "comment_moderation_denied":
      return "当前后台权限不能处理评论举报。"
    case "idempotency_conflict":
      return "这个处置编号已经用于其他内容，请关闭窗口后重新发起。"
    default:
      return "处置暂时无法保存，请稍后重试。"
  }
}

import * as React from "react"
import { useQuery } from "@tanstack/react-query"
import { Link } from "react-router"
import {
  CheckIcon,
  CircleAlertIcon,
  EyeOffIcon,
  FlagIcon,
  RefreshCwIcon,
  ShieldCheckIcon,
} from "lucide-react"

import { Alert, AlertDescription, AlertTitle } from "~/components/ui/alert"
import { Badge } from "~/components/ui/badge"
import { Button } from "~/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "~/components/ui/card"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "~/components/ui/dialog"
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "~/components/ui/empty"
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
  postReportReasonLabel,
  postViolationReasonOptions,
} from "~/features/social/model/post-moderation"
import {
  postModerationCasesQueryOptions,
  type SocialPostModerationCase,
  type SocialPostModerationDecisionResult,
  useDecidePostModerationCase,
} from "~/features/staff/api/post-moderation.queries"
import type { components } from "~/generated/api"
import { ApiProblemError, requestErrorDescription } from "~/shared/api/problem"
import { formatDateTime } from "~/shared/formatters/date-time"

type Decision = components["schemas"]["SocialPostModerationDecision"]
type ViolationReason = Exclude<
  components["schemas"]["SocialPostModerationDecisionReasonCode"],
  "no_violation"
>

const reviewPageSize = 20
const maximumNoteCharacters = 500

export function PostReportReview({
  enabled,
  canResolve,
  csrfToken,
  onDecided,
}: {
  enabled: boolean
  canResolve: boolean
  csrfToken: string
  onDecided: (message: string) => void
}) {
  const cases = useQuery({
    ...postModerationCasesQueryOptions(reviewPageSize, 0),
    enabled,
  })
  const [target, setTarget] = React.useState<SocialPostModerationCase>()

  if (!enabled) return null

  return (
    <>
      <Card className="gap-0 py-0">
        <CardHeader className="flex flex-row items-center justify-between gap-3 p-5 pb-3">
          <CardTitle className="flex items-center gap-2 text-base">
            <FlagIcon />
            动态举报审核
            {cases.data ? (
              <Badge variant="secondary">{cases.data.total}</Badge>
            ) : null}
          </CardTitle>
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            aria-label="刷新动态举报审核"
            disabled={cases.isFetching}
            onClick={() => void cases.refetch()}
          >
            <RefreshCwIcon />
          </Button>
        </CardHeader>
        <CardContent className="p-5 pt-0">
          {cases.isPending ? (
            <div className="flex min-h-20 items-center justify-center gap-2 text-sm text-muted-foreground">
              <Spinner /> 正在读取举报案件
            </div>
          ) : cases.isError ? (
            <Alert variant="destructive">
              <CircleAlertIcon />
              <AlertTitle>动态举报案件暂时无法读取</AlertTitle>
              <AlertDescription>
                {requestErrorDescription(cases.error, "请稍后重试。")}
              </AlertDescription>
            </Alert>
          ) : cases.data.items.length === 0 ? (
            <Empty className="min-h-28 border-0 py-4">
              <EmptyHeader>
                <EmptyMedia variant="icon">
                  <CheckIcon />
                </EmptyMedia>
                <EmptyTitle>暂无待处理动态举报</EmptyTitle>
                <EmptyDescription>
                  多名成员针对同一动态的举报会聚合到一条案件中。
                </EmptyDescription>
              </EmptyHeader>
            </Empty>
          ) : (
            <div className="divide-y rounded-md border">
              {cases.data.items.map((item) => {
                const latest = item.reports.at(-1)
                return (
                  <div
                    key={item.id}
                    className="flex flex-col gap-3 p-3 sm:flex-row sm:items-center sm:justify-between"
                  >
                    <div className="min-w-0">
                      <p className="truncate text-sm font-medium">
                        {postExcerpt(item)}
                      </p>
                      <p className="mt-1 text-xs text-muted-foreground">
                        @{item.post.author.display_name} ·{" "}
                        {item.post.board_name} · 最近举报{" "}
                        {formatDateTime(item.latest_reported_at)}
                      </p>
                      {latest ? (
                        <p className="mt-1 line-clamp-1 text-xs text-muted-foreground">
                          {postReportReasonLabel(latest.reason_code)}
                          {latest.details ? `：${latest.details}` : ""}
                        </p>
                      ) : null}
                    </div>
                    <div className="flex shrink-0 items-center gap-2">
                      <Badge variant="secondary">
                        {item.report_count} 人举报
                      </Badge>
                      {item.post.state !== "visible" ? (
                        <Badge variant="outline">
                          {postStateLabel(item.post.state)}
                        </Badge>
                      ) : null}
                      {canResolve ? (
                        <Button
                          type="button"
                          size="sm"
                          variant="outline"
                          onClick={() => setTarget(item)}
                        >
                          审核
                        </Button>
                      ) : (
                        <Badge variant="secondary">只读</Badge>
                      )}
                    </div>
                  </div>
                )
              })}
            </div>
          )}
        </CardContent>
      </Card>

      {target ? (
        <PostReportDecisionDialog
          key={`${target.id}:${target.version}:${target.post.version}`}
          moderationCase={target}
          csrfToken={csrfToken}
          onOpenChange={(open) => {
            if (!open) setTarget(undefined)
          }}
          onResolved={(result) => {
            setTarget(undefined)
            onDecided(decisionMessage(result))
          }}
        />
      ) : null}
    </>
  )
}

function PostReportDecisionDialog({
  moderationCase,
  csrfToken,
  onOpenChange,
  onResolved,
}: {
  moderationCase: SocialPostModerationCase
  csrfToken: string
  onOpenChange: (open: boolean) => void
  onResolved: (result: SocialPostModerationDecisionResult) => void
}) {
  const decisionFieldId = React.useId()
  const reasonFieldId = React.useId()
  const noteFieldId = React.useId()
  const [decision, setDecision] = React.useState<Decision>("dismiss")
  const [violationReason, setViolationReason] =
    React.useState<ViolationReason>("spam")
  const [note, setNote] = React.useState("")
  const requestId = React.useRef<string>(undefined)
  const decide = useDecidePostModerationCase()
  const noteCount = Array.from(note.trim()).length
  const noteInvalid = noteCount > maximumNoteCharacters
  const canHide = moderationCase.post.state === "visible"
  const decisionOptions: Array<{ value: Decision; label: string }> = [
    { value: "dismiss", label: "确认无违规并关闭" },
    ...(canHide ? [{ value: "hide_post" as const, label: "隐藏动态" }] : []),
  ]

  function resetAttempt() {
    requestId.current = undefined
    decide.reset()
  }

  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (noteInvalid) {
      return
    }
    requestId.current ??= globalThis.crypto.randomUUID()
    try {
      const result = await decide.mutateAsync({
        caseId: moderationCase.id,
        csrfToken,
        idempotencyKey: requestId.current,
        body: {
          expected_case_version: moderationCase.version,
          expected_post_version: moderationCase.post.version,
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
      <DialogContent className="max-h-[calc(100dvh-2rem)] overflow-y-auto sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>处置动态举报</DialogTitle>
          <DialogDescription>
            保存时会再次确认案件和动态是否已被其他管理员处理，避免覆盖最新结果。
          </DialogDescription>
        </DialogHeader>

        <form id="post-report-decision-form" onSubmit={submit} noValidate>
          <FieldGroup>
            <div className="rounded-md bg-muted/50 p-3">
              <p className="text-xs text-muted-foreground">
                @{moderationCase.post.author.display_name} ·{" "}
                {moderationCase.post.board_name} ·{" "}
                {formatDateTime(moderationCase.post.created_at)}
              </p>
              <p className="mt-1 line-clamp-4 text-sm leading-relaxed break-words whitespace-pre-wrap">
                {postExcerpt(moderationCase)}
              </p>
              {moderationCase.post.state === "visible" ? (
                <Link
                  to={`/social/post/${moderationCase.post.id}`}
                  className="mt-2 inline-block text-xs text-primary hover:underline"
                >
                  查看原动态
                </Link>
              ) : null}
            </div>

            <div className="flex flex-col gap-1">
              {moderationCase.reports.slice(-3).map((report, index) => (
                <p
                  key={`${report.created_at}:${index}`}
                  className="line-clamp-2 text-xs text-muted-foreground"
                >
                  {postReportReasonLabel(report.reason_code)}
                  {report.details ? `：${report.details}` : ""}
                </p>
              ))}
              <p className="text-xs text-muted-foreground">
                共 {moderationCase.report_count.toLocaleString("zh-CN")}{" "}
                份匿名举报
              </p>
            </div>

            <Alert>
              <ShieldCheckIcon />
              <AlertTitle>本次只处理这一条动态</AlertTitle>
              <AlertDescription>
                只能关闭案件或隐藏这一条动态；不会联动封禁用户、删除评论或修改原内容。
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
                  动态当前已不再公开，只能核对举报后关闭案件。
                </FieldDescription>
              ) : null}
            </Field>

            {decision === "hide_post" ? (
              <>
                <Field>
                  <FieldLabel htmlFor={reasonFieldId}>违规类别</FieldLabel>
                  <Select
                    items={postViolationReasonOptions}
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
                        {postViolationReasonOptions.map((option) => (
                          <SelectItem key={option.value} value={option.value}>
                            {option.label}
                          </SelectItem>
                        ))}
                      </SelectGroup>
                    </SelectContent>
                  </Select>
                </Field>
                <Alert variant="destructive">
                  <EyeOffIcon />
                  <AlertTitle>动态将对成员隐藏</AlertTitle>
                  <AlertDescription>
                    原正文与评论会保留，管理人员之后仍可在内容管理中恢复。
                  </AlertDescription>
                </Alert>
              </>
            ) : (
              <FieldDescription>
                关闭案件会固定记录为“无违规”，动态内容保持不变。
              </FieldDescription>
            )}

            <Field data-invalid={noteInvalid || undefined}>
              <FieldLabel htmlFor={noteFieldId}>内部处置说明</FieldLabel>
              <Textarea
                id={noteFieldId}
                value={note}
                rows={4}
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
                {maximumNoteCharacters.toLocaleString("zh-CN")} · 仅供后台留档
              </FieldDescription>
              {noteInvalid ? (
                <FieldError>内部说明不能超过 500 个字符。</FieldError>
              ) : null}
            </Field>

            {decide.isError ? (
              <Alert variant="destructive">
                <CircleAlertIcon />
                <AlertTitle>处置未能保存</AlertTitle>
                <AlertDescription>
                  {postReportDecisionErrorMessage(decide.error)}
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
            form="post-report-decision-form"
            variant={decision === "hide_post" ? "destructive" : "default"}
            disabled={decide.isPending || noteInvalid}
          >
            {decide.isPending ? <Spinner data-icon="inline-start" /> : null}
            {decision === "hide_post" ? "确认隐藏" : "确认关闭"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function postExcerpt(moderationCase: SocialPostModerationCase) {
  const { post } = moderationCase
  if (post.state === "author_deleted") return "该动态已由作者删除。"
  if (post.content.trim()) return post.content
  return post.media_count > 0
    ? `（无正文，含 ${post.media_count} 张图片）`
    : "（无正文）"
}

function postStateLabel(state: SocialPostModerationCase["post"]["state"]) {
  return state === "author_deleted" ? "作者已删除" : "已隐藏"
}

function decisionMessage(result: SocialPostModerationDecisionResult) {
  return result.decision === "hide_post"
    ? "动态已隐藏，举报案件已关闭并留下处置记录。"
    : "举报已按无违规关闭，动态内容保持不变。"
}

function postReportDecisionErrorMessage(error: Error) {
  if (!(error instanceof ApiProblemError)) {
    return "网络连接异常，请稍后重试。"
  }
  switch (error.code) {
    case "session_required":
    case "csrf_invalid":
      return "后台会话已经变化，请重新验证后再处理。"
    case "social_post_moderation_conflict_of_interest":
      return "你是动态作者或本案举报人，必须交由另一名审核员处理。"
    case "social_post_moderation_case_version_conflict":
    case "social_post_moderation_post_version_conflict":
    case "social_post_moderation_case_state_conflict":
      return "案件或动态已经变化，队列正在刷新，请重新核对最新版本。"
    case "social_post_moderation_case_not_found":
      return "案件不存在或已被处理，请刷新队列。"
    case "social_post_moderation_denied":
      return "当前后台权限不能处理动态举报。"
    case "idempotency_conflict":
      return "这个处置编号已经用于其他内容，请关闭窗口后重新发起。"
    default:
      return "处置暂时无法保存，请稍后重试。"
  }
}

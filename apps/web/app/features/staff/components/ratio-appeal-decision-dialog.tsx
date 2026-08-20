import * as React from "react"
import {
  CheckCircle2Icon,
  LoaderCircleIcon,
  ShieldAlertIcon,
  XCircleIcon,
} from "lucide-react"

import { Alert, AlertDescription, AlertTitle } from "~/components/ui/alert"
import { Button } from "~/components/ui/button"
import {
  Dialog,
  DialogClose,
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
import { Textarea } from "~/components/ui/textarea"
import { ToggleGroup, ToggleGroupItem } from "~/components/ui/toggle-group"
import {
  type RatioWatchAppeal,
  useDecideRatioWatchAppeal,
} from "~/features/staff/api/ratio-watch-administration.queries"
import { requestErrorDescription } from "~/shared/api/problem"
import { formatBytes } from "~/shared/formatters/bytes"

type AppealDecision = "approved" | "rejected"

export function RatioAppealDecisionDialog({
  appeal,
  csrfToken,
  onOpenChange,
}: {
  appeal: RatioWatchAppeal | null
  csrfToken: string
  onOpenChange: (open: boolean) => void
}) {
  const [decision, setDecision] = React.useState<AppealDecision | undefined>()
  const [response, setResponse] = React.useState("")
  const mutation = useDecideRatioWatchAppeal()
  const responseLength = Array.from(response.trim()).length

  React.useEffect(() => {
    if (!appeal) return
    setDecision(undefined)
    setResponse("")
    mutation.reset()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [appeal])

  async function handleSubmit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!appeal || !decision || responseLength < 10 || responseLength > 1000)
      return
    try {
      await mutation.mutateAsync({
        appealId: appeal.id,
        decision,
        expectedAssessmentVersion: appeal.assessment_version,
        response: response.trim(),
        csrfToken,
      })
      onOpenChange(false)
    } catch {
      // Preserve the decision and response when optimistic state has changed.
    }
  }

  return (
    <Dialog open={appeal !== null} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-xl">
        <form onSubmit={handleSubmit}>
          <DialogHeader>
            <DialogTitle>处理分享率申诉</DialogTitle>
            <DialogDescription>
              {appeal
                ? `${appeal.username}（ID ${appeal.user_numeric_id}）· 当前分享率 ${(appeal.current_ratio_basis_points / 10_000).toFixed(3)}`
                : "请选择一条待处理申诉。"}
            </DialogDescription>
          </DialogHeader>
          <FieldGroup className="py-5">
            {appeal ? (
              <div className="rounded-md border bg-muted/30 p-4">
                <div className="text-xs text-muted-foreground">用户说明</div>
                <p className="mt-1 text-sm leading-6 whitespace-pre-wrap">
                  {appeal.statement}
                </p>
                <div className="mt-3 text-xs text-muted-foreground">
                  有效上传 {formatBytes(appeal.current_credited_uploaded_bytes)}
                  {" · "}有效下载
                  {formatBytes(appeal.current_charged_downloaded_bytes)}
                </div>
              </div>
            ) : null}
            <Field>
              <FieldLabel>处理结果</FieldLabel>
              <ToggleGroup
                value={decision ? [decision] : []}
                onValueChange={(values) => {
                  const next = values[0]
                  if (next === "approved" || next === "rejected") {
                    setDecision(next)
                    mutation.reset()
                  }
                }}
                variant="outline"
                spacing={0}
                className="grid w-full grid-cols-2"
                aria-label="选择申诉处理结果"
              >
                <ToggleGroupItem value="approved" className="h-11">
                  <CheckCircle2Icon data-icon="inline-start" />
                  批准并解除考核
                </ToggleGroupItem>
                <ToggleGroupItem value="rejected" className="h-11">
                  <XCircleIcon data-icon="inline-start" />
                  驳回申诉
                </ToggleGroupItem>
              </ToggleGroup>
            </Field>
            {decision === "approved" ? (
              <Alert>
                <ShieldAlertIcon />
                <AlertTitle>批准会立即结束本期考核</AlertTitle>
                <AlertDescription>
                  操作会复用人工解除事务；PtYes
                  迁移或其他独立下载限制仍保持不变。
                </AlertDescription>
              </Alert>
            ) : null}
            <Field
              data-invalid={
                (responseLength > 0 && responseLength < 10) ||
                mutation.isError ||
                undefined
              }
            >
              <FieldLabel htmlFor="ratio-appeal-decision-response">
                给用户的处理意见
              </FieldLabel>
              <Textarea
                id="ratio-appeal-decision-response"
                value={response}
                maxLength={1000}
                rows={5}
                placeholder="说明核对结果和处理依据（至少 10 个字符）"
                onChange={(event) => {
                  setResponse(event.target.value)
                  mutation.reset()
                }}
              />
              <FieldDescription>{responseLength}/1000 个字符</FieldDescription>
              {mutation.isError ? (
                <FieldError>
                  {requestErrorDescription(
                    mutation.error,
                    "处理失败，请刷新申诉与考核状态后重试。"
                  )}
                </FieldError>
              ) : null}
            </Field>
          </FieldGroup>
          <DialogFooter>
            <DialogClose render={<Button variant="outline" />}>
              取消
            </DialogClose>
            <Button
              type="submit"
              variant={decision === "rejected" ? "destructive" : "default"}
              disabled={
                !appeal ||
                !decision ||
                responseLength < 10 ||
                responseLength > 1000 ||
                mutation.isPending
              }
            >
              {mutation.isPending ? (
                <LoaderCircleIcon
                  data-icon="inline-start"
                  className="animate-spin"
                />
              ) : decision === "approved" ? (
                <CheckCircle2Icon data-icon="inline-start" />
              ) : (
                <XCircleIcon data-icon="inline-start" />
              )}
              确认处理
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

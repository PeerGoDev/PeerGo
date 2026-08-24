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
  type AccountAccessAppeal,
  useDecideAccountAccessAppeal,
} from "~/features/staff/api/account-access-appeal-administration.queries"
import { requestErrorDescription } from "~/shared/api/problem"

type AppealDecision = "approved" | "rejected"

export function AccountAccessAppealDecisionDialog({
  appeal,
  csrfToken,
  onOpenChange,
}: {
  appeal: AccountAccessAppeal | null
  csrfToken: string
  onOpenChange: (open: boolean) => void
}) {
  const [decision, setDecision] = React.useState<AppealDecision>()
  const [response, setResponse] = React.useState("")
  const mutation = useDecideAccountAccessAppeal()
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
    if (!appeal || !decision || responseLength > 1000) return
    try {
      await mutation.mutateAsync({
        appealId: appeal.id,
        decision,
        expectedSourceVersion: appeal.restriction.source_version,
        response: response.trim(),
        csrfToken,
      })
      onOpenChange(false)
    } catch {
      // Preserve the reviewed input for a safe retry or conflict correction.
    }
  }

  const disabledAccount = appeal?.restriction.source_kind === "disabled_account"
  const manualDownload =
    appeal?.restriction.source_kind === "manual_download_restriction"
  return (
    <Dialog open={appeal !== null} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-xl">
        <form onSubmit={handleSubmit}>
          <DialogHeader>
            <DialogTitle>处理账户与下载限制申诉</DialogTitle>
            <DialogDescription>
              {appeal
                ? `${appeal.username}（ID ${appeal.user_numeric_id}）· ${disabledAccount ? "账户封禁" : manualDownload ? "旧站 / 人工下载限制" : "临时访问限制"}`
                : "请选择一条待处理申诉。"}
            </DialogDescription>
          </DialogHeader>
          <FieldGroup className="py-5">
            {appeal ? (
              <div className="rounded-md border bg-muted/30 p-4">
                <div className="text-sm font-medium">
                  {appeal.restriction.reason_summary}
                </div>
                <p className="mt-3 text-sm leading-6 whitespace-pre-wrap">
                  {appeal.statement}
                </p>
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
                aria-label="选择账户访问申诉处理结果"
              >
                <ToggleGroupItem value="approved" className="h-11">
                  <CheckCircle2Icon />
                  批准并解除限制
                </ToggleGroupItem>
                <ToggleGroupItem value="rejected" className="h-11">
                  <XCircleIcon />
                  驳回申诉
                </ToggleGroupItem>
              </ToggleGroup>
            </Field>
            {decision === "approved" ? (
              <Alert>
                <ShieldAlertIcon />
                <AlertTitle>只解除本次申诉对应的访问限制</AlertTitle>
                <AlertDescription>
                  {disabledAccount
                    ? "批准会恢复该账户的登录凭据和 Core 账户状态，不会清除下载限制、分享率考核或 H&R。"
                    : manualDownload
                      ? "批准只解除旧站迁入或人工设置的下载限制；长期分享率和 H&R 来源仍独立生效。"
                      : "批准只撤销快照中的临时限制；后续新增限制不会被一并解除。"}
                </AlertDescription>
              </Alert>
            ) : null}
            <Field data-invalid={mutation.isError || undefined}>
              <FieldLabel htmlFor="account-access-decision-response">
                给用户的处理意见
              </FieldLabel>
              <Textarea
                id="account-access-decision-response"
                value={response}
                maxLength={1000}
                rows={5}
                placeholder="可留空；系统自动记录，或填写核对结果和处理依据"
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
                    "处理失败，请刷新申诉和账户状态后重试。"
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
                responseLength > 1000 ||
                mutation.isPending
              }
            >
              {mutation.isPending ? (
                <LoaderCircleIcon className="animate-spin" />
              ) : decision === "approved" ? (
                <CheckCircle2Icon />
              ) : (
                <XCircleIcon />
              )}
              确认处理
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

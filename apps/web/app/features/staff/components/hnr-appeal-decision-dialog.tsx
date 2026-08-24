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
  type HNRAppeal,
  useDecideHNRAppeal,
} from "~/features/staff/api/hnr-appeal-administration.queries"
import {
  formatHNRDuration,
  formatHNRRatio,
} from "~/features/traffic/model/hnr-format"
import { requestErrorDescription } from "~/shared/api/problem"

type AppealDecision = "approved" | "rejected"

export function HNRAppealDecisionDialog({
  appeal,
  csrfToken,
  onOpenChange,
}: {
  appeal: HNRAppeal | null
  csrfToken: string
  onOpenChange: (open: boolean) => void
}) {
  const [decision, setDecision] = React.useState<AppealDecision | undefined>()
  const [response, setResponse] = React.useState("")
  const mutation = useDecideHNRAppeal()
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
        expectedObligationVersion: appeal.obligation_version,
        response: response.trim(),
        csrfToken,
      })
      onOpenChange(false)
    } catch {
      // Keep the decision and response so a version conflict can be reviewed.
    }
  }

  return (
    <Dialog open={appeal !== null} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-xl">
        <form onSubmit={handleSubmit}>
          <DialogHeader>
            <DialogTitle>处理 H&amp;R 申诉</DialogTitle>
            <DialogDescription>
              {appeal
                ? `${appeal.username}（ID ${appeal.user_numeric_id}）· 种子 #${appeal.torrent.id}`
                : "请选择一条待处理申诉。"}
            </DialogDescription>
          </DialogHeader>
          <FieldGroup className="py-5">
            {appeal ? (
              <div className="rounded-md border bg-muted/30 p-4">
                <div className="text-xs text-muted-foreground">
                  {appeal.torrent.title}
                </div>
                <p className="mt-2 text-sm leading-6 whitespace-pre-wrap">
                  {appeal.statement}
                </p>
                <div className="mt-3 text-xs text-muted-foreground">
                  已做种 {formatHNRDuration(appeal.seeded_seconds)} /{" "}
                  {formatHNRDuration(appeal.required_seed_seconds)}
                  {" · "}实际分享率{" "}
                  {formatHNRRatio(appeal.raw_ratio_basis_points)}
                  {" / "}
                  {formatHNRRatio(appeal.required_ratio_basis_points)}
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
                aria-label="选择 H&R 申诉处理结果"
              >
                <ToggleGroupItem value="approved" className="h-11">
                  <CheckCircle2Icon data-icon="inline-start" />
                  批准并豁免本条
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
                <AlertTitle>批准只豁免这一条 H&amp;R 义务</AlertTitle>
                <AlertDescription>
                  其他 H&amp;R、长期分享率或账户来源的下载限制不会被一并解除。
                </AlertDescription>
              </Alert>
            ) : null}
            <Field
              data-invalid={
                responseLength > 1000 || mutation.isError || undefined
              }
            >
              <FieldLabel htmlFor="hnr-appeal-decision-response">
                给用户的处理意见
              </FieldLabel>
              <Textarea
                id="hnr-appeal-decision-response"
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
                    "处理失败，请刷新申诉与 H&R 状态后重试。"
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

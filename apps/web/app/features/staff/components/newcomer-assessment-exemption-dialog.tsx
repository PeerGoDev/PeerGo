import * as React from "react"
import { CircleAlertIcon, ShieldCheckIcon } from "lucide-react"

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
import { Spinner } from "~/components/ui/spinner"
import {
  type NewcomerAssessment,
  useExemptNewcomerAssessment,
} from "~/features/staff/api/newcomer-administration.queries"
import { requestErrorDescription } from "~/shared/api/problem"

export function NewcomerAssessmentExemptionDialog({
  assessment,
  csrfToken,
  onOpenChange,
}: {
  assessment: NewcomerAssessment | null
  csrfToken: string
  onOpenChange: (open: boolean) => void
}) {
  const [reason, setReason] = React.useState("")
  const [idempotencyKey, setIdempotencyKey] = React.useState(() =>
    crypto.randomUUID()
  )
  const mutation = useExemptNewcomerAssessment()
  const reasonLength = Array.from(reason.trim()).length

  React.useEffect(() => {
    if (!assessment) return
    setReason("")
    setIdempotencyKey(crypto.randomUUID())
    mutation.reset()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [assessment])

  async function handleSubmit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!assessment || reasonLength < 10 || reasonLength > 1000) return
    try {
      await mutation.mutateAsync({
        assessmentId: assessment.id,
        expectedVersion: assessment.version,
        reason: reason.trim(),
        csrfToken,
        idempotencyKey,
      })
      onOpenChange(false)
    } catch {
      // Keep the reviewed reason and idempotency key for a safe retry.
    }
  }

  return (
    <Dialog open={assessment !== null} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <form onSubmit={handleSubmit}>
          <DialogHeader>
            <DialogTitle>豁免新人考核</DialogTitle>
            <DialogDescription>
              {assessment
                ? `用户 ${assessment.username}（ID ${assessment.user_numeric_id}）`
                : "请选择一条考核记录。"}
            </DialogDescription>
          </DialogHeader>
          <FieldGroup className="py-5">
            <Alert>
              <CircleAlertIcon />
              <AlertTitle>只结束这一条新人考核</AlertTitle>
              <AlertDescription>
                人工、长期分享率或 H&amp;R 等其他下载限制不会被清除。
              </AlertDescription>
            </Alert>
            <Field
              data-invalid={
                (reasonLength > 0 && reasonLength < 10) ||
                mutation.isError ||
                undefined
              }
            >
              <FieldLabel htmlFor="newcomer-exemption-reason">
                豁免原因
              </FieldLabel>
              <Textarea
                id="newcomer-exemption-reason"
                value={reason}
                maxLength={1000}
                placeholder="说明核对结果和豁免依据（至少 10 个字符）"
                aria-invalid={
                  (reasonLength > 0 && reasonLength < 10) || mutation.isError
                }
                onChange={(event) => {
                  setReason(event.target.value)
                  mutation.reset()
                }}
              />
              <FieldDescription>{reasonLength}/1000 个字符</FieldDescription>
              {mutation.isError ? (
                <FieldError>
                  {requestErrorDescription(
                    mutation.error,
                    "豁免失败，请刷新考核列表后重试。"
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
              disabled={
                !assessment ||
                reasonLength < 10 ||
                reasonLength > 1000 ||
                mutation.isPending
              }
            >
              {mutation.isPending ? (
                <Spinner data-icon="inline-start" />
              ) : (
                <ShieldCheckIcon data-icon="inline-start" />
              )}
              确认豁免
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

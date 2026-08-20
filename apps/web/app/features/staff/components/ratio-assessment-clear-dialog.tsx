import * as React from "react"
import {
  CircleAlertIcon,
  LoaderCircleIcon,
  ShieldCheckIcon,
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
import {
  type RatioWatchAssessment,
  useClearRatioWatchAssessment,
} from "~/features/staff/api/ratio-watch-administration.queries"
import { requestErrorDescription } from "~/shared/api/problem"

export function RatioAssessmentClearDialog({
  assessment,
  csrfToken,
  onOpenChange,
}: {
  assessment: RatioWatchAssessment | null
  csrfToken: string
  onOpenChange: (open: boolean) => void
}) {
  const [reason, setReason] = React.useState("")
  const mutation = useClearRatioWatchAssessment()
  const reasonLength = Array.from(reason.trim()).length

  React.useEffect(() => {
    if (!assessment) return
    setReason("")
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
      })
      onOpenChange(false)
    } catch {
      // Keep the reason visible on an optimistic-version conflict.
    }
  }

  return (
    <Dialog
      open={assessment !== null}
      onOpenChange={(open) => onOpenChange(open)}
    >
      <DialogContent className="sm:max-w-lg">
        <form onSubmit={handleSubmit}>
          <DialogHeader>
            <DialogTitle>人工解除分享率考核</DialogTitle>
            <DialogDescription>
              {assessment
                ? `用户 ${assessment.username}（ID ${assessment.user_numeric_id}）`
                : "请选择一条考核记录。"}
            </DialogDescription>
          </DialogHeader>
          <FieldGroup className="py-5">
            {assessment?.legacy_download_restricted ? (
              <Alert>
                <CircleAlertIcon />
                <AlertTitle>该用户还有独立下载限制</AlertTitle>
                <AlertDescription>
                  本操作只解除长期分享率考核；PtYes
                  迁移或管理员手工限制仍然生效。
                </AlertDescription>
              </Alert>
            ) : null}
            <Field
              data-invalid={
                (reasonLength > 0 && reasonLength < 10) ||
                mutation.isError ||
                undefined
              }
            >
              <FieldLabel htmlFor="ratio-assessment-clear-reason">
                解除原因
              </FieldLabel>
              <Textarea
                id="ratio-assessment-clear-reason"
                value={reason}
                maxLength={1000}
                placeholder="说明核对结果和解除依据（至少 10 个字符）"
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
                    "解除失败，请刷新考核列表后重试。"
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
                <LoaderCircleIcon
                  data-icon="inline-start"
                  className="animate-spin"
                />
              ) : (
                <ShieldCheckIcon data-icon="inline-start" />
              )}
              确认解除
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

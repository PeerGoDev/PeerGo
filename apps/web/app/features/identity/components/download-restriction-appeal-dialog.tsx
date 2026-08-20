import * as React from "react"
import { LoaderCircleIcon, MessageSquareWarningIcon } from "lucide-react"

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
import { useSubmitDownloadRestrictionAppeal } from "~/features/identity/api/download-restriction.queries"
import { requestErrorDescription } from "~/shared/api/problem"

export function DownloadRestrictionAppealDialog({
  open,
  userId,
  csrfToken,
  onOpenChange,
}: {
  open: boolean
  userId: string
  csrfToken: string
  onOpenChange: (open: boolean) => void
}) {
  const [statement, setStatement] = React.useState("")
  const idempotencyKey = React.useRef(crypto.randomUUID())
  const mutation = useSubmitDownloadRestrictionAppeal(userId)
  const statementLength = Array.from(statement.trim()).length

  React.useEffect(() => {
    if (!open) return
    setStatement("")
    idempotencyKey.current = crypto.randomUUID()
    mutation.reset()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open])

  async function handleSubmit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (statementLength < 20 || statementLength > 1000) return
    try {
      await mutation.mutateAsync({
        csrfToken,
        idempotencyKey: idempotencyKey.current,
        statement: statement.trim(),
      })
      onOpenChange(false)
    } catch {
      // Keep the statement and idempotency key stable for a safe retry.
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <form onSubmit={handleSubmit}>
          <DialogHeader>
            <DialogTitle>提交下载限制申诉</DialogTitle>
            <DialogDescription>
              这里只复核旧站迁入或人工设置的下载限制。分享率考核与 H&amp;R
              需要在各自页面处理。
            </DialogDescription>
          </DialogHeader>
          <FieldGroup className="py-5">
            <Field
              data-invalid={
                (statementLength > 0 && statementLength < 20) ||
                mutation.isError ||
                undefined
              }
            >
              <FieldLabel htmlFor="download-restriction-appeal-statement">
                情况说明
              </FieldLabel>
              <Textarea
                id="download-restriction-appeal-statement"
                value={statement}
                maxLength={1000}
                rows={6}
                placeholder="请说明限制需要复核的原因和必要依据（至少 20 个字符）"
                aria-invalid={
                  (statementLength > 0 && statementLength < 20) ||
                  mutation.isError ||
                  undefined
                }
                onChange={(event) => {
                  setStatement(event.target.value)
                  mutation.reset()
                }}
              />
              <FieldDescription>
                {statementLength}/1000 个字符；同一限制版本只能提交一次
              </FieldDescription>
              {mutation.isError ? (
                <FieldError>
                  {requestErrorDescription(
                    mutation.error,
                    "申诉提交失败，请刷新当前限制后重试。"
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
                statementLength < 20 ||
                statementLength > 1000 ||
                mutation.isPending
              }
            >
              {mutation.isPending ? (
                <LoaderCircleIcon
                  data-icon="inline-start"
                  className="animate-spin"
                />
              ) : (
                <MessageSquareWarningIcon data-icon="inline-start" />
              )}
              提交申诉
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

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
import { useSubmitMyRatioWatchAppeal } from "~/features/traffic/api/ratio-watch.queries"
import { requestErrorDescription } from "~/shared/api/problem"

export function RatioWatchAppealDialog({
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
  const mutation = useSubmitMyRatioWatchAppeal(userId)
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
      // Keep the statement and idempotency key for a safe retry.
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <form onSubmit={handleSubmit}>
          <DialogHeader>
            <DialogTitle>提交分享率申诉</DialogTitle>
            <DialogDescription>
              每期考核只能提交一次。请说明需要人工核对的异常或特殊情况；继续做种仍是自动恢复分享率的主要方式。
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
              <FieldLabel htmlFor="ratio-watch-appeal-statement">
                情况说明
              </FieldLabel>
              <Textarea
                id="ratio-watch-appeal-statement"
                value={statement}
                maxLength={1000}
                rows={6}
                placeholder="请写明发生了什么、希望管理员核对哪些记录（至少 20 个字符）"
                onChange={(event) => {
                  setStatement(event.target.value)
                  mutation.reset()
                }}
              />
              <FieldDescription>
                {statementLength}/1000 个字符；提交后不能修改或重复提交
              </FieldDescription>
              {mutation.isError ? (
                <FieldError>
                  {requestErrorDescription(
                    mutation.error,
                    "申诉提交失败，请刷新考核状态后重试。"
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

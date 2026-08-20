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
import {
  type HitAndRunPageData,
  useSubmitMyHNRAppeal,
} from "~/features/traffic/api/hnr.queries"
import { requestErrorDescription } from "~/shared/api/problem"

type HitAndRunEntry = HitAndRunPageData["items"][number]

export function HNRAppealDialog({
  entry,
  userId,
  csrfToken,
  onOpenChange,
}: {
  entry: HitAndRunEntry | null
  userId: string
  csrfToken: string
  onOpenChange: (open: boolean) => void
}) {
  const [statement, setStatement] = React.useState("")
  const idempotencyKey = React.useRef(crypto.randomUUID())
  const mutation = useSubmitMyHNRAppeal(userId)
  const statementLength = Array.from(statement.trim()).length

  React.useEffect(() => {
    if (!entry) return
    setStatement("")
    idempotencyKey.current = crypto.randomUUID()
    mutation.reset()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [entry])

  async function handleSubmit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!entry || statementLength < 20 || statementLength > 1000) return
    try {
      await mutation.mutateAsync({
        obligationId: entry.id,
        csrfToken,
        idempotencyKey: idempotencyKey.current,
        statement: statement.trim(),
      })
      onOpenChange(false)
    } catch {
      // Preserve both the text and idempotency key so retry stays safe.
    }
  }

  return (
    <Dialog open={entry !== null} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <form onSubmit={handleSubmit}>
          <DialogHeader>
            <DialogTitle>提交 H&amp;R 申诉</DialogTitle>
            <DialogDescription>
              {entry
                ? `针对《${entry.torrent.title}》的待补做记录提交一次人工核对。`
                : "请选择一条待补做记录。"}
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
              <FieldLabel htmlFor="hnr-appeal-statement">情况说明</FieldLabel>
              <Textarea
                id="hnr-appeal-statement"
                value={statement}
                maxLength={1000}
                rows={6}
                placeholder="请说明需要核对的客户端、做种记录或特殊情况（至少 20 个字符）"
                onChange={(event) => {
                  setStatement(event.target.value)
                  mutation.reset()
                }}
              />
              <FieldDescription>
                {statementLength}/1000
                个字符；每条义务只能提交一次，提交后不能修改
              </FieldDescription>
              {mutation.isError ? (
                <FieldError>
                  {requestErrorDescription(
                    mutation.error,
                    "申诉提交失败，请刷新 H&R 状态后重试。"
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
                !entry ||
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

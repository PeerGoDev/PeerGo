import * as React from "react"
import {
  GraduationCapIcon,
  LockKeyholeOpenIcon,
  ShieldAlertIcon,
} from "lucide-react"

import { Alert, AlertDescription, AlertTitle } from "~/components/ui/alert"
import { Badge } from "~/components/ui/badge"
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
  FieldLabel,
} from "~/components/ui/field"
import { Spinner } from "~/components/ui/spinner"
import { Textarea } from "~/components/ui/textarea"
import { useAssignNewcomerAssessment } from "~/features/staff/api/newcomer-administration.queries"
import {
  type ManagedUserDetail,
  useReactivateManagedUser,
} from "~/features/staff/api/user-administration.queries"
import { requestErrorDescription } from "~/shared/api/problem"

export function ManagedUserAccountActions({
  detail,
  csrfToken,
  currentStaffUserId,
  canReactivate,
  canAssignAssessment,
}: {
  detail: ManagedUserDetail
  csrfToken: string
  currentStaffUserId: string
  canReactivate: boolean
  canAssignAssessment: boolean
}) {
  const [reactivationOpen, setReactivationOpen] = React.useState(false)
  const [assignmentOpen, setAssignmentOpen] = React.useState(false)
  const isSelf = detail.id === currentStaffUserId

  return (
    <section className="flex flex-col gap-3">
      <div className="flex items-start justify-between gap-3">
        <div>
          <h2 className="font-heading font-medium">账户状态与新人考核</h2>
          <p className="text-xs text-muted-foreground">
            处理永久封禁状态，并为既有用户补分配当前新人考核。
          </p>
        </div>
        <Badge variant="destructive">高风险</Badge>
      </div>

      {detail.status === "disabled" ? (
        <Alert variant="destructive">
          <ShieldAlertIcon />
          <AlertTitle>账户已封禁</AlertTitle>
          <AlertDescription>
            解封会同时恢复登录凭据；旧会话不会恢复，用户需要重新登录。
          </AlertDescription>
          {canReactivate && !isSelf ? (
            <Button
              size="sm"
              variant="outline"
              onClick={() => setReactivationOpen(true)}
            >
              <LockKeyholeOpenIcon data-icon="inline-start" />
              解封账户
            </Button>
          ) : null}
        </Alert>
      ) : detail.status === "active" ? (
        <Alert>
          <GraduationCapIcon />
          <AlertTitle>新人考核分配</AlertTitle>
          <AlertDescription>
            使用当前生效规则，从分配时刻开始计时；已有考核不会重复创建。
          </AlertDescription>
          {canAssignAssessment && !isSelf ? (
            <Button
              size="sm"
              variant="outline"
              onClick={() => setAssignmentOpen(true)}
            >
              分配新人考核
            </Button>
          ) : null}
        </Alert>
      ) : (
        <Alert>
          <ShieldAlertIcon />
          <AlertTitle>待激活账户不能处置</AlertTitle>
          <AlertDescription>
            请先完成账户激活，再分配新人考核。
          </AlertDescription>
        </Alert>
      )}

      <AccountActionDialog
        kind="reactivate"
        open={reactivationOpen}
        onOpenChange={setReactivationOpen}
        detail={detail}
        csrfToken={csrfToken}
      />
      <AccountActionDialog
        kind="assessment"
        open={assignmentOpen}
        onOpenChange={setAssignmentOpen}
        detail={detail}
        csrfToken={csrfToken}
      />
    </section>
  )
}

function AccountActionDialog({
  kind,
  open,
  onOpenChange,
  detail,
  csrfToken,
}: {
  kind: "reactivate" | "assessment"
  open: boolean
  onOpenChange: (open: boolean) => void
  detail: ManagedUserDetail
  csrfToken: string
}) {
  const [reason, setReason] = React.useState("")
  const [idempotencyKey, setIdempotencyKey] = React.useState(() =>
    crypto.randomUUID()
  )
  const reactivate = useReactivateManagedUser()
  const assign = useAssignNewcomerAssessment()
  const mutation = kind === "reactivate" ? reactivate : assign
  const reasonLength = Array.from(reason.trim()).length

  React.useEffect(() => {
    if (!open) return
    setReason("")
    setIdempotencyKey(crypto.randomUUID())
    reactivate.reset()
    assign.reset()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, kind])

  async function handleSubmit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (reasonLength > 500) return
    try {
      if (kind === "reactivate") {
        await reactivate.mutateAsync({
          userId: detail.id,
          csrfToken,
          idempotencyKey,
          body: {
            expected_user_version: detail.version,
            reason: reason.trim() || undefined,
          },
        })
      } else {
        await assign.mutateAsync({
          userId: detail.id,
          csrfToken,
          idempotencyKey,
          reason: reason.trim(),
        })
      }
      onOpenChange(false)
    } catch {
      // Preserve the reviewed input and idempotency key for a safe retry.
    }
  }

  const pending = mutation.isPending
  const error = mutation.isError ? mutation.error : undefined
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <form onSubmit={handleSubmit}>
          <DialogHeader>
            <DialogTitle>
              {kind === "reactivate" ? "确认解封账户" : "分配新人考核"}
            </DialogTitle>
            <DialogDescription>
              {detail.display_name}（ID {detail.numeric_id}）
            </DialogDescription>
          </DialogHeader>
          <div className="py-5">
            <Field
              data-invalid={reasonLength > 500 || Boolean(error) || undefined}
            >
              <FieldLabel htmlFor={`${kind}-reason`}>变更理由</FieldLabel>
              <Textarea
                id={`${kind}-reason`}
                value={reason}
                maxLength={500}
                placeholder="可留空；系统会自动生成对应的管理理由"
                onChange={(event) => setReason(event.target.value)}
              />
              <FieldDescription>{reasonLength}/500 个字符</FieldDescription>
              {error ? (
                <FieldError>
                  {requestErrorDescription(
                    error,
                    "操作失败，请刷新账户详情后重试。"
                  )}
                </FieldError>
              ) : null}
            </Field>
          </div>
          <DialogFooter>
            <DialogClose render={<Button variant="outline" />}>
              取消
            </DialogClose>
            <Button type="submit" disabled={pending || reasonLength > 500}>
              {pending ? <Spinner data-icon="inline-start" /> : null}
              {kind === "reactivate" ? "确认解封" : "确认分配"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

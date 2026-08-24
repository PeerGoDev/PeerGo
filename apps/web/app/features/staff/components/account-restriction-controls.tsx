import * as React from "react"
import {
  CircleAlertIcon,
  ClipboardCheckIcon,
  LockKeyholeIcon,
  ShieldAlertIcon,
  ShieldCheckIcon,
} from "lucide-react"

import {
  Alert,
  AlertAction,
  AlertDescription,
  AlertTitle,
} from "~/components/ui/alert"
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogMedia,
  AlertDialogTitle,
} from "~/components/ui/alert-dialog"
import { Badge } from "~/components/ui/badge"
import { Button } from "~/components/ui/button"
import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from "~/components/ui/card"
import {
  Field,
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLabel,
} from "~/components/ui/field"
import { Spinner } from "~/components/ui/spinner"
import { Textarea } from "~/components/ui/textarea"
import { ToggleGroup, ToggleGroupItem } from "~/components/ui/toggle-group"
import {
  type ManagedUserDetail,
  useCreateAccountRestriction,
  useRevokeAccountRestriction,
} from "~/features/staff/api/user-administration.queries"
import {
  type AccountRestrictionReasonCode,
  type AccountRestrictionRevocationReasonCode,
  type CreateAccountRestrictionFormField,
  type CreateAccountRestrictionFormValues,
  type RevokeAccountRestrictionFormField,
  type RevokeAccountRestrictionFormValues,
  accountRestrictionDurationLabel,
  accountRestrictionReasonLabel,
  accountRestrictionRevocationReasonLabel,
  createAccountRestrictionFormSchema,
  revokeAccountRestrictionFormSchema,
} from "~/features/staff/model/account-restriction-form"
import { ApiProblemError } from "~/shared/api/problem"
import { formatDateTime } from "~/shared/formatters/date-time"

type CurrentRestriction = ManagedUserDetail["active_restrictions"][number]

export function AccountRestrictionControls({
  detail,
  csrfToken,
  currentStaffUserId,
  canRestrict,
  canRevoke,
  onRefresh,
}: {
  detail: ManagedUserDetail
  csrfToken: string
  currentStaffUserId: string
  canRestrict: boolean
  canRevoke: boolean
  onRefresh: () => void
}) {
  const currentRestriction = detail.active_restrictions[0]
  const isSelf = detail.id === currentStaffUserId

  return (
    <section className="flex flex-col gap-3">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h2 className="font-heading font-medium">受控账户操作</h2>
          <p className="text-xs text-muted-foreground">
            只处理临时访问限制；账户状态、凭据和资料不在这里修改。
          </p>
        </div>
        <Badge variant="destructive">高风险</Badge>
      </div>

      {isSelf ? (
        <Alert>
          <LockKeyholeIcon />
          <AlertTitle>不能处置自己的账户</AlertTitle>
          <AlertDescription>
            创建或解除账户限制必须由另一名具备对应职责的员工执行。
          </AlertDescription>
        </Alert>
      ) : currentRestriction ? (
        <>
          {detail.active_restrictions.length > 1 ? (
            <Alert variant="destructive">
              <CircleAlertIcon />
              <AlertTitle>发现多条同时生效的历史记录</AlertTitle>
              <AlertDescription>
                为避免冲突，每次只解除一条，并在提交后重新读取账户状态。
              </AlertDescription>
            </Alert>
          ) : null}
          {canRevoke ? (
            <RevokeRestrictionForm
              detail={detail}
              restriction={currentRestriction}
              csrfToken={csrfToken}
              onRefresh={onRefresh}
            />
          ) : (
            <ReadOnlyActionNotice action="user.account.restriction.revoke" />
          )}
        </>
      ) : detail.status !== "active" ? (
        <Alert>
          <LockKeyholeIcon />
          <AlertTitle>当前账户状态不接受新限制</AlertTitle>
          <AlertDescription>
            只有状态正常的账户可以新增临时访问限制；其他状态请在对应的账户流程中处理。
          </AlertDescription>
        </Alert>
      ) : canRestrict ? (
        <CreateRestrictionForm
          detail={detail}
          csrfToken={csrfToken}
          onRefresh={onRefresh}
        />
      ) : (
        <ReadOnlyActionNotice action="user.account.restrict" />
      )}
    </section>
  )
}

type CreateErrors = Partial<Record<CreateAccountRestrictionFormField, string>>

function CreateRestrictionForm({
  detail,
  csrfToken,
  onRefresh,
}: {
  detail: ManagedUserDetail
  csrfToken: string
  onRefresh: () => void
}) {
  const mutation = useCreateAccountRestriction()
  const [reasonCode, setReasonCode] =
    React.useState<AccountRestrictionReasonCode>("manual_review")
  const [durationHours, setDurationHours] = React.useState<"24" | "72" | "168">(
    "24"
  )
  const [reason, setReason] = React.useState("")
  const [errors, setErrors] = React.useState<CreateErrors>({})
  const [review, setReview] = React.useState<{
    values: CreateAccountRestrictionFormValues
    estimatedExpiresAt: string
  }>()
  const [confirmationOpen, setConfirmationOpen] = React.useState(false)

  function handleReview(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const formElement = event.currentTarget
    mutation.reset()
    const result = createAccountRestrictionFormSchema.safeParse({
      reasonCode,
      durationHours,
      reason,
    })
    if (!result.success) {
      const nextErrors = createFieldErrors(result.error.issues)
      setErrors(nextErrors)
      requestAnimationFrame(() => {
        formElement.querySelector<HTMLElement>("[aria-invalid='true']")?.focus()
      })
      return
    }

    setErrors({})
    setReview({
      values: result.data,
      estimatedExpiresAt: new Date(
        Date.now() + result.data.durationHours * 60 * 60 * 1000
      ).toISOString(),
    })
    setConfirmationOpen(true)
  }

  async function handleConfirm() {
    if (!review) {
      return
    }
    try {
      await mutation.mutateAsync({
        userId: detail.id,
        csrfToken,
        body: {
          reason_code: review.values.reasonCode,
          reason: review.values.reason,
          duration_hours: review.values.durationHours,
          expected_user_version: detail.version,
        },
      })
      setConfirmationOpen(false)
      setReason("")
      setReview(undefined)
    } catch {
      setConfirmationOpen(false)
      // The typed API problem is rendered beside the form for a safe retry.
    }
  }

  return (
    <>
      <form onSubmit={handleReview} noValidate>
        <Card size="sm">
          <CardHeader>
            <CardTitle>临时限制账户访问</CardTitle>
            <CardDescription>
              最长 7 天，不能与当前有效限制重叠。
            </CardDescription>
          </CardHeader>
          <CardContent className="flex flex-col gap-4">
            {mutation.isError ? (
              <RestrictionMutationError
                error={mutation.error}
                onRefresh={() => {
                  mutation.reset()
                  onRefresh()
                }}
              />
            ) : null}

            <Alert>
              <ShieldAlertIcon />
              <AlertTitle>提交后立即撤销现有会话</AlertTitle>
              <AlertDescription>
                Web 与后台会话都会失效；限制到期或解除后，旧会话也不会恢复。
              </AlertDescription>
            </Alert>

            <FieldGroup>
              <Field data-invalid={Boolean(errors.reasonCode)}>
                <FieldLabel id="restriction-reason-code-label">
                  处置类别
                </FieldLabel>
                <ToggleGroup
                  value={[reasonCode]}
                  onValueChange={(values) => {
                    const selected = values[0] as
                      | AccountRestrictionReasonCode
                      | undefined
                    if (selected) {
                      setReasonCode(selected)
                    }
                  }}
                  variant="outline"
                  spacing={0}
                  aria-labelledby="restriction-reason-code-label"
                  aria-invalid={Boolean(errors.reasonCode)}
                  className="w-full flex-wrap"
                  disabled={mutation.isPending}
                >
                  <ToggleGroupItem value="manual_review" className="flex-1">
                    人工复核
                  </ToggleGroupItem>
                  <ToggleGroupItem value="security_incident" className="flex-1">
                    安全事件处置
                  </ToggleGroupItem>
                </ToggleGroup>
                <FieldError
                  errors={
                    errors.reasonCode ? [{ message: errors.reasonCode }] : []
                  }
                />
              </Field>

              <Field data-invalid={Boolean(errors.durationHours)}>
                <FieldLabel id="restriction-duration-label">
                  限制时长
                </FieldLabel>
                <ToggleGroup
                  value={[durationHours]}
                  onValueChange={(values) => {
                    const selected = values[0] as
                      | "24"
                      | "72"
                      | "168"
                      | undefined
                    if (selected) {
                      setDurationHours(selected)
                    }
                  }}
                  variant="outline"
                  spacing={0}
                  aria-labelledby="restriction-duration-label"
                  aria-invalid={Boolean(errors.durationHours)}
                  className="w-full"
                  disabled={mutation.isPending}
                >
                  <ToggleGroupItem value="24" className="flex-1">
                    24 小时
                  </ToggleGroupItem>
                  <ToggleGroupItem value="72" className="flex-1">
                    3 天
                  </ToggleGroupItem>
                  <ToggleGroupItem value="168" className="flex-1">
                    7 天
                  </ToggleGroupItem>
                </ToggleGroup>
                <FieldDescription>
                  到期只终止限制，不重建已经失效的会话。
                </FieldDescription>
                <FieldError
                  errors={
                    errors.durationHours
                      ? [{ message: errors.durationHours }]
                      : []
                  }
                />
              </Field>

              <Field data-invalid={Boolean(errors.reason)}>
                <FieldLabel htmlFor="restriction-reason">人工理由</FieldLabel>
                <Textarea
                  id="restriction-reason"
                  value={reason}
                  onChange={(event) => setReason(event.target.value)}
                  rows={4}
                  maxLength={500}
                  placeholder="可留空；系统会自动记录人工理由"
                  disabled={mutation.isPending}
                  aria-invalid={Boolean(errors.reason)}
                />
                <FieldDescription>
                  完整理由会安全保存，审计记录仅保留必要摘要。
                </FieldDescription>
                <FieldError
                  errors={errors.reason ? [{ message: errors.reason }] : []}
                />
              </Field>
            </FieldGroup>
          </CardContent>
          <CardFooter className="justify-end border-t bg-muted/30">
            <Button type="submit" variant="destructive">
              <ClipboardCheckIcon data-icon="inline-start" />
              审阅限制
            </Button>
          </CardFooter>
        </Card>
      </form>

      <CreateRestrictionConfirmation
        open={confirmationOpen}
        review={review}
        pending={mutation.isPending}
        onOpenChange={setConfirmationOpen}
        onConfirm={() => void handleConfirm()}
      />
    </>
  )
}

function CreateRestrictionConfirmation({
  open,
  review,
  pending,
  onOpenChange,
  onConfirm,
}: {
  open: boolean
  review?: {
    values: CreateAccountRestrictionFormValues
    estimatedExpiresAt: string
  }
  pending: boolean
  onOpenChange: (open: boolean) => void
  onConfirm: () => void
}) {
  if (!review) {
    return null
  }

  return (
    <AlertDialog
      open={open}
      onOpenChange={(nextOpen) => {
        if (!nextOpen && pending) {
          return
        }
        onOpenChange(nextOpen)
      }}
    >
      <AlertDialogContent className="sm:max-w-lg">
        <AlertDialogHeader>
          <AlertDialogMedia>
            <ShieldAlertIcon />
          </AlertDialogMedia>
          <AlertDialogTitle>确认临时限制账户访问</AlertDialogTitle>
          <AlertDialogDescription>
            保存前会重新确认账户状态；若已被其他管理员修改，本次操作会停止。
          </AlertDialogDescription>
        </AlertDialogHeader>

        <div className="grid gap-2 rounded-lg border bg-muted/30 p-3 text-sm">
          <ConfirmationFact
            label="处置类别"
            value={accountRestrictionReasonLabel(review.values.reasonCode)}
          />
          <ConfirmationFact
            label="限制时长"
            value={accountRestrictionDurationLabel(review.values.durationHours)}
          />
          <ConfirmationFact
            label="预计到期"
            value={formatDateTime(review.estimatedExpiresAt)}
          />
          <ConfirmationFact label="人工理由" value={review.values.reason} />
        </div>

        <Alert variant="destructive">
          <ShieldAlertIcon />
          <AlertTitle>影响：立即阻断账户访问</AlertTitle>
          <AlertDescription>
            成功后会撤销目标的全部现有会话，并保留完整操作记录。
          </AlertDescription>
        </Alert>

        <AlertDialogFooter>
          <AlertDialogCancel disabled={pending}>返回修改</AlertDialogCancel>
          <AlertDialogAction
            variant="destructive"
            disabled={pending}
            onClick={onConfirm}
          >
            {pending ? <Spinner data-icon="inline-start" /> : null}
            {pending ? "正在提交…" : "确认并立即限制"}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}

type RevokeErrors = Partial<Record<RevokeAccountRestrictionFormField, string>>

function RevokeRestrictionForm({
  detail,
  restriction,
  csrfToken,
  onRefresh,
}: {
  detail: ManagedUserDetail
  restriction: CurrentRestriction
  csrfToken: string
  onRefresh: () => void
}) {
  const mutation = useRevokeAccountRestriction()
  const [reasonCode, setReasonCode] =
    React.useState<AccountRestrictionRevocationReasonCode>("review_completed")
  const [reason, setReason] = React.useState("")
  const [errors, setErrors] = React.useState<RevokeErrors>({})
  const [review, setReview] =
    React.useState<RevokeAccountRestrictionFormValues>()
  const [confirmationOpen, setConfirmationOpen] = React.useState(false)

  function handleReview(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const formElement = event.currentTarget
    mutation.reset()
    const result = revokeAccountRestrictionFormSchema.safeParse({
      reasonCode,
      reason,
    })
    if (!result.success) {
      const nextErrors = revokeFieldErrors(result.error.issues)
      setErrors(nextErrors)
      requestAnimationFrame(() => {
        formElement.querySelector<HTMLElement>("[aria-invalid='true']")?.focus()
      })
      return
    }

    setErrors({})
    setReview(result.data)
    setConfirmationOpen(true)
  }

  async function handleConfirm() {
    if (!review) {
      return
    }
    try {
      await mutation.mutateAsync({
        userId: detail.id,
        restrictionId: restriction.id,
        csrfToken,
        body: {
          reason_code: review.reasonCode,
          reason: review.reason,
          expected_user_version: detail.version,
          expected_restriction_version: restriction.version,
        },
      })
      setConfirmationOpen(false)
      setReason("")
      setReview(undefined)
    } catch {
      setConfirmationOpen(false)
      // Keep the typed API problem visible and require a fresh review.
    }
  }

  return (
    <>
      <form onSubmit={handleReview} noValidate>
        <Card size="sm">
          <CardHeader>
            <CardTitle>解除当前访问限制</CardTitle>
            <CardDescription>
              当前限制到期于 {formatDateTime(restriction.expires_at)}
            </CardDescription>
          </CardHeader>
          <CardContent className="flex flex-col gap-4">
            {mutation.isError ? (
              <RestrictionMutationError
                error={mutation.error}
                onRefresh={() => {
                  mutation.reset()
                  onRefresh()
                }}
              />
            ) : null}

            <Alert>
              <ShieldCheckIcon />
              <AlertTitle>解除后允许建立新会话</AlertTitle>
              <AlertDescription>
                这不会恢复被撤销的旧会话，也不会改变账户的停用状态。
              </AlertDescription>
            </Alert>

            <FieldGroup>
              <Field data-invalid={Boolean(errors.reasonCode)}>
                <FieldLabel id="restriction-revocation-code-label">
                  解除类别
                </FieldLabel>
                <ToggleGroup
                  value={[reasonCode]}
                  onValueChange={(values) => {
                    const selected = values[0] as
                      | AccountRestrictionRevocationReasonCode
                      | undefined
                    if (selected) {
                      setReasonCode(selected)
                    }
                  }}
                  variant="outline"
                  spacing={0}
                  aria-labelledby="restriction-revocation-code-label"
                  aria-invalid={Boolean(errors.reasonCode)}
                  className="w-full flex-wrap"
                  disabled={mutation.isPending}
                >
                  <ToggleGroupItem value="review_completed" className="flex-1">
                    复核已完成
                  </ToggleGroupItem>
                  <ToggleGroupItem
                    value="restriction_no_longer_needed"
                    className="flex-1"
                  >
                    限制已无必要
                  </ToggleGroupItem>
                </ToggleGroup>
                <FieldError
                  errors={
                    errors.reasonCode ? [{ message: errors.reasonCode }] : []
                  }
                />
              </Field>

              <Field data-invalid={Boolean(errors.reason)}>
                <FieldLabel htmlFor="restriction-revocation-reason">
                  人工理由
                </FieldLabel>
                <Textarea
                  id="restriction-revocation-reason"
                  value={reason}
                  onChange={(event) => setReason(event.target.value)}
                  rows={4}
                  maxLength={500}
                  placeholder="可留空；系统会自动记录解除理由"
                  disabled={mutation.isPending}
                  aria-invalid={Boolean(errors.reason)}
                />
                <FieldDescription>
                  保存前会同时复核账户和当前限制，避免覆盖其他管理员的处置。
                </FieldDescription>
                <FieldError
                  errors={errors.reason ? [{ message: errors.reason }] : []}
                />
              </Field>
            </FieldGroup>
          </CardContent>
          <CardFooter className="justify-end border-t bg-muted/30">
            <Button type="submit">
              <ClipboardCheckIcon data-icon="inline-start" />
              审阅解除
            </Button>
          </CardFooter>
        </Card>
      </form>

      <RevokeRestrictionConfirmation
        open={confirmationOpen}
        values={review}
        restriction={restriction}
        pending={mutation.isPending}
        onOpenChange={setConfirmationOpen}
        onConfirm={() => void handleConfirm()}
      />
    </>
  )
}

function RevokeRestrictionConfirmation({
  open,
  values,
  restriction,
  pending,
  onOpenChange,
  onConfirm,
}: {
  open: boolean
  values?: RevokeAccountRestrictionFormValues
  restriction: CurrentRestriction
  pending: boolean
  onOpenChange: (open: boolean) => void
  onConfirm: () => void
}) {
  if (!values) {
    return null
  }

  return (
    <AlertDialog
      open={open}
      onOpenChange={(nextOpen) => {
        if (!nextOpen && pending) {
          return
        }
        onOpenChange(nextOpen)
      }}
    >
      <AlertDialogContent className="sm:max-w-lg">
        <AlertDialogHeader>
          <AlertDialogMedia>
            <ShieldCheckIcon />
          </AlertDialogMedia>
          <AlertDialogTitle>确认解除账户访问限制</AlertDialogTitle>
          <AlertDialogDescription>
            保存前会再次确认账户和限制状态，并保留完整的解除记录。
          </AlertDialogDescription>
        </AlertDialogHeader>

        <div className="grid gap-2 rounded-lg border bg-muted/30 p-3 text-sm">
          <ConfirmationFact
            label="解除类别"
            value={accountRestrictionRevocationReasonLabel(values.reasonCode)}
          />
          <ConfirmationFact label="人工理由" value={values.reason} />
          <ConfirmationFact label="限制编号" value={restriction.id} mono />
        </div>

        <Alert>
          <ShieldCheckIcon />
          <AlertTitle>影响：允许重新建立会话</AlertTitle>
          <AlertDescription>
            已撤销的旧会话保持失效，目标需要重新完成登录。
          </AlertDescription>
        </Alert>

        <AlertDialogFooter>
          <AlertDialogCancel disabled={pending}>返回修改</AlertDialogCancel>
          <AlertDialogAction disabled={pending} onClick={onConfirm}>
            {pending ? <Spinner data-icon="inline-start" /> : null}
            {pending ? "正在提交…" : "确认解除限制"}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}

function ReadOnlyActionNotice({ action: _action }: { action: string }) {
  return (
    <Alert>
      <LockKeyholeIcon />
      <AlertTitle>当前权限只可查看</AlertTitle>
      <AlertDescription>
        当前后台权限不能执行这项操作，因此不会显示操作表单。
      </AlertDescription>
    </Alert>
  )
}

function RestrictionMutationError({
  error,
  onRefresh,
}: {
  error: Error
  onRefresh: () => void
}) {
  return (
    <Alert variant="destructive">
      <CircleAlertIcon />
      <AlertTitle>
        {error instanceof ApiProblemError ? error.message : "账户操作未能完成"}
      </AlertTitle>
      <AlertDescription>{restrictionErrorDescription(error)}</AlertDescription>
      <AlertAction>
        <Button type="button" variant="outline" size="sm" onClick={onRefresh}>
          刷新详情
        </Button>
      </AlertAction>
    </Alert>
  )
}

function restrictionErrorDescription(error: Error) {
  if (!(error instanceof ApiProblemError)) {
    return "请检查网络与后台会话状态，刷新详情后重新审阅。"
  }
  switch (error.code) {
    case "managed_user_version_conflict":
    case "account_restriction_version_conflict":
      return "账户或限制已被其他操作更新，请刷新详情并基于最新版本重新审阅。"
    case "account_restriction_already_active":
      return "账户已经有有效限制，请刷新详情查看当前记录。"
    case "account_restriction_not_active":
      return "目标限制已经解除或到期，请刷新详情。"
    case "managed_user_not_active":
      return "账户已不再是 active 状态，不能新增临时限制。"
    case "account_restriction_self_target":
      return "不能处置自己的账户，请由另一名具备职责的员工操作。"
    case "staff_csrf_invalid":
    case "staff_session_expired":
    case "staff_session_required":
      return "后台会话已失效或请求校验失败，请重新完成员工身份验证。"
    default:
      return "服务端拒绝了这次操作，请刷新详情并核对职责、版本和输入。"
  }
}

function ConfirmationFact({
  label,
  value,
  mono = false,
}: {
  label: string
  value: string
  mono?: boolean
}) {
  return (
    <div className="grid grid-cols-[5rem_1fr] gap-2">
      <span className="text-muted-foreground">{label}</span>
      <span className={mono ? "font-mono text-xs break-all" : "break-words"}>
        {value}
      </span>
    </div>
  )
}

function createFieldErrors(
  issues: Array<{ path: PropertyKey[]; message: string }>
) {
  const errors: CreateErrors = {}
  for (const issue of issues) {
    const field = issue.path[0]
    if (typeof field === "string" && !errors[field as keyof CreateErrors]) {
      errors[field as keyof CreateErrors] = issue.message
    }
  }
  return errors
}

function revokeFieldErrors(
  issues: Array<{ path: PropertyKey[]; message: string }>
) {
  const errors: RevokeErrors = {}
  for (const issue of issues) {
    const field = issue.path[0]
    if (typeof field === "string" && !errors[field as keyof RevokeErrors]) {
      errors[field as keyof RevokeErrors] = issue.message
    }
  }
  return errors
}

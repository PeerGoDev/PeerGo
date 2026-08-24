import * as React from "react"
import {
  HistoryIcon,
  PencilLineIcon,
  ShieldAlertIcon,
  ShieldCheckIcon,
} from "lucide-react"

import { Alert, AlertDescription, AlertTitle } from "~/components/ui/alert"
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
import { Spinner } from "~/components/ui/spinner"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "~/components/ui/table"
import { Textarea } from "~/components/ui/textarea"
import { ToggleGroup, ToggleGroupItem } from "~/components/ui/toggle-group"
import {
  type ManagedUserDetail,
  useCreateManualDownloadRestriction,
  useRevokeManualDownloadRestriction,
  useUpdateManualDownloadRestriction,
} from "~/features/staff/api/user-administration.queries"
import {
  type ManualDownloadRestrictionReasonCode,
  type ManualDownloadRestrictionRevocationReasonCode,
  manualDownloadRestrictionFormSchema,
  manualDownloadRestrictionReasonLabel,
  manualDownloadRestrictionTransitionLabel,
  revokeManualDownloadRestrictionFormSchema,
} from "~/features/staff/model/manual-download-restriction-form"
import { requestErrorDescription } from "~/shared/api/problem"
import { formatDateTime } from "~/shared/formatters/date-time"

export function ManualDownloadRestrictionControls({
  detail,
  csrfToken,
  currentStaffUserId,
  canRestrict,
  canRevoke,
}: {
  detail: ManagedUserDetail
  csrfToken: string
  currentStaffUserId: string
  canRestrict: boolean
  canRevoke: boolean
}) {
  const [editorMode, setEditorMode] = React.useState<"create" | "update">()
  const [revokeOpen, setRevokeOpen] = React.useState(false)
  const state = detail.manual_download_restriction
  const isSelf = detail.id === currentStaffUserId

  return (
    <section className="flex flex-col gap-3">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h2 className="font-heading font-medium">人工下载限制</h2>
          <p className="text-xs text-muted-foreground">
            只管理旧站／人工来源；分享率与 H&amp;R 保持各自独立。
          </p>
        </div>
        <Badge variant={state.active ? "destructive" : "secondary"}>
          {state.active ? "已限制" : "未限制"}
        </Badge>
      </div>

      {state.active ? (
        <Alert variant="destructive">
          <ShieldAlertIcon />
          <AlertTitle>
            {manualDownloadRestrictionReasonLabel(
              state.reason_code ?? "manual_review"
            )}
          </AlertTitle>
          <AlertDescription className="flex flex-col gap-1">
            <span>{state.reason_summary}</span>
            <span className="text-xs">
              {state.started_at
                ? `自 ${formatDateTime(state.started_at)} 起生效`
                : "当前正在生效"}
              {` · 状态版本 ${state.version}`}
            </span>
          </AlertDescription>
        </Alert>
      ) : detail.download_restricted ? (
        <Alert>
          <ShieldCheckIcon />
          <AlertTitle>人工来源未限制</AlertTitle>
          <AlertDescription>
            综合下载状态仍受限，说明长期分享率或 H&amp;R
            来源仍在生效；这里不能解除它们。
          </AlertDescription>
        </Alert>
      ) : (
        <Alert>
          <ShieldCheckIcon />
          <AlertTitle>人工来源正常</AlertTitle>
          <AlertDescription>
            当前没有旧站或管理员签发的下载限制。
          </AlertDescription>
        </Alert>
      )}

      {isSelf ? (
        <p className="text-xs text-muted-foreground">
          不能处置自己的下载权限，请由另一名管理员执行。
        </p>
      ) : (
        <div className="flex flex-wrap gap-2">
          {!state.active && canRestrict ? (
            <Button
              size="sm"
              variant="destructive"
              onClick={() => setEditorMode("create")}
            >
              <ShieldAlertIcon data-icon="inline-start" />
              签发限制
            </Button>
          ) : null}
          {state.active && canRestrict ? (
            <Button
              size="sm"
              variant="outline"
              onClick={() => setEditorMode("update")}
            >
              <PencilLineIcon data-icon="inline-start" />
              修改理由
            </Button>
          ) : null}
          {state.active && canRevoke ? (
            <Button
              size="sm"
              variant="outline"
              onClick={() => setRevokeOpen(true)}
            >
              <ShieldCheckIcon data-icon="inline-start" />
              解除限制
            </Button>
          ) : null}
          {!canRestrict && !canRevoke ? (
            <span className="text-xs text-muted-foreground">
              当前权限仅可查看。
            </span>
          ) : null}
        </div>
      )}

      <RestrictionHistory detail={detail} />

      <RestrictionEditorDialog
        open={Boolean(editorMode)}
        mode={editorMode ?? "create"}
        detail={detail}
        csrfToken={csrfToken}
        onOpenChange={(open) => {
          if (!open) setEditorMode(undefined)
        }}
      />
      <RestrictionRevokeDialog
        open={revokeOpen}
        detail={detail}
        csrfToken={csrfToken}
        onOpenChange={setRevokeOpen}
      />
    </section>
  )
}

function RestrictionHistory({ detail }: { detail: ManagedUserDetail }) {
  const history = detail.manual_download_restriction_history
  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-center gap-2 text-sm font-medium">
        <HistoryIcon aria-hidden="true" />
        操作历史
        <Badge variant="outline" className="tabular-nums">
          {history.length}
        </Badge>
      </div>
      {history.length === 0 ? (
        <p className="text-xs text-muted-foreground">暂无人工下载限制记录。</p>
      ) : (
        <Table className="text-xs">
          <TableHeader>
            <TableRow>
              <TableHead>时间／操作</TableHead>
              <TableHead>理由</TableHead>
              <TableHead>执行人</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {history.map((item) => (
              <TableRow key={`${item.state_version}-${item.occurred_at}`}>
                <TableCell className="align-top">
                  <div className="flex flex-col gap-1">
                    <Badge
                      variant={
                        item.transition === "revoked" ? "secondary" : "outline"
                      }
                    >
                      {manualDownloadRestrictionTransitionLabel(
                        item.transition
                      )}
                    </Badge>
                    <time
                      dateTime={item.occurred_at}
                      className="text-muted-foreground"
                    >
                      {formatDateTime(item.occurred_at)}
                    </time>
                  </div>
                </TableCell>
                <TableCell className="max-w-52 whitespace-normal">
                  <div className="flex flex-col gap-1">
                    <span>
                      {manualDownloadRestrictionReasonLabel(item.reason_code)}
                    </span>
                    <span className="text-muted-foreground">
                      {item.reason_summary}
                    </span>
                  </div>
                </TableCell>
                <TableCell className="align-top">
                  {item.actor_username
                    ? `${item.actor_username} (#${item.actor_numeric_id})`
                    : "系统迁入"}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
    </div>
  )
}

function RestrictionEditorDialog({
  open,
  mode,
  detail,
  csrfToken,
  onOpenChange,
}: {
  open: boolean
  mode: "create" | "update"
  detail: ManagedUserDetail
  csrfToken: string
  onOpenChange: (open: boolean) => void
}) {
  const createMutation = useCreateManualDownloadRestriction()
  const updateMutation = useUpdateManualDownloadRestriction()
  const mutation = mode === "create" ? createMutation : updateMutation
  const currentReasonCode = detail.manual_download_restriction.reason_code
  const [reasonCode, setReasonCode] =
    React.useState<ManualDownloadRestrictionReasonCode>("manual_review")
  const [reason, setReason] = React.useState("")
  const [validationError, setValidationError] = React.useState<string>()

  React.useEffect(() => {
    if (!open) return
    setReasonCode(
      currentReasonCode === "policy_violation" ||
        currentReasonCode === "abuse_prevention"
        ? currentReasonCode
        : "manual_review"
    )
    setReason(
      mode === "update"
        ? (detail.manual_download_restriction.reason_summary ?? "")
        : ""
    )
    setValidationError(undefined)
    createMutation.reset()
    updateMutation.reset()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, mode])

  async function handleSubmit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const parsed = manualDownloadRestrictionFormSchema.safeParse({
      reasonCode,
      reason,
    })
    if (!parsed.success) {
      setValidationError(parsed.error.issues[0]?.message)
      return
    }
    setValidationError(undefined)
    try {
      await mutation.mutateAsync({
        userId: detail.id,
        csrfToken,
        body: {
          reason_code: parsed.data.reasonCode,
          reason: parsed.data.reason,
          expected_user_version: detail.version,
          expected_state_version: detail.manual_download_restriction.version,
        },
      })
      onOpenChange(false)
    } catch {
      // Keep the reviewed values visible for a safe retry.
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <form onSubmit={handleSubmit}>
          <DialogHeader>
            <DialogTitle>
              {mode === "create" ? "签发人工下载限制" : "修改人工下载限制"}
            </DialogTitle>
            <DialogDescription>
              只影响新种子下载；登录、做种上传、长期分享率和 H&amp;R
              状态不会被修改。
            </DialogDescription>
          </DialogHeader>
          <FieldGroup className="py-5">
            <Field>
              <FieldLabel id="manual-download-restriction-reason-code">
                处置类别
              </FieldLabel>
              <ToggleGroup
                value={[reasonCode]}
                onValueChange={(values) => {
                  const value = values[0] as
                    | ManualDownloadRestrictionReasonCode
                    | undefined
                  if (value) setReasonCode(value)
                }}
                variant="outline"
                spacing={0}
                className="w-full flex-wrap"
                aria-labelledby="manual-download-restriction-reason-code"
              >
                <ToggleGroupItem value="manual_review" className="flex-1">
                  人工复核
                </ToggleGroupItem>
                <ToggleGroupItem value="policy_violation" className="flex-1">
                  规则处置
                </ToggleGroupItem>
                <ToggleGroupItem value="abuse_prevention" className="flex-1">
                  风险控制
                </ToggleGroupItem>
              </ToggleGroup>
            </Field>
            <Field data-invalid={Boolean(validationError || mutation.isError)}>
              <FieldLabel htmlFor="manual-download-restriction-reason">
                人工理由
              </FieldLabel>
              <Textarea
                id="manual-download-restriction-reason"
                value={reason}
                rows={5}
                maxLength={500}
                onChange={(event) => {
                  setReason(event.target.value)
                  setValidationError(undefined)
                  mutation.reset()
                }}
                aria-invalid={Boolean(validationError || mutation.isError)}
                placeholder="可留空；系统会自动记录人工理由"
              />
              <FieldDescription>
                保存后会追加历史记录，不覆盖已有处置证据。
              </FieldDescription>
              {validationError ? (
                <FieldError>{validationError}</FieldError>
              ) : null}
              {mutation.isError ? (
                <FieldError>
                  {requestErrorDescription(
                    mutation.error,
                    "操作失败，请刷新用户详情后重试。"
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
              variant={mode === "create" ? "destructive" : "default"}
              disabled={mutation.isPending}
            >
              {mutation.isPending ? <Spinner data-icon="inline-start" /> : null}
              {mutation.isPending
                ? "正在保存…"
                : mode === "create"
                  ? "确认签发"
                  : "保存修改"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

function RestrictionRevokeDialog({
  open,
  detail,
  csrfToken,
  onOpenChange,
}: {
  open: boolean
  detail: ManagedUserDetail
  csrfToken: string
  onOpenChange: (open: boolean) => void
}) {
  const mutation = useRevokeManualDownloadRestriction()
  const [reasonCode, setReasonCode] =
    React.useState<ManualDownloadRestrictionRevocationReasonCode>(
      "review_completed"
    )
  const [reason, setReason] = React.useState("")
  const [validationError, setValidationError] = React.useState<string>()

  React.useEffect(() => {
    if (!open) return
    setReasonCode("review_completed")
    setReason("")
    setValidationError(undefined)
    mutation.reset()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open])

  async function handleRevoke() {
    const parsed = revokeManualDownloadRestrictionFormSchema.safeParse({
      reasonCode,
      reason,
    })
    if (!parsed.success) {
      setValidationError(parsed.error.issues[0]?.message)
      return
    }
    try {
      await mutation.mutateAsync({
        userId: detail.id,
        csrfToken,
        body: {
          reason_code: parsed.data.reasonCode,
          reason: parsed.data.reason,
          expected_user_version: detail.version,
          expected_state_version: detail.manual_download_restriction.version,
        },
      })
      onOpenChange(false)
    } catch {
      // Keep the dialog open with the typed problem visible.
    }
  }

  return (
    <AlertDialog open={open} onOpenChange={onOpenChange}>
      <AlertDialogContent className="sm:max-w-lg">
        <AlertDialogHeader>
          <AlertDialogMedia>
            <ShieldCheckIcon />
          </AlertDialogMedia>
          <AlertDialogTitle>确认解除人工下载限制</AlertDialogTitle>
          <AlertDialogDescription>
            只解除旧站／人工来源。如果分享率或 H&amp;R
            仍不达标，用户依然不能发起新下载。
          </AlertDialogDescription>
        </AlertDialogHeader>
        <FieldGroup>
          <Field>
            <FieldLabel id="manual-download-restriction-revoke-code">
              解除类别
            </FieldLabel>
            <ToggleGroup
              value={[reasonCode]}
              onValueChange={(values) => {
                const value = values[0] as
                  | ManualDownloadRestrictionRevocationReasonCode
                  | undefined
                if (value) setReasonCode(value)
              }}
              variant="outline"
              spacing={0}
              className="w-full"
              aria-labelledby="manual-download-restriction-revoke-code"
            >
              <ToggleGroupItem value="review_completed" className="flex-1">
                复核已完成
              </ToggleGroupItem>
              <ToggleGroupItem
                value="restriction_no_longer_needed"
                className="flex-1"
              >
                已无必要
              </ToggleGroupItem>
            </ToggleGroup>
          </Field>
          <Field data-invalid={Boolean(validationError || mutation.isError)}>
            <FieldLabel htmlFor="manual-download-restriction-revoke-reason">
              人工理由
            </FieldLabel>
            <Textarea
              id="manual-download-restriction-revoke-reason"
              value={reason}
              rows={4}
              maxLength={500}
              onChange={(event) => {
                setReason(event.target.value)
                setValidationError(undefined)
                mutation.reset()
              }}
              aria-invalid={Boolean(validationError || mutation.isError)}
              placeholder="可留空；系统会自动记录解除理由"
            />
            {validationError ? (
              <FieldError>{validationError}</FieldError>
            ) : null}
            {mutation.isError ? (
              <FieldError>
                {requestErrorDescription(
                  mutation.error,
                  "解除失败，请刷新用户详情后重试。"
                )}
              </FieldError>
            ) : null}
          </Field>
        </FieldGroup>
        <AlertDialogFooter>
          <AlertDialogCancel disabled={mutation.isPending}>
            取消
          </AlertDialogCancel>
          <AlertDialogAction
            disabled={mutation.isPending}
            onClick={(event) => {
              event.preventDefault()
              void handleRevoke()
            }}
          >
            {mutation.isPending ? <Spinner data-icon="inline-start" /> : null}
            {mutation.isPending ? "正在解除…" : "确认解除"}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}

import * as React from "react"
import { useQueryClient } from "@tanstack/react-query"
import { REGEXP_ONLY_DIGITS } from "input-otp"
import {
  CheckIcon,
  CircleAlertIcon,
  CopyIcon,
  KeyRoundIcon,
  RotateCcwKeyIcon,
  ShieldCheckIcon,
  ShieldOffIcon,
} from "lucide-react"
import { QRCodeSVG } from "qrcode.react"

import { Alert, AlertDescription, AlertTitle } from "~/components/ui/alert"
import { Button } from "~/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "~/components/ui/card"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "~/components/ui/dialog"
import {
  Field,
  FieldError,
  FieldGroup,
  FieldLabel,
} from "~/components/ui/field"
import { Input } from "~/components/ui/input"
import {
  InputOTP,
  InputOTPGroup,
  InputOTPSlot,
} from "~/components/ui/input-otp"
import { Spinner } from "~/components/ui/spinner"
import type { components } from "~/generated/api"
import {
  useConfirmTOTPEnrollment,
  useDisableTOTP,
  useRotateTOTPRecoveryCodes,
  useStartTOTPEnrollment,
  type RecoveryCodeRotationResult,
  type TOTPEnrollmentResult,
} from "~/features/auth/api/two-factor.mutations"
import { sessionSecurityKeys } from "~/features/auth/api/session-security.queries"
import { ApiProblemError } from "~/shared/api/problem"
import { formatDateTime } from "~/shared/formatters/date-time"

type TwoFactorStatus = components["schemas"]["TwoFactorStatus"]

export function TwoFactorCard({
  status,
  userId,
  csrfToken,
}: {
  status: TwoFactorStatus
  userId: string
  csrfToken: string
}) {
  return (
    <Card className="gap-0 rounded-lg py-0 shadow-sm">
      <CardHeader className="px-6 pt-6 pb-5">
        <CardTitle className="text-2xl leading-none font-semibold">
          两步验证
        </CardTitle>
      </CardHeader>
      <CardContent className="px-6 pb-6">
        <div
          className={
            status.enabled
              ? "flex flex-col gap-4 rounded-md bg-success/10 p-4 sm:flex-row sm:items-center sm:justify-between"
              : "flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between"
          }
        >
          <div className="flex items-start gap-3 text-sm">
            {status.enabled ? (
              <ShieldCheckIcon className="mt-0.5 size-5 shrink-0 text-success" />
            ) : (
              <ShieldOffIcon className="mt-0.5 size-5 shrink-0 text-muted-foreground" />
            )}
            <div className="grid gap-1">
              <p
                className={
                  status.enabled ? "font-medium text-success" : "font-medium"
                }
              >
                {status.enabled ? "两步验证已启用" : "两步验证尚未启用"}
              </p>
              <p className="text-xs text-muted-foreground">
                {status.enabled
                  ? `您的账户已受到额外保护（剩余 ${status.recovery_codes_remaining} 个恢复码）`
                  : "启用后，登录时需要同时验证密码和动态码或恢复码。"}
              </p>
              {status.enabled && status.enabled_at ? (
                <p className="text-xs text-muted-foreground">
                  启用于 {formatDateTime(status.enabled_at)}
                </p>
              ) : null}
            </div>
          </div>
          <div className="flex shrink-0 flex-wrap gap-2">
            {status.enabled ? (
              <>
                <RotateRecoveryCodesDialog
                  userId={userId}
                  csrfToken={csrfToken}
                />
                <DisableTOTPDialog userId={userId} csrfToken={csrfToken} />
              </>
            ) : (
              <EnableTOTPDialog userId={userId} csrfToken={csrfToken} />
            )}
          </div>
        </div>
      </CardContent>
    </Card>
  )
}

function EnableTOTPDialog({
  userId,
  csrfToken,
}: {
  userId: string
  csrfToken: string
}) {
  const [open, setOpen] = React.useState(false)
  const [code, setCode] = React.useState("")
  const queryClient = useQueryClient()
  const start = useStartTOTPEnrollment()
  const confirm = useConfirmTOTPEnrollment()
  const busy = start.isPending || confirm.isPending

  function reset() {
    setCode("")
    start.reset()
    confirm.reset()
  }

  function handleOpenChange(nextOpen: boolean) {
    if (busy) return
    setOpen(nextOpen)
    if (!nextOpen) reset()
  }

  async function handleStart(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const password = new FormData(event.currentTarget).get("password")
    if (typeof password !== "string" || !password) return
    try {
      await start.mutateAsync({ csrfToken, password })
    } catch {
      // The dialog renders only the stable problem contract below.
    }
  }

  async function handleConfirm(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!start.data || code.length !== 6) return
    try {
      await confirm.mutateAsync({
        csrfToken,
        enrollmentId: start.data.enrollment_id,
        code,
      })
    } catch {
      // Keep the enrollment and code controls available for a safe retry.
    }
  }

  const enrollmentReady =
    start.data?.secret && start.data.provisioning_uri ? start.data : undefined

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogTrigger render={<Button size="sm" />}>
        <ShieldCheckIcon data-icon="inline-start" />
        启用两步验证
      </DialogTrigger>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>启用验证器动态码</DialogTitle>
          <DialogDescription>
            先重新输入当前密码，再用验证器扫描二维码并提交六位动态码。
          </DialogDescription>
        </DialogHeader>

        {confirm.data ? (
          <RecoveryCodesView
            result={confirm.data}
            onDone={() => {
              // Refresh only after the user acknowledges the one-time codes.
              // Refreshing on mutation success flips the parent status to
              // enabled and unmounts this dialog before the codes can be read.
              handleOpenChange(false)
              void queryClient.invalidateQueries({
                queryKey: sessionSecurityKeys.overview(userId),
              })
            }}
          />
        ) : enrollmentReady ? (
          <form onSubmit={handleConfirm} className="grid gap-4">
            <div className="grid gap-4 sm:grid-cols-[176px_1fr] sm:items-center">
              <div className="flex justify-center rounded-lg border bg-white p-3">
                <QRCodeSVG
                  value={enrollmentReady.provisioning_uri}
                  size={152}
                  level="M"
                  title="PeerGo 两步验证配置二维码"
                />
              </div>
              <div className="grid gap-2 text-sm">
                <p className="font-medium">无法扫码时手动输入</p>
                <code className="rounded-md bg-muted px-2 py-1.5 text-xs break-all">
                  {enrollmentReady.secret}
                </code>
                <CopyButton value={enrollmentReady.secret} label="复制密钥" />
                <p className="text-xs text-muted-foreground">
                  本次登记将在 {formatDateTime(enrollmentReady.expires_at)}{" "}
                  过期。
                </p>
              </div>
            </div>

            {confirm.isError ? <TwoFactorError error={confirm.error} /> : null}

            <Field data-invalid={code.length > 0 && code.length !== 6}>
              <FieldLabel htmlFor="totp-confirmation-code">
                验证器动态码
              </FieldLabel>
              <InputOTP
                id="totp-confirmation-code"
                name="code"
                maxLength={6}
                pattern={REGEXP_ONLY_DIGITS}
                value={code}
                onChange={setCode}
                disabled={confirm.isPending}
                autoComplete="one-time-code"
                aria-invalid={code.length > 0 && code.length !== 6}
              >
                <InputOTPGroup>
                  {Array.from({ length: 6 }, (_, index) => (
                    <InputOTPSlot
                      key={index}
                      index={index}
                      className="size-10"
                    />
                  ))}
                </InputOTPGroup>
              </InputOTP>
              <FieldError
                errors={
                  code.length > 0 && code.length !== 6
                    ? [{ message: "请输入完整的六位动态码" }]
                    : []
                }
              />
            </Field>

            <DialogFooter>
              <Button
                type="button"
                variant="outline"
                onClick={() => handleOpenChange(false)}
                disabled={busy}
              >
                取消
              </Button>
              <Button type="submit" disabled={code.length !== 6 || busy}>
                {confirm.isPending ? (
                  <Spinner data-icon="inline-start" />
                ) : null}
                验证并启用
              </Button>
            </DialogFooter>
          </form>
        ) : (
          <form onSubmit={handleStart} className="grid gap-4">
            {start.isError ? <TwoFactorError error={start.error} /> : null}
            <Field>
              <FieldLabel htmlFor="totp-enrollment-password">
                当前密码
              </FieldLabel>
              <Input
                id="totp-enrollment-password"
                name="password"
                type="password"
                autoComplete="current-password"
                required
                maxLength={1024}
                disabled={start.isPending}
              />
            </Field>
            <DialogFooter>
              <Button
                type="button"
                variant="outline"
                onClick={() => handleOpenChange(false)}
                disabled={busy}
              >
                取消
              </Button>
              <Button type="submit" disabled={busy}>
                {start.isPending ? <Spinner data-icon="inline-start" /> : null}
                继续
              </Button>
            </DialogFooter>
          </form>
        )}
      </DialogContent>
    </Dialog>
  )
}

function RotateRecoveryCodesDialog({
  userId,
  csrfToken,
}: {
  userId: string
  csrfToken: string
}) {
  const [open, setOpen] = React.useState(false)
  const idempotencyKey = React.useRef<string>(undefined)
  const rotation = useRotateTOTPRecoveryCodes(userId)

  function handleOpenChange(nextOpen: boolean) {
    if (rotation.isPending) return
    setOpen(nextOpen)
    if (!nextOpen) {
      idempotencyKey.current = undefined
      rotation.reset()
    }
  }

  async function handleSubmit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const formData = new FormData(event.currentTarget)
    const password = formData.get("password")
    const secondFactorCode = formData.get("second-factor-code")
    if (
      typeof password !== "string" ||
      !password ||
      typeof secondFactorCode !== "string" ||
      !secondFactorCode
    )
      return
    idempotencyKey.current ??= globalThis.crypto.randomUUID()
    try {
      await rotation.mutateAsync({
        csrfToken,
        idempotencyKey: idempotencyKey.current,
        password,
        secondFactorCode,
      })
    } catch {
      // Retain the same idempotency key while this dialog remains open.
    }
  }

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogTrigger render={<Button variant="outline" size="sm" />}>
        <RotateCcwKeyIcon data-icon="inline-start" />
        更新恢复码
      </DialogTrigger>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>生成新的恢复码</DialogTitle>
          <DialogDescription>
            原有未使用恢复码会全部失效。请使用当前密码与动态码或一枚恢复码重新验证。
          </DialogDescription>
        </DialogHeader>
        {rotation.data ? (
          <RecoveryCodesView
            result={rotation.data}
            onDone={() => handleOpenChange(false)}
          />
        ) : (
          <ReauthenticationForm
            pending={rotation.isPending}
            error={rotation.error}
            submitLabel="生成新恢复码"
            onCancel={() => handleOpenChange(false)}
            onSubmit={handleSubmit}
          />
        )}
      </DialogContent>
    </Dialog>
  )
}

function DisableTOTPDialog({
  userId,
  csrfToken,
}: {
  userId: string
  csrfToken: string
}) {
  const [open, setOpen] = React.useState(false)
  const idempotencyKey = React.useRef<string>(undefined)
  const disable = useDisableTOTP(userId)

  function handleOpenChange(nextOpen: boolean) {
    if (disable.isPending) return
    setOpen(nextOpen)
    if (!nextOpen) {
      idempotencyKey.current = undefined
      disable.reset()
    }
  }

  async function handleSubmit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const formData = new FormData(event.currentTarget)
    const password = formData.get("password")
    const secondFactorCode = formData.get("second-factor-code")
    if (
      typeof password !== "string" ||
      !password ||
      typeof secondFactorCode !== "string" ||
      !secondFactorCode
    )
      return
    idempotencyKey.current ??= globalThis.crypto.randomUUID()
    try {
      await disable.mutateAsync({
        csrfToken,
        idempotencyKey: idempotencyKey.current,
        password,
        secondFactorCode,
      })
      handleOpenChange(false)
    } catch {
      // Retain the same idempotency key for a safe retry.
    }
  }

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogTrigger render={<Button variant="ghost" size="sm" />}>
        <ShieldOffIcon data-icon="inline-start" />
        关闭
      </DialogTrigger>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>关闭两步验证？</DialogTitle>
          <DialogDescription>
            所有恢复码会同时失效，其他浏览器和后台会话也会被撤销；当前浏览器保持登录。
          </DialogDescription>
        </DialogHeader>
        <Alert variant="destructive">
          <CircleAlertIcon />
          <AlertTitle>账户保护会降低</AlertTitle>
          <AlertDescription>
            关闭后，站内密码将重新成为普通 Web 登录的唯一因素。
          </AlertDescription>
        </Alert>
        <ReauthenticationForm
          pending={disable.isPending}
          error={disable.error}
          destructive
          submitLabel="确认关闭"
          onCancel={() => handleOpenChange(false)}
          onSubmit={handleSubmit}
        />
      </DialogContent>
    </Dialog>
  )
}

function ReauthenticationForm({
  pending,
  error,
  submitLabel,
  destructive = false,
  onCancel,
  onSubmit,
}: {
  pending: boolean
  error: Error | null
  submitLabel: string
  destructive?: boolean
  onCancel: () => void
  onSubmit: (event: React.FormEvent<HTMLFormElement>) => void
}) {
  return (
    <form onSubmit={onSubmit} className="grid gap-4">
      {error ? <TwoFactorError error={error} /> : null}
      <FieldGroup>
        <Field>
          <FieldLabel htmlFor={`${submitLabel}-password`}>当前密码</FieldLabel>
          <Input
            id={`${submitLabel}-password`}
            name="password"
            type="password"
            autoComplete="current-password"
            required
            maxLength={1024}
            disabled={pending}
          />
        </Field>
        <Field>
          <FieldLabel htmlFor={`${submitLabel}-second-factor`}>
            动态码 / 恢复码
          </FieldLabel>
          <Input
            id={`${submitLabel}-second-factor`}
            name="second-factor-code"
            autoComplete="one-time-code"
            required
            maxLength={32}
            placeholder="6 位动态码或 XXXX-XXXX-XXXX"
            disabled={pending}
          />
        </Field>
      </FieldGroup>
      <DialogFooter>
        <Button
          type="button"
          variant="outline"
          onClick={onCancel}
          disabled={pending}
        >
          取消
        </Button>
        <Button
          type="submit"
          variant={destructive ? "destructive" : "default"}
          disabled={pending}
        >
          {pending ? <Spinner data-icon="inline-start" /> : null}
          {submitLabel}
        </Button>
      </DialogFooter>
    </form>
  )
}

function RecoveryCodesView({
  result,
  onDone,
}: {
  result: TOTPEnrollmentResult | RecoveryCodeRotationResult
  onDone: () => void
}) {
  return (
    <div className="grid gap-4">
      <Alert>
        <KeyRoundIcon />
        <AlertTitle>立即保存这组恢复码</AlertTitle>
        <AlertDescription>
          每枚只能使用一次，关闭本窗口后将无法再次查看这组恢复码。
        </AlertDescription>
      </Alert>
      <div
        className="grid grid-cols-2 gap-x-4 gap-y-2 rounded-lg border bg-muted/30 p-3 font-mono text-sm"
        aria-label="一次性恢复码"
      >
        {result.recovery_codes.map((code) => (
          <code key={code}>{code}</code>
        ))}
      </div>
      <div className="flex flex-wrap justify-between gap-2">
        <CopyButton value={result.recovery_codes.join("\n")} label="复制全部" />
        <Button onClick={onDone}>
          <CheckIcon data-icon="inline-start" />
          我已安全保存
        </Button>
      </div>
    </div>
  )
}

function CopyButton({ value, label }: { value: string; label: string }) {
  const [copied, setCopied] = React.useState(false)

  async function copy() {
    try {
      await navigator.clipboard.writeText(value)
      setCopied(true)
    } catch {
      setCopied(false)
    }
  }

  return (
    <Button
      type="button"
      variant="outline"
      size="sm"
      onClick={() => void copy()}
    >
      {copied ? (
        <CheckIcon data-icon="inline-start" />
      ) : (
        <CopyIcon data-icon="inline-start" />
      )}
      {copied ? "已复制" : label}
    </Button>
  )
}

function TwoFactorError({ error }: { error: Error }) {
  const description =
    error instanceof ApiProblemError && error.requestId
      ? `请检查输入后重试。请求编号：${error.requestId}`
      : "请检查输入与当前安全状态后重试。"
  return (
    <Alert variant="destructive">
      <CircleAlertIcon />
      <AlertTitle>
        {error instanceof ApiProblemError ? error.message : "操作失败"}
      </AlertTitle>
      <AlertDescription>{description}</AlertDescription>
    </Alert>
  )
}

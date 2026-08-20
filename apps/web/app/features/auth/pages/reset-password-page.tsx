import * as React from "react"
import { Link } from "react-router"
import {
  CircleAlertIcon,
  CircleCheckIcon,
  CircleXIcon,
  KeyRoundIcon,
} from "lucide-react"

import { Alert, AlertDescription, AlertTitle } from "~/components/ui/alert"
import { Button } from "~/components/ui/button"
import {
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from "~/components/ui/card"
import {
  Field,
  FieldError,
  FieldGroup,
  FieldLabel,
} from "~/components/ui/field"
import { Input } from "~/components/ui/input"
import { Spinner } from "~/components/ui/spinner"
import { useConfirmPasswordRecovery } from "~/features/auth/api/password-recovery.mutations"
import { AuthEntryCard } from "~/features/auth/components/auth-entry-card"
import { useFragmentToken } from "~/features/auth/hooks/use-fragment-token"
import { opaqueTokenPattern } from "~/features/auth/model/opaque-token"
import {
  passwordRecoveryFieldErrors,
  passwordRecoveryFormSchema,
  type PasswordRecoveryFormErrors,
} from "~/features/auth/model/password-recovery-form"
import { ApiProblemError } from "~/shared/api/problem"

export function ResetPasswordPage() {
  const token = useFragmentToken()

  // A second link opened in the same tab must not inherit form or mutation
  // state from the previous credential.
  return <ResetPasswordContent key={token} token={token} />
}

function ResetPasswordContent({ token }: { token: string }) {
  const validToken = opaqueTokenPattern.test(token)
  const confirmRecovery = useConfirmPasswordRecovery()
  const [errors, setErrors] = React.useState<PasswordRecoveryFormErrors>({})

  async function handleSubmit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    confirmRecovery.reset()
    const form = event.currentTarget
    const formData = new FormData(form)
    const parsed = passwordRecoveryFormSchema.safeParse({
      password: formData.get("password"),
      confirmPassword: formData.get("confirmPassword"),
    })
    if (!parsed.success) {
      const nextErrors = passwordRecoveryFieldErrors(parsed.error)
      setErrors(nextErrors)
      const first = nextErrors.password ? "password" : "confirmPassword"
      const control = form.elements.namedItem(first)
      if (control instanceof HTMLElement) control.focus()
      return
    }
    setErrors({})
    try {
      await confirmRecovery.mutateAsync({
        token,
        newPassword: parsed.data.password,
      })
      form.reset()
    } catch {
      // Keep the page token in component memory for a safe retry. It has
      // already been removed from the visible URL and never enters query data.
    }
  }

  if (!validToken) {
    return (
      <AuthEntryCard
        className="gap-0 py-0"
        aria-labelledby="reset-password-invalid-title"
      >
        <CardContent className="p-6">
          <div className="flex flex-col gap-4 text-center">
            <div className="mx-auto flex size-12 items-center justify-center rounded-full bg-destructive/10">
              <CircleXIcon className="size-6 text-destructive" />
            </div>
            <h1
              id="reset-password-invalid-title"
              className="text-xl font-semibold"
            >
              链接无效或已过期
            </h1>
            <p className="text-muted-foreground">
              该密码重置链接无效或已过期。
              <br />
              请重新申请密码重置。
            </p>
          </div>
        </CardContent>
        <CardFooter className="justify-center gap-4 border-t-0 bg-transparent px-6 pt-0 pb-6">
          <Button
            variant="outline"
            render={<Link to="/forgot-password" />}
            nativeButton={false}
          >
            重新申请
          </Button>
          <Button render={<Link to="/login" />} nativeButton={false}>
            返回登录
          </Button>
        </CardFooter>
      </AuthEntryCard>
    )
  }

  if (confirmRecovery.data) {
    return (
      <AuthEntryCard
        className="gap-0 py-0"
        aria-labelledby="reset-password-success-title"
      >
        <CardContent className="p-6">
          <div className="flex flex-col gap-4 text-center">
            <div className="mx-auto flex size-12 items-center justify-center rounded-full bg-success/10">
              <CircleCheckIcon className="size-6 text-success" />
            </div>
            <h1
              id="reset-password-success-title"
              className="text-xl font-semibold"
            >
              {confirmRecovery.data.already_completed
                ? "密码此前已经重置"
                : "密码重置成功"}
            </h1>
            <p className="text-muted-foreground">
              所有旧会话均已撤销。
              <br />
              请使用新密码重新登录。
            </p>
          </div>
        </CardContent>
        <CardFooter className="justify-center border-t-0 bg-transparent px-6 pt-0 pb-6">
          <Link to="/login">立即登录</Link>
        </CardFooter>
      </AuthEntryCard>
    )
  }

  return (
    <AuthEntryCard aria-labelledby="reset-password-title">
      <CardHeader className="gap-2 px-6">
        <CardTitle>
          <h1
            id="reset-password-title"
            className="flex items-center gap-2 text-2xl leading-none font-semibold tracking-tight"
          >
            <KeyRoundIcon />
            重置密码
          </h1>
        </CardTitle>
        <CardDescription>
          设置新密码后，该账户在所有设备上的 Web 与后台会话都会失效。
        </CardDescription>
      </CardHeader>

      <CardContent className="px-6">
        <form id="reset-password-form" onSubmit={handleSubmit} noValidate>
          <FieldGroup className="gap-4">
            {confirmRecovery.isError ? (
              <Alert variant="destructive">
                <CircleAlertIcon />
                <AlertTitle>
                  {confirmationErrorTitle(confirmRecovery.error)}
                </AlertTitle>
                <AlertDescription>
                  {confirmationErrorDescription(confirmRecovery.error)}
                </AlertDescription>
              </Alert>
            ) : null}
            <Field data-invalid={Boolean(errors.password)}>
              <FieldLabel htmlFor="password">新密码</FieldLabel>
              <Input
                id="password"
                name="password"
                type="password"
                autoComplete="new-password"
                placeholder="请输入新密码（至少 12 位）"
                aria-invalid={Boolean(errors.password)}
                disabled={confirmRecovery.isPending}
                className="h-10"
              />
              <FieldError
                errors={errors.password ? [{ message: errors.password }] : []}
              />
            </Field>
            <Field data-invalid={Boolean(errors.confirmPassword)}>
              <FieldLabel htmlFor="confirmPassword">确认密码</FieldLabel>
              <Input
                id="confirmPassword"
                name="confirmPassword"
                type="password"
                autoComplete="new-password"
                placeholder="请再次输入新密码"
                aria-invalid={Boolean(errors.confirmPassword)}
                disabled={confirmRecovery.isPending}
                className="h-10"
              />
              <FieldError
                errors={
                  errors.confirmPassword
                    ? [{ message: errors.confirmPassword }]
                    : []
                }
              />
            </Field>
          </FieldGroup>
        </form>
      </CardContent>

      <CardFooter className="flex-col gap-4 border-t-0 bg-transparent px-6">
        <Button
          type="submit"
          form="reset-password-form"
          size="lg"
          className="h-10 w-full"
          disabled={confirmRecovery.isPending}
        >
          {confirmRecovery.isPending ? (
            <>
              <Spinner data-icon="inline-start" />
              正在重置…
            </>
          ) : (
            "重置密码"
          )}
        </Button>
        <Link to="/login" className="text-sm text-muted-foreground">
          返回登录
        </Link>
      </CardFooter>
    </AuthEntryCard>
  )
}

function confirmationErrorTitle(error: Error) {
  return error instanceof ApiProblemError ? error.message : "重置失败"
}

function confirmationErrorDescription(error: Error) {
  if (error instanceof ApiProblemError) {
    if (error.status === 404) {
      return "链接可能已过期、已使用或已被新的找回邮件替换。"
    }
    return error.requestId
      ? `可以保留本页后重试。请求编号：${error.requestId}`
      : "请稍后重试。"
  }
  return "密码恢复服务暂时不可用，请稍后重试。"
}

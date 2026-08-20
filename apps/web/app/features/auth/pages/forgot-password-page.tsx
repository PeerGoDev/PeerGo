import * as React from "react"
import { Link } from "react-router"
import {
  ArrowLeftIcon,
  CircleAlertIcon,
  CircleCheckIcon,
  MailIcon,
} from "lucide-react"

import { Alert, AlertDescription, AlertTitle } from "~/components/ui/alert"
import { Button } from "~/components/ui/button"
import {
  CardContent,
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
import { useRequestPasswordRecovery } from "~/features/auth/api/password-recovery.mutations"
import {
  AuthEntryCard,
  AuthEntryLoading,
} from "~/features/auth/components/auth-entry-card"
import { TurnstileWidget } from "~/features/auth/components/turnstile-widget"
import { emailAddressSchema } from "~/features/auth/model/email-address"
import { useSiteInfo } from "~/features/site/api/site.queries"
import { ApiProblemError } from "~/shared/api/problem"

export function ForgotPasswordPage() {
  const siteInfo = useSiteInfo()
  const requestRecovery = useRequestPasswordRecovery()
  const [emailError, setEmailError] = React.useState<string>()
  const [humanVerificationToken, setHumanVerificationToken] = React.useState("")
  const [humanVerificationResetKey, setHumanVerificationResetKey] =
    React.useState(0)

  if (siteInfo.isPending) {
    return <AuthEntryLoading label="正在读取密码恢复策略" />
  }
  if (siteInfo.isError || !siteInfo.data) {
    return (
      <AuthEntryCard aria-labelledby="forgot-password-unavailable-title">
        <CardContent className="px-6 pt-2">
          <Alert variant="destructive">
            <CircleAlertIcon />
            <AlertTitle id="forgot-password-unavailable-title">
              暂时无法读取密码恢复策略
            </AlertTitle>
            <AlertDescription>请稍后刷新页面再试。</AlertDescription>
          </Alert>
        </CardContent>
      </AuthEntryCard>
    )
  }

  const humanVerification = siteInfo.data.human_verification
  const humanVerificationEnabled =
    humanVerification.provider === "turnstile" &&
    humanVerification.password_recovery_enabled

  async function handleSubmit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    requestRecovery.reset()
    const form = event.currentTarget
    const parsed = emailAddressSchema.safeParse(new FormData(form).get("email"))
    if (!parsed.success) {
      setEmailError(parsed.error.issues[0]?.message ?? "请输入有效邮箱")
      const control = form.elements.namedItem("email")
      if (control instanceof HTMLElement) control.focus()
      return
    }
    if (humanVerificationEnabled && !humanVerificationToken) {
      return
    }
    setEmailError(undefined)
    try {
      await requestRecovery.mutateAsync({
        email: parsed.data,
        ...(humanVerificationEnabled
          ? { human_verification_token: humanVerificationToken }
          : {}),
      })
    } catch {
      if (humanVerificationEnabled) {
        setHumanVerificationToken("")
        setHumanVerificationResetKey((current) => current + 1)
      }
      // Do not copy the raw address into shared query state; the mutation
      // exposes only the stable problem contract to the rendered error.
    }
  }

  if (requestRecovery.data) {
    return (
      <AuthEntryCard aria-labelledby="forgot-password-success-title">
        <CardContent className="px-6 pt-2">
          <div className="flex flex-col gap-4 text-center">
            <div className="mx-auto flex size-12 items-center justify-center rounded-full bg-success/10">
              <CircleCheckIcon className="size-6 text-success" />
            </div>
            <h1
              id="forgot-password-success-title"
              className="text-xl font-semibold"
            >
              邮件已发送
            </h1>
            <p className="text-muted-foreground">
              如果该邮箱属于已验证账户，重置邮件已经发送。
              <br />
              请同时检查垃圾邮件目录。
            </p>
            <p className="text-sm text-muted-foreground">
              重置链接有效期为 30 分钟
            </p>
          </div>
        </CardContent>
        <CardFooter className="justify-center border-t-0 bg-transparent">
          <Link to="/login" className="flex items-center gap-1">
            <ArrowLeftIcon className="size-4" />
            返回登录
          </Link>
        </CardFooter>
      </AuthEntryCard>
    )
  }

  return (
    <AuthEntryCard aria-labelledby="forgot-password-title">
      <CardHeader className="gap-2 px-6">
        <CardTitle>
          <h1
            id="forgot-password-title"
            className="flex items-center gap-2 text-2xl leading-none font-semibold tracking-tight"
          >
            <MailIcon />
            找回密码
          </h1>
        </CardTitle>
      </CardHeader>

      <CardContent className="px-6">
        <form id="forgot-password-form" onSubmit={handleSubmit} noValidate>
          <FieldGroup className="gap-4">
            <p className="text-sm text-muted-foreground">
              请输入注册时使用的邮箱地址，我们会发送一条 30
              分钟内有效的密码重置链接。
            </p>
            {requestRecovery.isError ? (
              <Alert variant="destructive">
                <CircleAlertIcon />
                <AlertTitle>
                  {recoveryErrorTitle(requestRecovery.error)}
                </AlertTitle>
                <AlertDescription>
                  {recoveryErrorDescription(requestRecovery.error)}
                </AlertDescription>
              </Alert>
            ) : null}
            <Field data-invalid={Boolean(emailError)}>
              <FieldLabel htmlFor="email">邮箱地址</FieldLabel>
              <Input
                id="email"
                name="email"
                type="email"
                autoComplete="email"
                placeholder="请输入邮箱地址"
                aria-invalid={Boolean(emailError)}
                disabled={requestRecovery.isPending}
                className="h-10"
              />
              <FieldError
                errors={emailError ? [{ message: emailError }] : []}
              />
            </Field>
            <TurnstileWidget
              enabled={humanVerificationEnabled}
              siteKey={humanVerification.site_key}
              action="password_recovery"
              resetKey={humanVerificationResetKey}
              onTokenChange={setHumanVerificationToken}
            />
          </FieldGroup>
        </form>
      </CardContent>

      <CardFooter className="flex-col gap-4 border-t-0 bg-transparent px-6">
        <Button
          type="submit"
          form="forgot-password-form"
          size="lg"
          className="h-10 w-full"
          disabled={
            requestRecovery.isPending ||
            (humanVerificationEnabled && !humanVerificationToken)
          }
        >
          {requestRecovery.isPending ? (
            <>
              <Spinner data-icon="inline-start" />
              正在发送…
            </>
          ) : (
            "发送重置邮件"
          )}
        </Button>
        <Link to="/login" className="text-sm text-muted-foreground">
          返回登录
        </Link>
      </CardFooter>
    </AuthEntryCard>
  )
}

function recoveryErrorTitle(error: Error) {
  return error instanceof ApiProblemError ? error.message : "发送失败"
}

function recoveryErrorDescription(error: Error) {
  if (error instanceof ApiProblemError) {
    return error.requestId
      ? `请稍后重试。请求编号：${error.requestId}`
      : "请稍后重试。"
  }
  return "密码恢复服务暂时不可用，请稍后重试。"
}

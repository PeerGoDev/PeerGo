import * as React from "react"
import { Link, Navigate, useNavigate } from "react-router"
import { CircleAlertIcon, KeyRoundIcon } from "lucide-react"
import { z } from "zod"

import { Alert, AlertDescription, AlertTitle } from "~/components/ui/alert"
import { Button } from "~/components/ui/button"
import {
  CardContent,
  CardFooter,
  CardHeader,
  CardTitle,
} from "~/components/ui/card"
import { Checkbox } from "~/components/ui/checkbox"
import {
  Field,
  FieldError,
  FieldGroup,
  FieldLabel,
} from "~/components/ui/field"
import { Input } from "~/components/ui/input"
import { Spinner } from "~/components/ui/spinner"
import {
  useCreateWebSession,
  useWebSession,
} from "~/features/auth/api/session.mutations"
import {
  AuthEntryCard,
  AuthEntryLoading,
} from "~/features/auth/components/auth-entry-card"
import { TurnstileWidget } from "~/features/auth/components/turnstile-widget"
import { useSiteInfo } from "~/features/site/api/site.queries"
import { ApiProblemError } from "~/shared/api/problem"

const loginSchema = z.object({
  identifier: z
    .string()
    .trim()
    .min(1, "请输入用户名或邮箱")
    .max(254, "用户名或邮箱过长"),
  password: z.string().min(1, "请输入密码").max(1024, "密码过长"),
  secondFactorCode: z
    .string()
    .refine(
      (value) =>
        value === "" ||
        /^[0-9]{6}$/.test(value) ||
        /^[A-HJ-NP-Z2-9]{4}-?[A-HJ-NP-Z2-9]{4}-?[A-HJ-NP-Z2-9]{4}$/i.test(
          value
        ),
      { message: "请输入 6 位动态码或有效的恢复码" }
    ),
})

type LoginField = keyof z.infer<typeof loginSchema>
type LoginErrors = Partial<Record<LoginField, string>>

export function LoginPage() {
  const navigate = useNavigate()
  const webSession = useWebSession()
  const siteInfo = useSiteInfo()
  const createSession = useCreateWebSession()
  const [errors, setErrors] = React.useState<LoginErrors>({})
  const [secondFactorRequired, setSecondFactorRequired] = React.useState(false)
  const [humanVerificationToken, setHumanVerificationToken] = React.useState("")
  const [humanVerificationResetKey, setHumanVerificationResetKey] =
    React.useState(0)
  const secondFactorInputRef = React.useRef<HTMLInputElement>(null)

  React.useEffect(() => {
    if (secondFactorRequired) {
      secondFactorInputRef.current?.focus()
    }
  }, [secondFactorRequired])

  if (webSession.isPending || siteInfo.isPending) {
    return <AuthEntryLoading label="正在读取登录状态" />
  }
  if (webSession.data) {
    return <Navigate to="/" replace />
  }
  if (siteInfo.isError || !siteInfo.data) {
    return (
      <AuthEntryCard aria-labelledby="login-unavailable-title">
        <CardContent className="px-6 pt-2">
          <Alert variant="destructive">
            <CircleAlertIcon />
            <AlertTitle id="login-unavailable-title">
              暂时无法读取登录策略
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
    humanVerification.login_enabled

  async function handleSubmit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    createSession.reset()
    const form = event.currentTarget
    const formData = new FormData(form)
    const result = loginSchema.safeParse({
      identifier: formData.get("identifier"),
      password: formData.get("password"),
      secondFactorCode: formData.get("second-factor-code") ?? "",
    })

    if (!result.success) {
      const fieldErrors = result.error.flatten().fieldErrors
      const nextErrors: LoginErrors = {
        identifier: fieldErrors.identifier?.[0],
        password: fieldErrors.password?.[0],
        secondFactorCode: fieldErrors.secondFactorCode?.[0],
      }
      setErrors(nextErrors)
      const firstInvalidField: LoginField = nextErrors.identifier
        ? "identifier"
        : nextErrors.password
          ? "password"
          : "secondFactorCode"
      const control = form.elements.namedItem(
        firstInvalidField === "secondFactorCode"
          ? "second-factor-code"
          : firstInvalidField
      )
      if (control instanceof HTMLElement) {
        control.focus()
      }
      return
    }

    if (secondFactorRequired && !result.data.secondFactorCode) {
      setErrors({ secondFactorCode: "请输入动态码或一枚恢复码" })
      const control = form.elements.namedItem("second-factor-code")
      if (control instanceof HTMLElement) control.focus()
      return
    }
    if (humanVerificationEnabled && !humanVerificationToken) {
      return
    }

    setErrors({})
    try {
      const session = await createSession.mutateAsync({
        identifier: result.data.identifier,
        password: result.data.password,
        ...(result.data.secondFactorCode
          ? { second_factor_code: result.data.secondFactorCode }
          : {}),
        remember_me: formData.get("remember") === "on",
        ...(humanVerificationEnabled
          ? { human_verification_token: humanVerificationToken }
          : {}),
      })
      navigate(session.user.email_verified ? "/" : "/account/email", {
        replace: true,
      })
    } catch (error) {
      if (humanVerificationEnabled) {
        setHumanVerificationToken("")
        setHumanVerificationResetKey((current) => current + 1)
      }
      if (
        error instanceof ApiProblemError &&
        error.status === 428 &&
        error.code === "second_factor_required"
      ) {
        setSecondFactorRequired(true)
        setErrors({ secondFactorCode: "请输入动态码或一枚恢复码" })
      }
      // Mutation state renders the contract error below. Credentials remain in
      // the browser form only and are never copied into app/query state.
    }
  }

  return (
    <AuthEntryCard aria-labelledby="login-title">
      <CardHeader className="gap-2 px-6">
        <CardTitle>
          <h1
            id="login-title"
            className="text-2xl leading-none font-semibold tracking-tight"
          >
            登录
          </h1>
        </CardTitle>
      </CardHeader>

      <CardContent className="px-6">
        <form id="login-form" onSubmit={handleSubmit} noValidate>
          <FieldGroup>
            {createSession.isError &&
            !isSecondFactorChallenge(createSession.error) ? (
              <Alert variant="destructive">
                <CircleAlertIcon />
                <AlertTitle>{loginErrorTitle(createSession.error)}</AlertTitle>
                <AlertDescription>
                  {loginErrorDescription(createSession.error)}
                </AlertDescription>
              </Alert>
            ) : null}

            {secondFactorRequired ? (
              <Alert>
                <KeyRoundIcon />
                <AlertTitle>还需要第二因素</AlertTitle>
                <AlertDescription>
                  密码已通过验证。请输入验证器动态码，或使用一枚一次性恢复码。
                </AlertDescription>
              </Alert>
            ) : null}

            <Field data-invalid={Boolean(errors.identifier)}>
              <FieldLabel htmlFor="identifier">用户名 / 邮箱</FieldLabel>
              <Input
                id="identifier"
                name="identifier"
                autoComplete="username"
                placeholder="输入用户名或邮箱地址"
                aria-invalid={Boolean(errors.identifier)}
                disabled={createSession.isPending}
                className="h-10"
              />
              <FieldError
                errors={
                  errors.identifier ? [{ message: errors.identifier }] : []
                }
              />
            </Field>

            <Field data-invalid={Boolean(errors.password)}>
              <FieldLabel htmlFor="password">密码</FieldLabel>
              <Input
                id="password"
                name="password"
                type="password"
                autoComplete="current-password"
                aria-invalid={Boolean(errors.password)}
                disabled={createSession.isPending}
                className="h-10"
              />
              <FieldError
                errors={errors.password ? [{ message: errors.password }] : []}
              />
            </Field>

            <Field data-invalid={Boolean(errors.secondFactorCode)}>
              <FieldLabel htmlFor="second-factor-code">
                两步验证码（可选）
              </FieldLabel>
              <Input
                ref={secondFactorInputRef}
                id="second-factor-code"
                name="second-factor-code"
                autoComplete="one-time-code"
                maxLength={32}
                placeholder="如果启用了两步验证，请输入 6 位数字"
                aria-invalid={Boolean(errors.secondFactorCode)}
                disabled={createSession.isPending}
                className="h-10"
              />
              <FieldError
                errors={
                  errors.secondFactorCode
                    ? [{ message: errors.secondFactorCode }]
                    : []
                }
              />
            </Field>

            <Field orientation="horizontal">
              <Checkbox
                id="remember"
                name="remember"
                disabled={createSession.isPending}
              />
              <FieldLabel htmlFor="remember" className="font-normal">
                记住登录（30 天内免登录）
              </FieldLabel>
            </Field>

            <TurnstileWidget
              enabled={humanVerificationEnabled}
              siteKey={humanVerification.site_key}
              action="login"
              resetKey={humanVerificationResetKey}
              onTokenChange={setHumanVerificationToken}
            />

            <Button
              type="submit"
              size="lg"
              className="h-10 w-full"
              disabled={
                createSession.isPending ||
                (humanVerificationEnabled && !humanVerificationToken)
              }
            >
              {createSession.isPending ? (
                <>
                  <Spinner />
                  登录中…
                </>
              ) : (
                "登录"
              )}
            </Button>
          </FieldGroup>
        </form>
      </CardContent>

      <CardFooter className="flex-col gap-3 border-t-0 bg-transparent px-6 text-xs text-muted-foreground">
        <div className="flex w-full items-center justify-between gap-3 text-sm">
          <Link to="/forgot-password">忘记密码？</Link>
          <span>
            没有账号？<Link to="/register">注册</Link>
          </span>
        </div>
        <p className="w-full border-t pt-3 text-center">
          <Link to="/restrictions">查看封禁记录或提交申诉</Link>
        </p>
      </CardFooter>
    </AuthEntryCard>
  )
}

function loginErrorTitle(error: Error) {
  return error instanceof ApiProblemError ? error.message : "登录失败"
}

function isSecondFactorChallenge(error: Error) {
  return (
    error instanceof ApiProblemError &&
    error.status === 428 &&
    error.code === "second_factor_required"
  )
}

function loginErrorDescription(error: Error) {
  if (error instanceof ApiProblemError) {
    if (error.status === 429) {
      return "失败次数较多，请短暂等待后再试。该提示不会说明账户是否存在。"
    }
    return error.requestId
      ? `请检查登录信息后重试。请求编号：${error.requestId}`
      : "请检查登录信息后重试。"
  }
  return "服务暂时不可用，请稍后重试。"
}

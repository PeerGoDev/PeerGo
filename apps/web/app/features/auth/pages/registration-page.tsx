import * as React from "react"
import { Link, Navigate, useSearchParams } from "react-router"
import {
  ArrowRightIcon,
  CircleAlertIcon,
  CircleCheckIcon,
  GiftIcon,
  LockIcon,
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
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLabel,
} from "~/components/ui/field"
import { Input } from "~/components/ui/input"
import { Spinner } from "~/components/ui/spinner"
import { useWebSession } from "~/features/auth/api/session.mutations"
import { useCreateRegistration } from "~/features/auth/api/registration.mutations"
import {
  AuthEntryCard,
  AuthEntryLoading,
} from "~/features/auth/components/auth-entry-card"
import { TurnstileWidget } from "~/features/auth/components/turnstile-widget"
import {
  registrationFieldErrors,
  registrationFormSchema,
  type RegistrationFormErrors,
  type RegistrationFormField,
} from "~/features/auth/model/registration-form"
import { invitationTokenPattern } from "~/features/auth/model/invitation-token"
import { useSiteInfo } from "~/features/site/api/site.queries"
import { ApiProblemError, requestErrorDescription } from "~/shared/api/problem"

export function RegistrationPage() {
  const [searchParams, setSearchParams] = useSearchParams()
  const linkedInvitation = searchParams.get("invite")?.trim() ?? ""
  const webSession = useWebSession()
  const siteInfo = useSiteInfo()
  const createRegistration = useCreateRegistration()
  const [errors, setErrors] = React.useState<RegistrationFormErrors>({})
  const [invitationToken, setInvitationToken] = React.useState(() =>
    invitationTokenPattern.test(linkedInvitation) ? linkedInvitation : ""
  )
  const [invitationStepComplete, setInvitationStepComplete] = React.useState(
    () => invitationTokenPattern.test(linkedInvitation)
  )
  const [humanVerificationToken, setHumanVerificationToken] = React.useState("")
  const [humanVerificationResetKey, setHumanVerificationResetKey] =
    React.useState(0)
  const registrationAttempt = React.useRef<{
    fingerprint: string
    idempotencyKey: string
  } | null>(null)

  React.useEffect(() => {
    if (linkedInvitation) {
      setSearchParams({}, { replace: true })
    }
  }, [linkedInvitation, setSearchParams])

  if (webSession.isPending || siteInfo.isPending) {
    return <AuthEntryLoading label="正在读取注册策略" />
  }
  if (webSession.data) {
    return <Navigate to="/" replace />
  }
  if (siteInfo.isError || !siteInfo.data) {
    return (
      <RegistrationFrame>
        <Alert variant="destructive">
          <CircleAlertIcon />
          <AlertTitle>暂时无法读取注册策略</AlertTitle>
          <AlertDescription>
            {requestErrorDescription(
              siteInfo.error,
              "注册策略请求未能完成，请稍后刷新页面。"
            )}
          </AlertDescription>
        </Alert>
      </RegistrationFrame>
    )
  }
  if (siteInfo.data.registration_mode === "closed") {
    return <ClosedRegistrationState siteName={siteInfo.data.name} />
  }
  if (createRegistration.data) {
    return (
      <RegistrationFrame>
        <Alert className="border-success/30 bg-success/10">
          <CircleCheckIcon className="text-success" />
          <AlertTitle>账户已经创建</AlertTitle>
          <AlertDescription>
            请先使用用户名 @{createRegistration.data.user.username} 登录。
            登录后会引导你发送验证邮件；验证前不能使用邮箱登录。
          </AlertDescription>
        </Alert>
        <Button
          render={<Link to="/login" />}
          nativeButton={false}
          size="lg"
          className="h-10 w-full"
        >
          前往登录
        </Button>
      </RegistrationFrame>
    )
  }

  const mode = siteInfo.data.registration_mode
  const usernameMinCharacters =
    siteInfo.data.registration_username_min_characters
  const usernameMaxCharacters =
    siteInfo.data.registration_username_max_characters
  const emailDomainMode = siteInfo.data.registration_email_domain_mode
  const humanVerification = siteInfo.data.human_verification
  const humanVerificationEnabled =
    humanVerification.provider === "turnstile" &&
    humanVerification.registration_enabled

  if (mode === "invite" && !invitationStepComplete) {
    return (
      <InvitationRegistrationStep
        invitationToken={invitationToken}
        onInvitationTokenChange={setInvitationToken}
        onContinue={() => setInvitationStepComplete(true)}
      />
    )
  }

  async function handleSubmit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    createRegistration.reset()
    const form = event.currentTarget
    const formData = new FormData(form)
    const result = registrationFormSchema({
      mode,
      usernameMinCharacters,
      usernameMaxCharacters,
    }).safeParse({
      username: formData.get("username"),
      displayName: formData.get("displayName"),
      email: formData.get("email"),
      password: formData.get("password"),
      confirmPassword: formData.get("confirmPassword"),
      invitationToken,
    })
    if (!result.success) {
      const nextErrors = registrationFieldErrors(result.error)
      setErrors(nextErrors)
      focusFirstInvalidField(form, nextErrors)
      return
    }
    if (humanVerificationEnabled && !humanVerificationToken) {
      return
    }

    setErrors({})
    const fingerprint = JSON.stringify([
      result.data.username,
      result.data.displayName,
      result.data.email,
      result.data.password,
      result.data.invitationToken ?? "",
    ])
    if (registrationAttempt.current?.fingerprint !== fingerprint) {
      registrationAttempt.current = {
        fingerprint,
        idempotencyKey: globalThis.crypto.randomUUID(),
      }
    }
    try {
      await createRegistration.mutateAsync({
        idempotencyKey: registrationAttempt.current.idempotencyKey,
        input: {
          username: result.data.username,
          display_name: result.data.displayName,
          email: result.data.email,
          password: result.data.password,
          ...(result.data.invitationToken
            ? { invitation_token: result.data.invitationToken }
            : {}),
          ...(humanVerificationEnabled
            ? { human_verification_token: humanVerificationToken }
            : {}),
        },
      })
    } catch {
      if (humanVerificationEnabled) {
        setHumanVerificationToken("")
        setHumanVerificationResetKey((current) => current + 1)
      }
      // An unchanged retry resumes the same server-side registration. Editing
      // any credential or identity field starts a new idempotent attempt.
    }
  }

  return (
    <AuthEntryCard aria-labelledby="registration-title">
      <CardHeader className="gap-2 px-6">
        <CardTitle>
          <h1
            id="registration-title"
            className="text-2xl leading-none font-semibold tracking-tight"
          >
            注册
          </h1>
        </CardTitle>
      </CardHeader>

      <CardContent className="px-6">
        <form id="registration-form" onSubmit={handleSubmit} noValidate>
          <FieldGroup className="gap-4">
            {mode === "invite" || invitationToken ? (
              <Alert>
                <CircleCheckIcon />
                <AlertTitle>
                  {mode === "invite" ? "邀请凭证已填写" : "已识别邀请链接"}
                </AlertTitle>
                <AlertDescription>
                  {mode === "invite"
                    ? "凭证将在提交注册时由服务端原子校验并消费；若邀请人绑定了邮箱，必须填写完全相同的邮箱。"
                    : "当前可直接注册；提交后仍会校验邀请码并记录邀请关系。"}
                </AlertDescription>
              </Alert>
            ) : null}
            {createRegistration.isError ? (
              <Alert variant="destructive">
                <CircleAlertIcon />
                <AlertTitle>
                  {registrationErrorTitle(createRegistration.error)}
                </AlertTitle>
                <AlertDescription>
                  {registrationErrorDescription(createRegistration.error)}
                </AlertDescription>
              </Alert>
            ) : null}

            <RegistrationField
              name="username"
              label="用户名"
              error={errors.username}
            >
              <Input
                id="username"
                name="username"
                autoComplete="username"
                minLength={usernameMinCharacters}
                maxLength={usernameMaxCharacters}
                placeholder={`${usernameMinCharacters}–${usernameMaxCharacters} 位小写字母、数字或 _ -`}
                className="h-10"
                aria-invalid={Boolean(errors.username)}
                disabled={createRegistration.isPending}
              />
              <FieldDescription>
                创建后先使用用户名登录；用户名会统一转为小写。
              </FieldDescription>
            </RegistrationField>

            <RegistrationField
              name="displayName"
              label="显示名称"
              error={errors.displayName}
            >
              <Input
                id="displayName"
                name="displayName"
                autoComplete="name"
                className="h-10"
                aria-invalid={Boolean(errors.displayName)}
                disabled={createRegistration.isPending}
              />
            </RegistrationField>

            <RegistrationField name="email" label="邮箱" error={errors.email}>
              <Input
                id="email"
                name="email"
                type="email"
                autoComplete="email"
                className="h-10"
                aria-invalid={Boolean(errors.email)}
                disabled={createRegistration.isPending}
              />
              {emailDomainMode !== "any" ? (
                <FieldDescription>
                  当前站点启用了邮箱域名准入规则，提交后由服务端校验。
                </FieldDescription>
              ) : null}
            </RegistrationField>

            <FieldGroup className="gap-4">
              <RegistrationField
                name="password"
                label="密码"
                error={errors.password}
              >
                <Input
                  id="password"
                  name="password"
                  type="password"
                  autoComplete="new-password"
                  className="h-10"
                  aria-invalid={Boolean(errors.password)}
                  disabled={createRegistration.isPending}
                />
                <FieldDescription>至少 12 位。</FieldDescription>
              </RegistrationField>
              <RegistrationField
                name="confirmPassword"
                label="确认密码"
                error={errors.confirmPassword}
              >
                <Input
                  id="confirmPassword"
                  name="confirmPassword"
                  type="password"
                  autoComplete="new-password"
                  className="h-10"
                  aria-invalid={Boolean(errors.confirmPassword)}
                  disabled={createRegistration.isPending}
                />
              </RegistrationField>
            </FieldGroup>

            <TurnstileWidget
              enabled={humanVerificationEnabled}
              siteKey={humanVerification.site_key}
              action="registration"
              resetKey={humanVerificationResetKey}
              onTokenChange={setHumanVerificationToken}
            />
          </FieldGroup>
        </form>
      </CardContent>

      <CardFooter className="flex-col gap-4 border-t-0 bg-transparent px-6">
        <Button
          type="submit"
          form="registration-form"
          size="lg"
          className="h-10 w-full"
          disabled={
            createRegistration.isPending ||
            (humanVerificationEnabled && !humanVerificationToken)
          }
        >
          {createRegistration.isPending ? (
            <>
              <Spinner />
              注册中…
            </>
          ) : (
            "注册"
          )}
        </Button>
        <p className="text-sm text-muted-foreground">
          已有账号？ <Link to="/login">登录</Link>
        </p>
        {mode === "invite" ? (
          <Button
            type="button"
            variant="link"
            size="sm"
            className="h-auto p-0 text-muted-foreground"
            onClick={() => {
              createRegistration.reset()
              setErrors({})
              setInvitationStepComplete(false)
            }}
          >
            更换邀请凭证
          </Button>
        ) : null}
      </CardFooter>
    </AuthEntryCard>
  )
}

function ClosedRegistrationState({ siteName }: { siteName: string }) {
  return (
    <AuthEntryCard aria-labelledby="registration-closed-title">
      <CardHeader className="justify-items-center gap-1.5 px-6 pt-2 text-center">
        <div className="mx-auto mb-4 flex size-12 items-center justify-center rounded-full bg-destructive/10 text-destructive">
          <LockIcon className="size-6" />
        </div>
        <CardTitle>
          <h1
            id="registration-closed-title"
            className="text-2xl leading-none font-semibold tracking-tight"
          >
            注册已关闭
          </h1>
        </CardTitle>
        <CardDescription>{siteName} 目前暂不开放注册</CardDescription>
      </CardHeader>
      <CardContent className="px-6">
        <div className="rounded-lg bg-muted p-4 text-center">
          <p className="text-sm text-muted-foreground">
            如希望加入本站，请联系现有成员了解后续开放或邀请安排。
          </p>
        </div>
      </CardContent>
      <CardFooter className="justify-center border-t-0 bg-transparent px-6">
        <p className="text-sm text-muted-foreground">
          已有账号？ <Link to="/login">登录</Link>
        </p>
      </CardFooter>
    </AuthEntryCard>
  )
}

function InvitationRegistrationStep({
  invitationToken,
  onInvitationTokenChange,
  onContinue,
}: {
  invitationToken: string
  onInvitationTokenChange: (value: string) => void
  onContinue: () => void
}) {
  const [error, setError] = React.useState("")

  function handleContinue(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!invitationTokenPattern.test(invitationToken.trim())) {
      setError("请输入有效的邀请码")
      const control = event.currentTarget.elements.namedItem("invitationToken")
      if (control instanceof HTMLElement) control.focus()
      return
    }
    setError("")
    onContinue()
  }

  return (
    <AuthEntryCard aria-labelledby="invitation-registration-title">
      <CardHeader className="justify-items-center gap-1.5 px-6 pt-2 text-center">
        <div className="mx-auto mb-4 flex size-12 items-center justify-center rounded-full bg-primary/10 text-primary">
          <GiftIcon className="size-6" />
        </div>
        <CardTitle>
          <h1
            id="invitation-registration-title"
            className="text-2xl leading-none font-semibold tracking-tight"
          >
            邀请注册
          </h1>
        </CardTitle>
        <CardDescription>PeerGo 采用邀请制，请输入邀请凭证继续</CardDescription>
      </CardHeader>
      <CardContent className="px-6 pt-2">
        <form id="invitation-step-form" onSubmit={handleContinue} noValidate>
          <FieldGroup className="gap-4">
            <Field data-invalid={Boolean(error)}>
              <FieldLabel htmlFor="invitationToken">邀请凭证</FieldLabel>
              <Input
                id="invitationToken"
                name="invitationToken"
                value={invitationToken}
                onChange={(event) => {
                  onInvitationTokenChange(event.currentTarget.value)
                  setError("")
                }}
                autoComplete="off"
                spellCheck={false}
                placeholder="请输入邀请凭证"
                className="h-10"
                aria-invalid={Boolean(error)}
              />
              <FieldDescription>
                邀请凭证由站点或现有用户发放，仅限一次注册。
              </FieldDescription>
              <FieldError errors={error ? [{ message: error }] : []} />
            </Field>
          </FieldGroup>
        </form>
      </CardContent>
      <CardFooter className="-mt-4 flex-col gap-4 border-t-0 bg-transparent px-6 pb-6">
        <Button
          type="submit"
          form="invitation-step-form"
          size="lg"
          className="h-10 w-full"
          disabled={!invitationToken.trim()}
        >
          继续填写注册资料
          <ArrowRightIcon data-icon="inline-end" />
        </Button>
        <p className="text-sm text-muted-foreground">
          已有账号？ <Link to="/login">登录</Link>
        </p>
      </CardFooter>
    </AuthEntryCard>
  )
}

function RegistrationField({
  name,
  label,
  error,
  children,
}: {
  name: RegistrationFormField
  label: string
  error?: string
  children: React.ReactNode
}) {
  return (
    <Field data-invalid={Boolean(error)}>
      <FieldLabel htmlFor={name}>{label}</FieldLabel>
      {children}
      <FieldError errors={error ? [{ message: error }] : []} />
    </Field>
  )
}

function RegistrationFrame({ children }: { children: React.ReactNode }) {
  return (
    <AuthEntryCard>
      <CardHeader>
        <CardTitle className="text-2xl leading-none font-semibold">
          注册
        </CardTitle>
        <CardDescription>创建站点账户</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">{children}</CardContent>
      <CardFooter className="justify-between bg-transparent text-sm">
        <Link to="/login">已有账号</Link>
        <Link to="/">返回内容首页</Link>
      </CardFooter>
    </AuthEntryCard>
  )
}

function focusFirstInvalidField(
  form: HTMLFormElement,
  errors: RegistrationFormErrors
) {
  const order: RegistrationFormField[] = [
    "username",
    "displayName",
    "email",
    "password",
    "confirmPassword",
    "invitationToken",
  ]
  const first = order.find((field) => errors[field])
  const control = first ? form.elements.namedItem(first) : null
  if (control instanceof HTMLElement) {
    control.focus()
  }
}

function registrationErrorTitle(error: Error) {
  return error instanceof ApiProblemError ? error.message : "注册失败"
}

function registrationErrorDescription(error: Error) {
  return requestErrorDescription(
    error,
    "服务暂时不可用，请保留当前页面并稍后重试。"
  )
}

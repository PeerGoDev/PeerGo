import * as React from "react"
import { Link } from "react-router"
import { CircleAlertIcon, MailCheckIcon, SendIcon } from "lucide-react"

import { Alert, AlertDescription, AlertTitle } from "~/components/ui/alert"
import { buttonVariants, Button } from "~/components/ui/button"
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
import { Input } from "~/components/ui/input"
import { Separator } from "~/components/ui/separator"
import { Skeleton } from "~/components/ui/skeleton"
import { Spinner } from "~/components/ui/spinner"
import { useRequestEmailVerification } from "~/features/auth/api/email-verification.mutations"
import { useWebSession } from "~/features/auth/api/session.mutations"
import { AccountSettingsLayout } from "~/features/auth/components/account-settings-layout"
import { EmailStatusCard } from "~/features/auth/components/email-status-card"
import { emailAddressSchema } from "~/features/auth/model/email-address"
import { cn } from "~/lib/utils"
import { ApiProblemError } from "~/shared/api/problem"

export function EmailVerificationPage() {
  const session = useWebSession()
  const requestVerification = useRequestEmailVerification()
  const [emailError, setEmailError] = React.useState<string>()

  if (session.isPending) {
    return (
      <EmailVerificationFrame>
        <div className="flex flex-col gap-3" aria-label="正在检查邮箱验证状态">
          <Skeleton className="h-10 w-full" />
          <Skeleton className="h-10 w-full" />
        </div>
      </EmailVerificationFrame>
    )
  }
  if (session.isError) {
    return (
      <EmailVerificationFrame>
        <Alert variant="destructive">
          <CircleAlertIcon />
          <AlertTitle>暂时无法读取账户状态</AlertTitle>
          <AlertDescription>请刷新页面后重试。</AlertDescription>
        </Alert>
      </EmailVerificationFrame>
    )
  }
  if (!session.data) {
    return (
      <EmailVerificationFrame>
        <Alert>
          <MailCheckIcon />
          <AlertTitle>需要先登录</AlertTitle>
          <AlertDescription>
            请求验证邮件前需要先使用用户名登录。
          </AlertDescription>
        </Alert>
        <Link to="/login" className={cn(buttonVariants(), "w-full")}>
          前往登录
        </Link>
      </EmailVerificationFrame>
    )
  }
  if (session.data.user.email_verified) {
    return (
      <AccountSettingsLayout
        active="email"
        title="邮箱验证"
        description="验证登录邮箱，保障账户恢复与安全通知。"
      >
        <EmailStatusCard verified />
      </AccountSettingsLayout>
    )
  }

  const activeSession = session.data

  async function handleSubmit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    requestVerification.reset()
    const form = event.currentTarget
    const parsed = emailAddressSchema.safeParse(new FormData(form).get("email"))
    if (!parsed.success) {
      setEmailError(parsed.error.issues[0]?.message ?? "请输入有效邮箱")
      const control = form.elements.namedItem("email")
      if (control instanceof HTMLElement) {
        control.focus()
      }
      return
    }
    setEmailError(undefined)
    try {
      await requestVerification.mutateAsync({
        email: parsed.data,
        csrfToken: activeSession.csrf_token,
      })
    } catch {
      // Mutation state owns the stable problem response; the address remains
      // in this form only and is never copied into query or application state.
    }
  }

  return (
    <AccountSettingsLayout
      active="email"
      title="邮箱验证"
      description="验证登录邮箱，保障账户恢复与安全通知。"
    >
      <Card
        className="w-full gap-0 py-0"
        aria-labelledby="email-verification-title"
      >
        <CardHeader className="gap-2 px-6 pt-6 pb-6">
          <CardTitle
            id="email-verification-title"
            className="text-2xl leading-none font-semibold"
          >
            验证邮箱
          </CardTitle>
          <CardDescription>
            重新输入注册邮箱，我们会发送一条 30 分钟内有效的验证链接。
          </CardDescription>
        </CardHeader>
        <CardContent className="px-6 pb-6">
          <form id="email-verification-form" onSubmit={handleSubmit} noValidate>
            <FieldGroup>
              <Alert>
                <MailCheckIcon />
                <AlertTitle>邮箱不会公开显示</AlertTitle>
                <AlertDescription>
                  邮箱仅用于验证账户、登录和找回密码。
                </AlertDescription>
              </Alert>
              {requestVerification.isError ? (
                <Alert variant="destructive">
                  <CircleAlertIcon />
                  <AlertTitle>
                    {verificationErrorTitle(requestVerification.error)}
                  </AlertTitle>
                  <AlertDescription>
                    {verificationErrorDescription(requestVerification.error)}
                  </AlertDescription>
                </Alert>
              ) : null}
              {requestVerification.data ? (
                <Alert>
                  <SendIcon />
                  <AlertTitle>
                    {requestVerification.data.already_verified
                      ? "邮箱已经验证"
                      : "请求已处理"}
                  </AlertTitle>
                  <AlertDescription>
                    {requestVerification.data.already_verified
                      ? "无需再次发送，可以直接使用邮箱登录。"
                      : "如果输入与账户保存的地址一致，验证邮件已经发送。两分钟后可重发。"}
                  </AlertDescription>
                </Alert>
              ) : null}
              <Field data-invalid={Boolean(emailError)}>
                <FieldLabel htmlFor="email">注册邮箱</FieldLabel>
                <Input
                  id="email"
                  name="email"
                  type="email"
                  autoComplete="email"
                  aria-invalid={Boolean(emailError)}
                  disabled={requestVerification.isPending}
                />
                <FieldDescription>
                  为避免泄露账户信息，响应不会说明地址是否匹配。
                </FieldDescription>
                <FieldError
                  errors={emailError ? [{ message: emailError }] : []}
                />
              </Field>
            </FieldGroup>
          </form>
        </CardContent>
        <CardFooter className="flex-col gap-3 bg-transparent px-6 pb-6">
          <Button
            type="submit"
            form="email-verification-form"
            size="lg"
            className="w-full"
            disabled={
              requestVerification.isPending ||
              requestVerification.data?.already_verified
            }
          >
            {requestVerification.isPending ? (
              <>
                <Spinner data-icon="inline-start" />
                正在发送…
              </>
            ) : (
              <>
                <SendIcon data-icon="inline-start" />
                发送验证邮件
              </>
            )}
          </Button>
          <Separator />
          <div className="flex w-full items-center justify-between gap-3 text-sm">
            <Link to="/">稍后处理</Link>
            <Link to="/login">返回登录</Link>
          </div>
        </CardFooter>
      </Card>
    </AccountSettingsLayout>
  )
}

function EmailVerificationFrame({ children }: { children: React.ReactNode }) {
  return (
    <AccountSettingsLayout
      active="email"
      title="邮箱验证"
      description="验证登录邮箱，保障账户恢复与安全通知。"
    >
      <Card className="w-full gap-0 py-0">
        <CardHeader>
          <CardTitle className="text-2xl leading-none font-semibold">
            验证邮箱
          </CardTitle>
          <CardDescription>确认当前账户的登录邮箱。</CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-4">{children}</CardContent>
        <CardFooter className="justify-between bg-transparent text-sm">
          <Link to="/">返回首页</Link>
          <Link to="/account/security">账户安全</Link>
        </CardFooter>
      </Card>
    </AccountSettingsLayout>
  )
}

function verificationErrorTitle(error: Error) {
  return error instanceof ApiProblemError ? error.message : "发送失败"
}

function verificationErrorDescription(error: Error) {
  if (error instanceof ApiProblemError) {
    return error.requestId
      ? `请稍后重试。请求编号：${error.requestId}`
      : "请稍后重试。"
  }
  return "邮件服务暂时不可用，请稍后重试。"
}

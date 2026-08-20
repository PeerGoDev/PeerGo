import * as React from "react"
import { Link } from "react-router"
import {
  CheckCircle2Icon,
  CircleAlertIcon,
  FingerprintIcon,
  KeyRoundIcon,
  LogInIcon,
  ShieldAlertIcon,
} from "lucide-react"
import { z } from "zod"

import { Alert, AlertDescription, AlertTitle } from "~/components/ui/alert"
import { Button, buttonVariants } from "~/components/ui/button"
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
  FieldError,
  FieldGroup,
  FieldLabel,
} from "~/components/ui/field"
import { Input } from "~/components/ui/input"
import { Skeleton } from "~/components/ui/skeleton"
import { Spinner } from "~/components/ui/spinner"
import { useWebSession } from "~/features/auth/api/session.mutations"
import { useCapabilities } from "~/features/authz/api/capabilities.queries"
import {
  enrollStaffCredential,
  type StaffCredentialEnrollment,
} from "~/features/staff/api/staff-session.mutations"
import { WebAuthnClientError } from "~/features/staff/model/webauthn"
import { ApiProblemError } from "~/shared/api/problem"
import { formatDateTime } from "~/shared/formatters/date-time"

const enrollmentSchema = z.object({
  bootstrapToken: z
    .string()
    .regex(/^[A-Za-z0-9_-]{43}$/, "请输入系统生成的 43 位一次性票据"),
  label: z.string().trim().min(1, "请输入凭据名称").max(80, "凭据名称过长"),
})

type EnrollmentField = keyof z.infer<typeof enrollmentSchema>
type EnrollmentErrors = Partial<Record<EnrollmentField, string>>

export function StaffEnrollmentPage() {
  const webSession = useWebSession()
  const capabilities = useCapabilities(webSession.data?.user.id)
  const [errors, setErrors] = React.useState<EnrollmentErrors>({})
  const [pending, setPending] = React.useState(false)
  const [requestError, setRequestError] = React.useState<Error | null>(null)
  const [enrollment, setEnrollment] =
    React.useState<StaffCredentialEnrollment | null>(null)

  if (webSession.isPending || (webSession.data && capabilities.isPending)) {
    return <EnrollmentSkeleton />
  }
  if (webSession.isError || capabilities.isError) {
    return (
      <EnrollmentFrame>
        <Alert variant="destructive">
          <CircleAlertIcon />
          <AlertTitle>登记条件暂时无法确认</AlertTitle>
          <AlertDescription>请刷新页面后重试。</AlertDescription>
        </Alert>
      </EnrollmentFrame>
    )
  }
  if (!webSession.data) {
    return (
      <EnrollmentFrame>
        <Card className="max-w-xl">
          <CardHeader>
            <CardTitle>需要登录</CardTitle>
            <CardDescription>
              一次性票据只能登记到它指定账号的当前 Web 会话。
            </CardDescription>
          </CardHeader>
          <CardFooter>
            <Link to="/login" className={buttonVariants()}>
              <LogInIcon data-icon="inline-start" />
              前往登录
            </Link>
          </CardFooter>
        </Card>
      </EnrollmentFrame>
    )
  }

  const canEnroll = capabilities.data?.items.some(
    (capability) => capability.action === "staff.credential.enroll.self"
  )
  if (!canEnroll) {
    return (
      <EnrollmentFrame>
        <Card className="max-w-xl">
          <CardHeader>
            <CardTitle>当前账号不能登记后台凭据</CardTitle>
            <CardDescription>
              登记入口只对具有有效后台任期的账号开放。
            </CardDescription>
          </CardHeader>
          <CardContent>
            <Alert>
              <ShieldAlertIcon />
              <AlertTitle>缺少凭据登记权限</AlertTitle>
              <AlertDescription>
                一次性票据不会替代账号授权，也不能跨账号使用。
              </AlertDescription>
            </Alert>
          </CardContent>
        </Card>
      </EnrollmentFrame>
    )
  }
  const activeWebSession = webSession.data

  async function handleSubmit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setRequestError(null)
    setEnrollment(null)
    const form = event.currentTarget
    const formData = new FormData(form)
    const result = enrollmentSchema.safeParse({
      bootstrapToken: formData.get("bootstrap-token"),
      label: formData.get("label"),
    })
    if (!result.success) {
      const fieldErrors = result.error.flatten().fieldErrors
      const nextErrors: EnrollmentErrors = {
        bootstrapToken: fieldErrors.bootstrapToken?.[0],
        label: fieldErrors.label?.[0],
      }
      setErrors(nextErrors)
      const firstInvalid: EnrollmentField = nextErrors.bootstrapToken
        ? "bootstrapToken"
        : "label"
      const control = form.elements.namedItem(
        firstInvalid === "bootstrapToken" ? "bootstrap-token" : firstInvalid
      )
      if (control instanceof HTMLElement) {
        control.focus()
      }
      return
    }

    setErrors({})
    setPending(true)
    try {
      const completed = await enrollStaffCredential({
        bootstrapToken: result.data.bootstrapToken,
        label: result.data.label,
        csrfToken: activeWebSession.csrf_token,
      })
      form.reset()
      setEnrollment(completed)
    } catch (error) {
      setRequestError(
        error instanceof Error ? error : new Error("后台凭据登记失败")
      )
    } finally {
      setPending(false)
    }
  }

  return (
    <EnrollmentFrame>
      <header className="flex flex-col gap-1">
        <h1 className="font-heading text-3xl font-bold tracking-tight">
          安全凭据登记
        </h1>
        <p className="text-sm text-muted-foreground">
          为 @{activeWebSession.user.username} 登记后台通行密钥。
        </p>
      </header>

      <Card className="max-w-2xl">
        <CardHeader>
          <CardTitle>使用一次性登记票据</CardTitle>
          <CardDescription>
            票据由站点管理员生成，最长 30 分钟，并且只能使用一次。
          </CardDescription>
        </CardHeader>
        <CardContent>
          <form id="staff-enrollment-form" onSubmit={handleSubmit} noValidate>
            <FieldGroup>
              <Alert>
                <KeyRoundIcon />
                <AlertTitle>票据与浏览器登记缺一不可</AlertTitle>
                <AlertDescription>
                  票据不会代替当前登录与账号权限；完整凭据会加密保存。
                </AlertDescription>
              </Alert>

              {requestError ? (
                <Alert variant="destructive">
                  <CircleAlertIcon />
                  <AlertTitle>{enrollmentErrorTitle(requestError)}</AlertTitle>
                  <AlertDescription>
                    {enrollmentErrorDescription(requestError)}
                  </AlertDescription>
                </Alert>
              ) : null}

              {enrollment ? (
                <Alert>
                  <CheckCircle2Icon />
                  <AlertTitle>{enrollment.label} 已登记</AlertTitle>
                  <AlertDescription>
                    完成时间：{formatDateTime(enrollment.enrolled_at)}
                    。一次性票据已经消费。
                  </AlertDescription>
                </Alert>
              ) : null}

              <Field data-invalid={Boolean(errors.bootstrapToken)}>
                <FieldLabel htmlFor="bootstrap-token">一次性票据</FieldLabel>
                <Input
                  id="bootstrap-token"
                  name="bootstrap-token"
                  type="password"
                  autoComplete="off"
                  spellCheck={false}
                  placeholder="粘贴仅显示一次的登记票据"
                  aria-invalid={Boolean(errors.bootstrapToken)}
                  disabled={pending}
                />
                <FieldError
                  errors={
                    errors.bootstrapToken
                      ? [{ message: errors.bootstrapToken }]
                      : []
                  }
                />
              </Field>

              <Field data-invalid={Boolean(errors.label)}>
                <FieldLabel htmlFor="label">凭据名称</FieldLabel>
                <Input
                  id="label"
                  name="label"
                  autoComplete="off"
                  maxLength={80}
                  defaultValue="当前设备的通行密钥"
                  aria-invalid={Boolean(errors.label)}
                  disabled={pending}
                />
                <FieldError
                  errors={errors.label ? [{ message: errors.label }] : []}
                />
              </Field>
            </FieldGroup>
          </form>
        </CardContent>
        <CardFooter className="flex-wrap justify-between gap-3">
          <p className="text-xs text-muted-foreground">
            页面不会保存这张一次性票据。
          </p>
          {enrollment ? (
            <Link to="/staff" className={buttonVariants()}>
              <FingerprintIcon data-icon="inline-start" />
              前往后台验证
            </Link>
          ) : (
            <Button
              type="submit"
              form="staff-enrollment-form"
              disabled={pending}
            >
              {pending ? (
                <>
                  <Spinner />
                  等待安全设备…
                </>
              ) : (
                <>
                  <FingerprintIcon data-icon="inline-start" />
                  登记安全凭据
                </>
              )}
            </Button>
          )}
        </CardFooter>
      </Card>
    </EnrollmentFrame>
  )
}

function EnrollmentFrame({ children }: { children: React.ReactNode }) {
  return (
    <main className="mx-auto flex w-full max-w-[1248px] flex-1 flex-col justify-center gap-6 p-4 lg:p-6">
      {children}
    </main>
  )
}

function EnrollmentSkeleton() {
  return (
    <EnrollmentFrame>
      <Card
        className="max-w-2xl"
        aria-label="正在检查登记条件"
        aria-busy="true"
      >
        <CardHeader>
          <CardTitle>
            <Skeleton className="h-6 w-44" />
          </CardTitle>
          <CardDescription>
            <Skeleton className="h-4 w-80 max-w-full" />
          </CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-4">
          <Skeleton className="h-20 w-full" />
          <Skeleton className="h-10 w-full" />
          <Skeleton className="h-10 w-full" />
        </CardContent>
      </Card>
    </EnrollmentFrame>
  )
}

function enrollmentErrorTitle(error: Error) {
  if (error instanceof WebAuthnClientError) {
    return "浏览器登记未完成"
  }
  if (error instanceof ApiProblemError) {
    return error.message
  }
  return "后台凭据登记失败"
}

function enrollmentErrorDescription(error: Error) {
  if (error instanceof WebAuthnClientError) {
    return error.message
  }
  if (error instanceof ApiProblemError) {
    return error.requestId
      ? `请确认票据属于当前账号且仍然有效。请求编号：${error.requestId}`
      : "请确认票据属于当前账号、尚未消费且仍在有效期内。"
  }
  return "服务暂时不可用，请稍后重试。"
}

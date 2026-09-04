import * as React from "react"
import { Link } from "react-router"
import {
  CircleAlertIcon,
  CircleCheckIcon,
  Clock3Icon,
  SearchIcon,
  SendIcon,
  ShieldAlertIcon,
} from "lucide-react"
import { z } from "zod"

import { Alert, AlertDescription, AlertTitle } from "~/components/ui/alert"
import { Badge } from "~/components/ui/badge"
import { Button } from "~/components/ui/button"
import {
  CardContent,
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
import { Textarea } from "~/components/ui/textarea"
import {
  type AccountAccessCredentialProof,
  type AccountAccessStatus,
  inspectAccountAccess,
  submitAccountAccessAppeal,
} from "~/features/auth/api/account-access"
import { AuthEntryCard } from "~/features/auth/components/auth-entry-card"
import { ApiProblemError, requestErrorDescription } from "~/shared/api/problem"

const proofSchema = z.object({
  identifier: z.string().trim().min(1, "请输入用户名或邮箱").max(254),
  password: z.string().min(1, "请输入密码").max(1024),
  secondFactorCode: z.string().max(32),
})

export function AccountAccessPage() {
  const formRef = React.useRef<HTMLFormElement>(null)
  const [status, setStatus] = React.useState<AccountAccessStatus>()
  const [statement, setStatement] = React.useState("")
  const [error, setError] = React.useState<Error>()
  const [pending, setPending] = React.useState<"inspect" | "submit">()
  const [idempotencyKey, setIdempotencyKey] = React.useState(() =>
    crypto.randomUUID()
  )
  const statementLength = Array.from(statement.trim()).length

  async function handleInspect(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    let credentials: AccountAccessCredentialProof
    try {
      credentials = credentialsFromForm(event.currentTarget)
    } catch (caught) {
      setError(asError(caught))
      return
    }
    setError(undefined)
    setPending("inspect")
    try {
      setStatus(await inspectAccountAccess(credentials))
    } catch (caught) {
      setStatus(undefined)
      setError(asError(caught))
    } finally {
      setPending(undefined)
    }
  }

  async function handleAppeal() {
    const form = formRef.current
    if (!form || statementLength < 20 || statementLength > 1000) return
    let credentials: AccountAccessCredentialProof
    try {
      credentials = credentialsFromForm(form)
    } catch (caught) {
      setError(asError(caught))
      return
    }
    setError(undefined)
    setPending("submit")
    try {
      const appeal = await submitAccountAccessAppeal({
        credentials,
        statement: statement.trim(),
        idempotencyKey,
      })
      setStatus((current) =>
        current ? { ...current, appeal, can_appeal: false } : current
      )
      setIdempotencyKey(crypto.randomUUID())
      setStatement("")
      clearSecretFields(form)
    } catch (caught) {
      setError(asError(caught))
    } finally {
      setPending(undefined)
    }
  }

  return (
    <AuthEntryCard
      viewport="shell"
      className="max-w-xl"
      aria-labelledby="restriction-title"
    >
      <CardHeader className="gap-2 px-6">
        <CardTitle>
          <h1
            id="restriction-title"
            className="text-[22px] leading-none font-semibold tracking-tight"
          >
            封禁记录与申诉
          </h1>
        </CardTitle>
        <p className="text-sm leading-6 text-muted-foreground">
          使用本人账号查询当前访问限制。这里不会登录账号，也不会保存密码。
        </p>
      </CardHeader>
      <CardContent className="px-6">
        <form ref={formRef} onSubmit={handleInspect} noValidate>
          <FieldGroup>
            {error ? (
              <Alert variant="destructive">
                <CircleAlertIcon />
                <AlertTitle>{error.message}</AlertTitle>
                <AlertDescription>
                  {requestErrorDescription(error)}
                </AlertDescription>
              </Alert>
            ) : null}
            <Field>
              <FieldLabel htmlFor="restriction-identifier">
                用户名 / 邮箱
              </FieldLabel>
              <Input
                id="restriction-identifier"
                name="identifier"
                autoComplete="username"
                maxLength={254}
                disabled={Boolean(pending)}
              />
            </Field>
            <Field>
              <FieldLabel htmlFor="restriction-password">密码</FieldLabel>
              <Input
                id="restriction-password"
                name="password"
                type="password"
                autoComplete="current-password"
                maxLength={1024}
                disabled={Boolean(pending)}
              />
            </Field>
            <Field>
              <FieldLabel htmlFor="restriction-second-factor">
                两步验证码（可选）
              </FieldLabel>
              <Input
                id="restriction-second-factor"
                name="second-factor-code"
                autoComplete="one-time-code"
                maxLength={32}
                placeholder="启用两步验证时填写"
                disabled={Boolean(pending)}
              />
            </Field>
            <Button type="submit" disabled={Boolean(pending)}>
              {pending === "inspect" ? <Spinner /> : <SearchIcon />}
              {pending === "inspect" ? "正在查询…" : "查询本人限制"}
            </Button>
          </FieldGroup>
        </form>

        {status ? (
          <div className="mt-5 border-t pt-5">
            {!status.restricted ? (
              <div className="space-y-4">
                <Alert>
                  <CircleCheckIcon />
                  <AlertTitle>当前没有账户访问限制</AlertTitle>
                  <AlertDescription>
                    该账号可以正常登录；下载受限、分享率考核与 H&amp;R
                    记录请登录后分别查看。
                  </AlertDescription>
                </Alert>
                {status.appeal ? <AppealRecord appeal={status.appeal} /> : null}
              </div>
            ) : status.restriction ? (
              <RestrictionResult status={status} />
            ) : null}

            {status.can_appeal ? (
              <Field
                className="mt-5"
                data-invalid={
                  statementLength > 0 && statementLength < 20 ? true : undefined
                }
              >
                <FieldLabel htmlFor="account-access-statement">
                  申诉说明
                </FieldLabel>
                <Textarea
                  id="account-access-statement"
                  value={statement}
                  maxLength={1000}
                  rows={6}
                  placeholder="说明情况、希望复核的内容和必要依据（至少 20 个字符）"
                  onChange={(event) => {
                    setStatement(event.target.value)
                    setError(undefined)
                  }}
                />
                <FieldDescription>
                  {statementLength}/1000 个字符
                </FieldDescription>
                {statementLength > 0 && statementLength < 20 ? (
                  <FieldError>至少需要 20 个字符</FieldError>
                ) : null}
                <Button
                  type="button"
                  onClick={() => void handleAppeal()}
                  disabled={
                    Boolean(pending) ||
                    statementLength < 20 ||
                    statementLength > 1000
                  }
                >
                  {pending === "submit" ? <Spinner /> : <SendIcon />}
                  {pending === "submit" ? "正在提交…" : "提交本次申诉"}
                </Button>
              </Field>
            ) : null}
          </div>
        ) : null}
      </CardContent>
      <CardFooter className="justify-center border-t px-6 text-sm">
        <Link to="/login">返回登录</Link>
      </CardFooter>
    </AuthEntryCard>
  )
}

function RestrictionResult({ status }: { status: AccountAccessStatus }) {
  const restriction = status.restriction
  if (!restriction) return null
  return (
    <div className="space-y-4">
      <Alert variant="destructive">
        <ShieldAlertIcon />
        <AlertTitle>账户访问当前受限</AlertTitle>
        <AlertDescription>{restriction.reason_summary}</AlertDescription>
      </Alert>
      <dl className="grid gap-3 rounded-md border bg-muted/20 p-4 text-sm sm:grid-cols-2">
        <div>
          <dt className="text-muted-foreground">限制类型</dt>
          <dd className="mt-1 font-medium">
            {restriction.source_kind === "disabled_account"
              ? "账户已封禁"
              : "临时访问限制"}
          </dd>
        </div>
        <div>
          <dt className="text-muted-foreground">开始时间</dt>
          <dd className="mt-1">{formatDateTime(restriction.starts_at)}</dd>
        </div>
        <div className="sm:col-span-2">
          <dt className="text-muted-foreground">结束时间</dt>
          <dd className="mt-1">
            {restriction.expires_at
              ? formatDateTime(restriction.expires_at)
              : "未设置自动结束时间"}
          </dd>
        </div>
      </dl>
      {status.appeal ? <AppealRecord appeal={status.appeal} /> : null}
    </div>
  )
}

function AppealRecord({
  appeal,
}: {
  appeal: NonNullable<AccountAccessStatus["appeal"]>
}) {
  return (
    <div className="rounded-md border p-4">
      <div className="flex flex-wrap items-center gap-2">
        <Clock3Icon className="size-4" />
        <span className="font-medium">已有申诉</span>
        <AppealStatusBadge status={appeal.status} />
      </div>
      <p className="mt-3 text-sm leading-6 whitespace-pre-wrap">
        {appeal.statement}
      </p>
      {appeal.response ? (
        <div className="mt-3 border-t pt-3 text-sm">
          <div className="text-muted-foreground">处理意见</div>
          <p className="mt-1 whitespace-pre-wrap">{appeal.response}</p>
        </div>
      ) : null}
    </div>
  )
}

function AppealStatusBadge({ status }: { status: string }) {
  const labels: Record<string, string> = {
    pending: "等待处理",
    approved: "已批准",
    rejected: "已驳回",
    source_resolved: "限制已另行解除",
  }
  return <Badge variant="outline">{labels[status] ?? status}</Badge>
}

function credentialsFromForm(
  form: HTMLFormElement
): AccountAccessCredentialProof {
  const data = new FormData(form)
  const parsed = proofSchema.safeParse({
    identifier: data.get("identifier"),
    password: data.get("password"),
    secondFactorCode: data.get("second-factor-code") ?? "",
  })
  if (!parsed.success) {
    const first = parsed.error.issues[0]
    form.querySelector<HTMLInputElement>("input:invalid")?.focus()
    throw new ApiProblemError(400, {
      title: "查询信息无效",
      status: 400,
      code: "invalid_account_access_proof",
      detail: first?.message ?? "请检查查询信息。",
    })
  }
  return {
    identifier: parsed.data.identifier,
    password: parsed.data.password,
    ...(parsed.data.secondFactorCode
      ? { second_factor_code: parsed.data.secondFactorCode }
      : {}),
  }
}

function clearSecretFields(form: HTMLFormElement) {
  for (const name of ["password", "second-factor-code"]) {
    const field = form.elements.namedItem(name)
    if (field instanceof HTMLInputElement) field.value = ""
  }
}

function asError(value: unknown) {
  return value instanceof Error ? value : new Error("请求失败")
}

function formatDateTime(value: string) {
  return new Intl.DateTimeFormat("zh-CN", {
    dateStyle: "medium",
    timeStyle: "short",
  }).format(new Date(value))
}

import * as React from "react"
import { useQuery } from "@tanstack/react-query"
import {
  CircleAlertIcon,
  CircleCheckIcon,
  Clock3Icon,
  FileClockIcon,
  MailCheckIcon,
  MailIcon,
  RefreshCwIcon,
  RotateCcwKeyIcon,
  SendIcon,
  ShieldCheckIcon,
} from "lucide-react"

import { Alert, AlertDescription, AlertTitle } from "~/components/ui/alert"
import { Badge } from "~/components/ui/badge"
import { Button } from "~/components/ui/button"
import {
  Card,
  CardContent,
  CardDescription,
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
import { Skeleton } from "~/components/ui/skeleton"
import { Spinner } from "~/components/ui/spinner"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "~/components/ui/table"
import {
  emailSettingsQueryOptions,
  useTestEmailDelivery,
} from "~/features/staff/api/operations.queries"
import { StaffAccessGate } from "~/features/staff/components/staff-access-gate"
import { StaffPageFrame } from "~/features/staff/components/staff-page-frame"
import { StaffSettingsValueTable } from "~/features/staff/components/staff-settings-value-table"
import { hasCapability } from "~/features/staff/model/capability"
import type { components } from "~/generated/api"
import { MetricCard } from "~/shared/components/metric-card"
import { requestErrorDescription } from "~/shared/api/problem"
import { formatInteger } from "~/shared/formatters/integer"
import { isSecurePublicOrigin } from "~/shared/validation/public-origin"

type CapabilityList = components["schemas"]["CapabilityList"]

export function StaffEmailSettingsPage() {
  return (
    <StaffAccessGate
      requiredAction="operations.monitor.read"
      pageHeader={{
        title: "邮件设置",
        description: "查看邮件投递方式、公开链接来源和发送状态。",
      }}
    >
      {({ session, capabilities }) => (
        <EmailSettingsContent
          csrfToken={session.csrf_token}
          capabilities={capabilities}
        />
      )}
    </StaffAccessGate>
  )
}

function EmailSettingsContent({
  csrfToken,
  capabilities,
}: {
  csrfToken: string
  capabilities: CapabilityList
}) {
  const settings = useQuery(emailSettingsQueryOptions())
  if (settings.isPending) return <EmailSettingsSkeleton />
  if (settings.isError || !settings.data) {
    return (
      <StaffPageFrame className="gap-4">
        <Alert variant="destructive">
          <CircleAlertIcon />
          <AlertTitle>邮件设置暂时无法读取</AlertTitle>
          <AlertDescription>
            Privacy Vault 可能尚未重启，或邮件状态接口暂时不可用。
          </AlertDescription>
        </Alert>
        <Button
          variant="outline"
          className="w-fit"
          onClick={() => void settings.refetch()}
        >
          <RefreshCwIcon data-icon="inline-start" />
          重试
        </Button>
      </StaffPageFrame>
    )
  }

  const { stats } = settings.data
  const failedCount =
    BigInt(stats.verification_failed) + BigInt(stats.recovery_failed)
  const securePublicOrigins =
    isSecurePublicOrigin(settings.data.verification_public_origin) &&
    isSecurePublicOrigin(settings.data.password_recovery_public_origin)

  return (
    <StaffPageFrame className="gap-4">
      <header className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h1 className="font-heading text-xl font-semibold">邮件设置</h1>
          <p className="mt-1 text-sm text-muted-foreground">
            邮件地址和一次性链接只在 Privacy Vault
            中处理；后台不读取收件人或投递凭据。
          </p>
        </div>
        <Button
          variant="outline"
          size="sm"
          disabled={settings.isFetching}
          onClick={() => void settings.refetch()}
        >
          <RefreshCwIcon
            data-icon="inline-start"
            className={settings.isFetching ? "animate-spin" : undefined}
          />
          刷新
        </Button>
      </header>

      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
        <MetricCard
          title="投递方式"
          value={
            settings.data.delivery_mode === "development_outbox"
              ? "本地信箱"
              : "HTTPS Relay"
          }
          description="由 Privacy Vault 启动配置决定"
          icon={<MailIcon />}
          tone="primary"
        />
        <MetricCard
          title="验证邮件已发送"
          value={formatInteger(stats.verification_sent)}
          description={`已完成验证 ${formatInteger(stats.verification_verified)}`}
          icon={<MailCheckIcon />}
          tone="positive"
        />
        <MetricCard
          title="找回邮件已发送"
          value={formatInteger(stats.recovery_sent)}
          description={`已完成重置 ${formatInteger(stats.recovery_completed)}`}
          icon={<RotateCcwKeyIcon />}
          tone="default"
        />
        <MetricCard
          title="投递失败"
          value={formatInteger(failedCount)}
          description="验证与密码找回合计"
          icon={<CircleAlertIcon />}
          tone={failedCount > 0n ? "warning" : "muted"}
        />
      </div>

      {settings.data.delivery_mode === "development_outbox" ? (
        <Alert>
          <FileClockIcon />
          <AlertTitle>开发环境使用本地信箱</AlertTitle>
          <AlertDescription>
            当前邮件会写入仅限本机的私有开发信箱，便于本地验证流程；正式环境必须切换为
            HTTPS Relay。
          </AlertDescription>
        </Alert>
      ) : !securePublicOrigins ? (
        <Alert variant="destructive">
          <CircleAlertIcon />
          <AlertTitle>公开操作链接尚未使用 HTTPS</AlertTitle>
          <AlertDescription>
            Relay 已接入，但邮箱验证或密码找回来源仍不符合生产要求；修正 Vault
            部署配置并重启后再进行测试投递。
          </AlertDescription>
        </Alert>
      ) : (
        <Alert>
          <ShieldCheckIcon />
          <AlertTitle>正式环境使用 HTTPS Relay</AlertTitle>
          <AlertDescription>
            Relay 地址和服务令牌只存在于 Privacy Vault
            进程配置，不通过管理后台返回。
          </AlertDescription>
        </Alert>
      )}

      <EmailDeliveryTestCard
        csrfToken={csrfToken}
        canSend={hasCapability(capabilities, "operations.email.test")}
        deliveryMode={settings.data.delivery_mode}
      />

      <div className="grid gap-4 xl:grid-cols-2">
        <Card className="gap-0 py-0">
          <CardHeader className="p-6 pb-4">
            <CardTitle>公开链接与时效</CardTitle>
            <CardDescription>
              用户点击链接时可见的来源和服务端有效期
            </CardDescription>
          </CardHeader>
          <CardContent className="p-0">
            <StaffSettingsValueTable
              rows={[
                {
                  label: "邮箱验证来源",
                  value: settings.data.verification_public_origin,
                },
                {
                  label: "密码找回来源",
                  value: settings.data.password_recovery_public_origin,
                },
                {
                  label: "验证链接有效期",
                  value: formatDuration(settings.data.verification_ttl_seconds),
                },
                {
                  label: "找回链接有效期",
                  value: formatDuration(
                    settings.data.password_recovery_ttl_seconds
                  ),
                },
                {
                  label: "重复发送冷却",
                  value: formatDuration(settings.data.cooldown_seconds),
                },
              ]}
            />
          </CardContent>
        </Card>

        <Card className="gap-0 py-0">
          <CardHeader className="p-6 pb-4">
            <CardTitle>投递状态</CardTitle>
            <CardDescription>
              只统计状态，不返回邮箱地址或一次性链接
            </CardDescription>
          </CardHeader>
          <CardContent className="p-0">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>邮件类型</TableHead>
                  <TableHead className="text-right">待发送</TableHead>
                  <TableHead className="text-right">已发送</TableHead>
                  <TableHead className="text-right">失败</TableHead>
                  <TableHead className="text-right">已完成</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                <TableRow>
                  <TableCell className="font-medium">邮箱验证</TableCell>
                  <TableCell className="text-right tabular-nums">
                    {formatInteger(stats.verification_pending)}
                  </TableCell>
                  <TableCell className="text-right tabular-nums">
                    {formatInteger(stats.verification_sent)}
                  </TableCell>
                  <TableCell className="text-right tabular-nums">
                    {formatInteger(stats.verification_failed)}
                  </TableCell>
                  <TableCell className="text-right tabular-nums">
                    {formatInteger(stats.verification_verified)}
                  </TableCell>
                </TableRow>
                <TableRow>
                  <TableCell className="font-medium">密码找回</TableCell>
                  <TableCell className="text-right tabular-nums">
                    {formatInteger(stats.recovery_pending)}
                  </TableCell>
                  <TableCell className="text-right tabular-nums">
                    {formatInteger(stats.recovery_sent)}
                  </TableCell>
                  <TableCell className="text-right tabular-nums">
                    {formatInteger(stats.recovery_failed)}
                  </TableCell>
                  <TableCell className="text-right tabular-nums">
                    {formatInteger(stats.recovery_completed)}
                  </TableCell>
                </TableRow>
              </TableBody>
            </Table>
          </CardContent>
        </Card>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>已启用模板</CardTitle>
          <CardDescription>
            验证与找回共用同一个受限投递适配器，避免重复实现 SMTP 或 Relay
            客户端。
          </CardDescription>
        </CardHeader>
        <CardContent className="flex flex-wrap gap-2">
          {settings.data.templates.map((template) => (
            <Badge key={template} variant="outline">
              <Clock3Icon data-icon="inline-start" />
              {templateLabel(template)}
            </Badge>
          ))}
        </CardContent>
      </Card>
    </StaffPageFrame>
  )
}

function formatDuration(seconds: number) {
  if (seconds % 60 === 0) return `${seconds / 60} 分钟`
  return `${seconds} 秒`
}

function templateLabel(template: string) {
  switch (template) {
    case "peergo-email-verification-v1":
      return "邮箱验证"
    case "peergo-password-recovery-v1":
      return "密码找回"
    default:
      return "未知模板"
  }
}

function EmailDeliveryTestCard({
  csrfToken,
  canSend,
  deliveryMode,
}: {
  csrfToken: string
  canSend: boolean
  deliveryMode: "development_outbox" | "https_relay"
}) {
  const mutation = useTestEmailDelivery()
  const [recipient, setRecipient] = React.useState("")
  const [fieldError, setFieldError] = React.useState("")

  function handleSubmit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    mutation.reset()
    const value = recipient.trim()
    if (!/^\S+@\S+\.\S+$/.test(value) || value.length > 254) {
      setFieldError("请输入一个完整的邮箱地址。")
      return
    }
    setFieldError("")
    mutation.mutate({ csrfToken, body: { recipient: value } })
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>测试邮件投递</CardTitle>
        <CardDescription>
          使用与邮箱验证、密码找回相同的投递链路发送一封无链接测试邮件。
        </CardDescription>
      </CardHeader>
      <CardContent>
        <form onSubmit={handleSubmit} noValidate>
          <FieldGroup>
            <Field data-invalid={Boolean(fieldError)} data-disabled={!canSend}>
              <FieldLabel htmlFor="email-delivery-test-recipient">
                测试收件地址
              </FieldLabel>
              <div className="flex flex-col gap-2 sm:flex-row">
                <Input
                  id="email-delivery-test-recipient"
                  name="recipient"
                  type="email"
                  inputMode="email"
                  autoComplete="email"
                  placeholder="admin@example.com"
                  value={recipient}
                  maxLength={254}
                  disabled={!canSend || mutation.isPending}
                  aria-invalid={Boolean(fieldError)}
                  onChange={(event) => {
                    setRecipient(event.target.value)
                    setFieldError("")
                    mutation.reset()
                  }}
                />
                <Button
                  type="submit"
                  className="sm:w-32"
                  disabled={!canSend || mutation.isPending}
                >
                  {mutation.isPending ? (
                    <Spinner data-icon="inline-start" />
                  ) : (
                    <SendIcon data-icon="inline-start" />
                  )}
                  {mutation.isPending ? "发送中…" : "发送测试"}
                </Button>
              </div>
              <FieldDescription>
                {deliveryMode === "development_outbox"
                  ? "开发环境会写入本机私有信箱，不会连接真实 SMTP。"
                  : "地址只用于本次发送，不会保存到 Core 数据库或显示在投递状态中。"}
              </FieldDescription>
              <FieldError
                errors={fieldError ? [{ message: fieldError }] : []}
              />
            </Field>

            {!canSend ? (
              <Alert>
                <ShieldCheckIcon />
                <AlertTitle>当前权限仅可查看</AlertTitle>
                <AlertDescription>
                  发送测试邮件需要单独的邮件测试权限。
                </AlertDescription>
              </Alert>
            ) : null}
            {mutation.isSuccess ? (
              <Alert>
                <CircleCheckIcon />
                <AlertTitle>测试邮件已接受</AlertTitle>
                <AlertDescription>
                  投递服务已于
                  {new Date(mutation.data.accepted_at).toLocaleString("zh-CN")}
                  接受请求，请检查收件箱和垃圾邮件目录。
                </AlertDescription>
              </Alert>
            ) : null}
            {mutation.isError ? (
              <Alert variant="destructive">
                <CircleAlertIcon />
                <AlertTitle>测试邮件发送失败</AlertTitle>
                <AlertDescription>
                  {requestErrorDescription(mutation.error)}
                </AlertDescription>
              </Alert>
            ) : null}
          </FieldGroup>
        </form>
      </CardContent>
    </Card>
  )
}

function EmailSettingsSkeleton() {
  return (
    <StaffPageFrame aria-label="正在加载邮件设置">
      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
        {Array.from({ length: 4 }, (_, index) => (
          <Skeleton key={index} className="h-36 rounded-lg" />
        ))}
      </div>
      <Skeleton className="h-20 rounded-lg" />
      <div className="grid gap-4 xl:grid-cols-2">
        <Skeleton className="h-80 rounded-lg" />
        <Skeleton className="h-80 rounded-lg" />
      </div>
    </StaffPageFrame>
  )
}

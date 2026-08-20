import * as React from "react"
import { useQuery } from "@tanstack/react-query"
import {
  CircleAlertIcon,
  CircleCheckIcon,
  RefreshCwIcon,
  RssIcon,
  SaveIcon,
  ShieldCheckIcon,
} from "lucide-react"

import { Alert, AlertDescription, AlertTitle } from "~/components/ui/alert"
import { Button } from "~/components/ui/button"
import {
  Card,
  CardAction,
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
import { Switch } from "~/components/ui/switch"
import { Textarea } from "~/components/ui/textarea"
import {
  rssSettingsQueryOptions,
  useUpdateRSSSettings,
} from "~/features/staff/api/rss-settings.queries"
import { StaffAccessGate } from "~/features/staff/components/staff-access-gate"
import { StaffPageFrame } from "~/features/staff/components/staff-page-frame"
import { hasCapability } from "~/features/staff/model/capability"
import type { components } from "~/generated/api"
import { requestErrorDescription } from "~/shared/api/problem"
import { formatDateTime } from "~/shared/formatters/date-time"

type CapabilityList = components["schemas"]["CapabilityList"]

export function StaffRSSSettingsPage() {
  return (
    <StaffAccessGate
      requiredAction="rss.settings.manage.read"
      pageHeader={{
        title: "RSS 设置",
        description: "管理私密订阅的容量、缓存与请求频率。",
      }}
    >
      {({ session, capabilities }) => (
        <RSSSettingsContent
          csrfToken={session.csrf_token}
          capabilities={capabilities}
        />
      )}
    </StaffAccessGate>
  )
}

function RSSSettingsContent({
  csrfToken,
  capabilities,
}: {
  csrfToken: string
  capabilities: CapabilityList
}) {
  const settings = useQuery(rssSettingsQueryOptions)
  const canUpdate = hasCapability(capabilities, "rss.settings.update")
  if (settings.isPending) {
    return (
      <StaffPageFrame className="gap-4">
        <Skeleton className="h-96" />
      </StaffPageFrame>
    )
  }
  if (settings.isError || !settings.data) {
    return (
      <StaffPageFrame className="gap-4">
        <Alert variant="destructive">
          <CircleAlertIcon />
          <AlertTitle>RSS 设置暂时无法读取</AlertTitle>
          <AlertDescription>
            {requestErrorDescription(settings.error)}
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
  return (
    <RSSSettingsForm
      key={settings.data.version}
      initial={settings.data}
      csrfToken={csrfToken}
      canUpdate={canUpdate}
    />
  )
}

function RSSSettingsForm({
  initial,
  csrfToken,
  canUpdate,
}: {
  initial: components["schemas"]["RSSSettings"]
  csrfToken: string
  canUpdate: boolean
}) {
  const update = useUpdateRSSSettings()
  const [enabled, setEnabled] = React.useState(initial.enabled)
  const [cacheTTL, setCacheTTL] = React.useState(initial.cache_ttl_seconds)
  const [maxItems, setMaxItems] = React.useState(initial.max_items_per_feed)
  const [maxSubscriptions, setMaxSubscriptions] = React.useState(
    initial.max_subscriptions_per_user
  )
  const [rate, setRate] = React.useState(initial.requests_per_minute)
  const [reason, setReason] = React.useState("")
  const [success, setSuccess] = React.useState("")
  const reasonValid = Array.from(reason.trim()).length >= 10
  const changed =
    enabled !== initial.enabled ||
    cacheTTL !== initial.cache_ttl_seconds ||
    maxItems !== initial.max_items_per_feed ||
    maxSubscriptions !== initial.max_subscriptions_per_user ||
    rate !== initial.requests_per_minute

  async function submit(event: React.FormEvent) {
    event.preventDefault()
    if (!changed || !reasonValid) return
    const result = await update.mutateAsync({
      csrfToken,
      body: {
        enabled,
        cache_ttl_seconds: cacheTTL,
        max_items_per_feed: maxItems,
        max_subscriptions_per_user: maxSubscriptions,
        requests_per_minute: rate,
        expected_version: initial.version,
        reason: reason.trim(),
      },
    })
    setReason("")
    setSuccess(`RSS 设置已生效，当前为第 ${result.version} 版。`)
  }

  return (
    <StaffPageFrame className="gap-4">
      <header>
        <h1 className="font-heading text-xl font-semibold">RSS 设置</h1>
        <p className="mt-1 text-sm text-muted-foreground">
          管理私密订阅的容量、缓存与请求频率。
        </p>
      </header>
      {success ? (
        <Alert>
          <CircleCheckIcon />
          <AlertTitle>RSS 设置已更新</AlertTitle>
          <AlertDescription>{success}</AlertDescription>
        </Alert>
      ) : null}
      {update.isError ? (
        <Alert variant="destructive">
          <CircleAlertIcon />
          <AlertTitle>RSS 设置未能保存</AlertTitle>
          <AlertDescription>
            {requestErrorDescription(update.error)}
          </AlertDescription>
        </Alert>
      ) : null}
      {!canUpdate ? (
        <Alert>
          <ShieldCheckIcon />
          <AlertTitle>当前权限仅可查看</AlertTitle>
          <AlertDescription>可以查看实际生效值，但不能修改。</AlertDescription>
        </Alert>
      ) : null}

      <Card className="max-w-3xl gap-0 py-0">
        <CardHeader className="p-6 pb-4">
          <CardTitle className="flex items-center gap-2">
            <RssIcon className="size-4" />
            RSS 订阅策略
          </CardTitle>
          <CardDescription>
            缓存保存的是不含令牌的安全种子投影；到达优惠或置顶切换时刻会提前失效。
          </CardDescription>
          <CardAction className="text-xs text-muted-foreground">
            第 {initial.version} 版 · {formatDateTime(initial.updated_at)}
          </CardAction>
        </CardHeader>
        <CardContent className="border-t p-6">
          <form onSubmit={(event) => void submit(event)}>
            <FieldGroup>
              <Field orientation="horizontal" className="rounded-lg border p-4">
                <div className="flex flex-1 flex-col gap-1">
                  <FieldLabel htmlFor="rss-enabled">启用 RSS</FieldLabel>
                  <FieldDescription>
                    关闭后所有私密 RSS 地址立即停止读取和下载。
                  </FieldDescription>
                </div>
                <Switch
                  id="rss-enabled"
                  checked={enabled}
                  onCheckedChange={setEnabled}
                  disabled={!canUpdate || update.isPending}
                />
              </Field>

              <div className="grid gap-4 sm:grid-cols-2">
                <NumberField
                  id="rss-cache-ttl"
                  label="缓存时长（秒）"
                  description="60–900；若优惠更早切换，则提前失效。"
                  value={cacheTTL}
                  min={60}
                  max={900}
                  disabled={!canUpdate || update.isPending}
                  onChange={setCacheTTL}
                />
                <NumberField
                  id="rss-max-items"
                  label="每个订阅最多条目"
                  description="1–50；用户设置不能超过此值。"
                  value={maxItems}
                  min={1}
                  max={50}
                  disabled={!canUpdate || update.isPending}
                  onChange={setMaxItems}
                />
                <NumberField
                  id="rss-max-feeds"
                  label="每位用户最多订阅"
                  description="1–20 个独立私密地址。"
                  value={maxSubscriptions}
                  min={1}
                  max={20}
                  disabled={!canUpdate || update.isPending}
                  onChange={setMaxSubscriptions}
                />
                <NumberField
                  id="rss-rate"
                  label="每用户每分钟请求"
                  description="1–120；同一用户的所有订阅共享额度。"
                  value={rate}
                  min={1}
                  max={120}
                  disabled={!canUpdate || update.isPending}
                  onChange={setRate}
                />
              </div>

              <Field data-invalid={Boolean(reason && !reasonValid)}>
                <FieldLabel htmlFor="rss-change-reason">变更理由</FieldLabel>
                <Textarea
                  id="rss-change-reason"
                  value={reason}
                  maxLength={500}
                  rows={3}
                  disabled={!canUpdate || update.isPending}
                  onChange={(event) => setReason(event.target.value)}
                  placeholder="至少 10 个字符，将写入后台审计记录"
                />
                <FieldError>
                  {reason && !reasonValid
                    ? "请填写至少 10 个字符的变更理由"
                    : undefined}
                </FieldError>
              </Field>

              <div className="flex justify-end">
                <Button
                  type="submit"
                  disabled={
                    !canUpdate || update.isPending || !changed || !reasonValid
                  }
                >
                  <SaveIcon data-icon="inline-start" />
                  {update.isPending ? "保存中…" : "保存修改"}
                </Button>
              </div>
            </FieldGroup>
          </form>
        </CardContent>
      </Card>
    </StaffPageFrame>
  )
}

function NumberField({
  id,
  label,
  description,
  value,
  min,
  max,
  disabled,
  onChange,
}: {
  id: string
  label: string
  description: string
  value: number
  min: number
  max: number
  disabled: boolean
  onChange: (value: number) => void
}) {
  return (
    <Field>
      <FieldLabel htmlFor={id}>{label}</FieldLabel>
      <Input
        id={id}
        type="number"
        min={min}
        max={max}
        value={value}
        disabled={disabled}
        onChange={(event) =>
          onChange(
            Math.max(min, Math.min(max, Number(event.target.value) || min))
          )
        }
      />
      <FieldDescription>{description}</FieldDescription>
    </Field>
  )
}

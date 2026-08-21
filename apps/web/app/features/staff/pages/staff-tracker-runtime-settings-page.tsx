import * as React from "react"
import { useQuery } from "@tanstack/react-query"
import {
  CircleAlertIcon,
  CircleCheckIcon,
  Clock3Icon,
  GaugeIcon,
  RefreshCwIcon,
  RouterIcon,
  SaveIcon,
  ServerCogIcon,
  ShieldCheckIcon,
  UsersRoundIcon,
} from "lucide-react"

import { Alert, AlertDescription, AlertTitle } from "~/components/ui/alert"
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
  FieldContent,
  FieldDescription,
  FieldGroup,
  FieldLabel,
} from "~/components/ui/field"
import { Input } from "~/components/ui/input"
import { Skeleton } from "~/components/ui/skeleton"
import { Spinner } from "~/components/ui/spinner"
import { Switch } from "~/components/ui/switch"
import { Textarea } from "~/components/ui/textarea"
import {
  type TrackerPolicySettings,
  trackerSettingsQueryOptions,
  useIssueTrackerPolicy,
} from "~/features/staff/api/operations.queries"
import { StaffAccessGate } from "~/features/staff/components/staff-access-gate"
import { StaffPageFrame } from "~/features/staff/components/staff-page-frame"
import { StaffSettingsValueTable } from "~/features/staff/components/staff-settings-value-table"
import { hasCapability } from "~/features/staff/model/capability"
import type { components } from "~/generated/api"
import { MetricCard } from "~/shared/components/metric-card"
import { formatInteger } from "~/shared/formatters/integer"

type ClientFamily = components["schemas"]["TrackerClientRule"]["family"]

const clientFamilies: ReadonlyArray<{
  family: ClientFamily
  label: string
}> = [
  { family: "qbittorrent", label: "qBittorrent" },
  { family: "transmission", label: "Transmission" },
  { family: "deluge", label: "Deluge" },
  { family: "libtorrent", label: "libtorrent" },
  { family: "utorrent", label: "µTorrent" },
]

export function StaffTrackerRuntimeSettingsPage() {
  return (
    <StaffAccessGate
      requiredAction="tracker.policy.read"
      pageHeader={{
        title: "Tracker 设置",
        description: "管理客户端连接、Scrape 和请求保护规则。",
      }}
    >
      {({ session, capabilities }) => (
        <TrackerRuntimeSettingsContent
          csrfToken={session.csrf_token}
          canUpdate={hasCapability(capabilities, "tracker.policy.issue")}
        />
      )}
    </StaffAccessGate>
  )
}

function TrackerRuntimeSettingsContent({
  csrfToken,
  canUpdate,
}: {
  csrfToken: string
  canUpdate: boolean
}) {
  const settings = useQuery(trackerSettingsQueryOptions())
  if (settings.isPending) return <TrackerRuntimeSettingsSkeleton />
  if (settings.isError || !settings.data) {
    return (
      <StaffPageFrame className="gap-4">
        <Alert variant="destructive">
          <CircleAlertIcon />
          <AlertTitle>Tracker 设置暂时无法读取</AlertTitle>
          <AlertDescription>
            请确认 Core、Tracker 和签名策略快照均已启动并处于就绪状态。
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

  const data = settings.data
  return (
    <StaffPageFrame className="gap-4">
      <header className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h1 className="font-heading text-xl font-semibold">Tracker 设置</h1>
          <p className="mt-1 text-sm text-muted-foreground">
            日常策略在这里统一管理；进程容量仍由部署配置控制，避免误改后中断服务。
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
          title="Announce 间隔"
          value={formatDuration(
            data.effective.settings.announce_interval_seconds
          )}
          description={`最小 ${formatDuration(data.effective.settings.min_announce_interval_seconds)}`}
          icon={<Clock3Icon />}
          tone="primary"
        />
        <MetricCard
          title="默认返回 Peer"
          value={formatInteger(data.effective.settings.default_numwant)}
          description={`单次最多 ${formatInteger(data.effective.settings.max_numwant)}`}
          icon={<UsersRoundIcon />}
          tone="default"
        />
        <MetricCard
          title="Scrape"
          value={data.effective.settings.scrape_enabled ? "已启用" : "已关闭"}
          description={`单次最多 ${data.effective.settings.max_scrape_hashes} 个种子`}
          icon={<GaugeIcon />}
          tone="default"
        />
        <MetricCard
          title="客户端策略"
          value={
            data.effective.settings.client_mode === "allow_all"
              ? "允许全部"
              : "白名单"
          }
          description={`生效版本 #${data.effective.sequence}`}
          icon={<ShieldCheckIcon />}
          tone="muted"
        />
      </div>

      {data.activation_pending ? (
        <Alert>
          <RefreshCwIcon />
          <AlertTitle>新政策等待签名发布</AlertTitle>
          <AlertDescription>
            Core 已配置 #{data.configured.sequence}，Tracker 当前生效 #
            {data.effective.sequence}。签名发布器会在下一轮刷新时发布， Tracker
            验证后热加载，无需重启。
          </AlertDescription>
        </Alert>
      ) : (
        <Alert>
          <CircleCheckIcon />
          <AlertTitle>已配置政策与 Tracker 实际状态一致</AlertTitle>
          <AlertDescription>
            当前第 {data.effective.sequence} 版设置已经生效，生成于
            {formatDateTime(data.effective.generated_at)}。
          </AlertDescription>
        </Alert>
      )}

      <TrackerPolicyForm
        key={data.configured.sequence}
        initial={data.configured.settings}
        expectedSequence={data.configured.sequence}
        csrfToken={csrfToken}
        canUpdate={canUpdate}
      />

      <Card className="gap-0 py-0">
        <CardHeader className="p-6 pb-4">
          <CardTitle className="flex items-center gap-2">
            <ServerCogIcon className="size-4" />
            进程容量（只读）
          </CardTitle>
          <CardDescription>
            这些值影响内存布局和 Peer 生命周期，修改后需要受控重启。
          </CardDescription>
        </CardHeader>
        <CardContent className="p-0">
          <StaffSettingsValueTable
            rows={[
              {
                label: "Peer 过期时间",
                value: formatDuration(data.capacity.peer_ttl_seconds),
              },
              {
                label: "最大群集数",
                value: formatInteger(data.capacity.max_swarms),
              },
              {
                label: "最大 Peer 总数",
                value: formatInteger(data.capacity.max_peers),
              },
              {
                label: "单种最大 Peer",
                value: formatInteger(data.capacity.max_peers_per_swarm),
              },
            ]}
          />
        </CardContent>
      </Card>
    </StaffPageFrame>
  )
}

function TrackerPolicyForm({
  initial,
  expectedSequence,
  csrfToken,
  canUpdate,
}: {
  initial: TrackerPolicySettings
  expectedSequence: string
  csrfToken: string
  canUpdate: boolean
}) {
  const mutation = useIssueTrackerPolicy()
  const [settings, setSettings] = React.useState(() => cloneSettings(initial))
  const [reason, setReason] = React.useState("")
  const [error, setError] = React.useState("")
  const [success, setSuccess] = React.useState("")
  const changed = JSON.stringify(settings) !== JSON.stringify(initial)
  const disabled = !canUpdate || mutation.isPending

  function updateNumber(
    key: keyof Pick<
      TrackerPolicySettings,
      | "announce_interval_seconds"
      | "min_announce_interval_seconds"
      | "default_numwant"
      | "max_numwant"
      | "max_scrape_hashes"
      | "user_requests_per_minute"
      | "user_burst"
      | "address_requests_per_minute"
      | "address_burst"
    >,
    value: string
  ) {
    setSettings((current) => ({
      ...current,
      [key]: Number.parseInt(value, 10) || 0,
    }))
  }

  function setAllowList(enabled: boolean) {
    setSettings((current) => ({
      ...current,
      client_mode: enabled ? "allow_list" : "allow_all",
      allowed_clients: enabled
        ? current.allowed_clients.length > 0
          ? current.allowed_clients
          : clientFamilies.map(({ family }) => ({ family, min_version: "" }))
        : [],
    }))
  }

  function setClientEnabled(family: ClientFamily, enabled: boolean) {
    setSettings((current) => ({
      ...current,
      allowed_clients: enabled
        ? [...current.allowed_clients, { family, min_version: "" }].sort(
            (left, right) => left.family.localeCompare(right.family)
          )
        : current.allowed_clients.filter((rule) => rule.family !== family),
    }))
  }

  function setClientVersion(family: ClientFamily, minVersion: string) {
    setSettings((current) => ({
      ...current,
      allowed_clients: current.allowed_clients.map((rule) =>
        rule.family === family ? { ...rule, min_version: minVersion } : rule
      ),
    }))
  }

  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    mutation.reset()
    setSuccess("")
    const validation = validatePolicy(settings, reason, changed)
    if (validation) {
      setError(validation)
      return
    }
    setError("")
    try {
      const issued = await mutation.mutateAsync({
        csrfToken,
        idempotencyKey: crypto.randomUUID(),
        body: {
          expected_sequence: expectedSequence,
          settings,
          reason: reason.trim(),
        },
      })
      setReason("")
      setSuccess(
        `政策 #${issued.sequence} 已保存，将在下一轮签名发布后由 Tracker 自动热加载。`
      )
    } catch {
      // Typed problem details remain on the mutation error. The compact form
      // message below avoids exposing transport details to administrators.
    }
  }

  return (
    <form onSubmit={submit} className="flex flex-col gap-4" noValidate>
      {success ? (
        <Alert>
          <CircleCheckIcon />
          <AlertTitle>Tracker 政策已保存</AlertTitle>
          <AlertDescription>{success}</AlertDescription>
        </Alert>
      ) : null}
      {error || mutation.isError ? (
        <Alert variant="destructive">
          <CircleAlertIcon />
          <AlertTitle>无法保存 Tracker 政策</AlertTitle>
          <AlertDescription>
            {error || "请求失败，请刷新当前版本后重试。"}
          </AlertDescription>
        </Alert>
      ) : null}

      <div className="grid gap-4 xl:grid-cols-2">
        <PolicyCard
          title="客户端通信"
          description="与常用 PT 后台字段保持接近，所有数值都会由 Core 再次校验。"
          icon={<Clock3Icon className="size-4" />}
        >
          <FieldGroup className="grid gap-4 sm:grid-cols-2">
            <NumberField
              id="announce-interval"
              label="Announce 间隔（秒）"
              value={settings.announce_interval_seconds}
              min={60}
              max={86400}
              disabled={disabled}
              onChange={(value) =>
                updateNumber("announce_interval_seconds", value)
              }
            />
            <NumberField
              id="min-announce-interval"
              label="最小 Announce 间隔（秒）"
              value={settings.min_announce_interval_seconds}
              min={30}
              max={86400}
              disabled={disabled}
              onChange={(value) =>
                updateNumber("min_announce_interval_seconds", value)
              }
            />
            <NumberField
              id="default-numwant"
              label="默认返回 Peer 数"
              value={settings.default_numwant}
              min={0}
              max={500}
              disabled={disabled}
              onChange={(value) => updateNumber("default_numwant", value)}
            />
            <NumberField
              id="max-numwant"
              label="最大返回 Peer 数"
              value={settings.max_numwant}
              min={1}
              max={500}
              disabled={disabled}
              onChange={(value) => updateNumber("max_numwant", value)}
            />
          </FieldGroup>
          <Field orientation="horizontal" data-disabled={disabled}>
            <FieldContent>
              <FieldLabel htmlFor="scrape-enabled">
                允许客户端 Scrape
              </FieldLabel>
              <FieldDescription>
                Scrape 需要用户 passkey，只返回站内已登记种子的群集统计。
              </FieldDescription>
            </FieldContent>
            <Switch
              id="scrape-enabled"
              checked={settings.scrape_enabled}
              onCheckedChange={(checked) =>
                setSettings((current) => ({
                  ...current,
                  scrape_enabled: checked,
                }))
              }
              disabled={disabled}
            />
          </Field>
          <NumberField
            id="max-scrape-hashes"
            label="单次最多查询种子数"
            value={settings.max_scrape_hashes}
            min={1}
            max={100}
            disabled={disabled}
            onChange={(value) => updateNumber("max_scrape_hashes", value)}
          />
        </PolicyCard>

        <PolicyCard
          title="请求保护"
          description="地址限制先挡异常请求，用户限制再保护单个账号；不会保存原始地址。"
          icon={<RouterIcon className="size-4" />}
        >
          <FieldGroup className="grid gap-4 sm:grid-cols-2">
            <NumberField
              id="user-rate"
              label="单用户每分钟请求"
              value={settings.user_requests_per_minute}
              min={1}
              max={600}
              disabled={disabled}
              onChange={(value) =>
                updateNumber("user_requests_per_minute", value)
              }
            />
            <NumberField
              id="user-burst"
              label="单用户突发上限"
              value={settings.user_burst}
              min={1}
              max={1200}
              disabled={disabled}
              onChange={(value) => updateNumber("user_burst", value)}
            />
            <NumberField
              id="address-rate"
              label="单地址每分钟请求"
              value={settings.address_requests_per_minute}
              min={1}
              max={5000}
              disabled={disabled}
              onChange={(value) =>
                updateNumber("address_requests_per_minute", value)
              }
            />
            <NumberField
              id="address-burst"
              label="单地址突发上限"
              value={settings.address_burst}
              min={1}
              max={10000}
              disabled={disabled}
              onChange={(value) => updateNumber("address_burst", value)}
            />
          </FieldGroup>
          <Alert>
            <ShieldCheckIcon />
            <AlertTitle>隐私边界</AlertTitle>
            <AlertDescription>
              限流器只在内存中保存域分离哈希键，不落库、不写日志，也不存
              passkey。
            </AlertDescription>
          </Alert>
        </PolicyCard>
      </div>

      <PolicyCard
        title="允许的客户端"
        description="默认兼容迁移用户；切换白名单后只按 peer-id 固定指纹识别，不信任 User-Agent 或自定义正则。"
        icon={<ShieldCheckIcon className="size-4" />}
      >
        <Field orientation="horizontal" data-disabled={disabled}>
          <FieldContent>
            <FieldLabel htmlFor="client-allow-list">
              启用客户端白名单
            </FieldLabel>
            <FieldDescription>
              首次启用会选中全部已支持客户端，再按站点需要逐项关闭。
            </FieldDescription>
          </FieldContent>
          <Switch
            id="client-allow-list"
            checked={settings.client_mode === "allow_list"}
            onCheckedChange={setAllowList}
            disabled={disabled}
          />
        </Field>
        {settings.client_mode === "allow_list" ? (
          <div className="overflow-hidden rounded-lg border">
            {clientFamilies.map(({ family, label }, index) => {
              const rule = settings.allowed_clients.find(
                (candidate) => candidate.family === family
              )
              return (
                <div
                  key={family}
                  className={`grid gap-3 p-3 sm:grid-cols-[minmax(11rem,1fr)_minmax(12rem,18rem)] sm:items-center ${index > 0 ? "border-t" : ""}`}
                >
                  <Field orientation="horizontal" data-disabled={disabled}>
                    <FieldContent>
                      <FieldLabel htmlFor={`client-${family}`}>
                        {label}
                      </FieldLabel>
                      <FieldDescription>固定指纹：{family}</FieldDescription>
                    </FieldContent>
                    <Switch
                      id={`client-${family}`}
                      checked={Boolean(rule)}
                      onCheckedChange={(checked) =>
                        setClientEnabled(family, checked)
                      }
                      disabled={disabled}
                    />
                  </Field>
                  <Field data-disabled={disabled || !rule}>
                    <FieldLabel htmlFor={`client-version-${family}`}>
                      最低版本（可留空）
                    </FieldLabel>
                    <Input
                      id={`client-version-${family}`}
                      value={rule?.min_version ?? ""}
                      placeholder="例如 4.6.4"
                      disabled={disabled || !rule}
                      onChange={(event) =>
                        setClientVersion(family, event.target.value)
                      }
                    />
                  </Field>
                </div>
              )
            })}
          </div>
        ) : null}
      </PolicyCard>

      {canUpdate ? (
        <Card>
          <CardHeader>
            <CardTitle>变更说明</CardTitle>
            <CardDescription>
              每次保存都会创建不可变政策版本，历史配置不会被覆盖。
            </CardDescription>
          </CardHeader>
          <CardContent className="flex flex-col gap-4">
            <Field data-disabled={mutation.isPending}>
              <FieldLabel htmlFor="tracker-policy-reason">修改原因</FieldLabel>
              <Textarea
                id="tracker-policy-reason"
                value={reason}
                minLength={5}
                maxLength={1000}
                placeholder="至少 5 个字符，例如：调整 Scrape 请求频率。"
                disabled={mutation.isPending}
                onChange={(event) => setReason(event.target.value)}
              />
            </Field>
            <div className="flex justify-end">
              <Button type="submit" disabled={!changed || mutation.isPending}>
                {mutation.isPending ? (
                  <Spinner data-icon="inline-start" />
                ) : (
                  <SaveIcon data-icon="inline-start" />
                )}
                {mutation.isPending ? "保存中…" : "保存政策"}
              </Button>
            </div>
          </CardContent>
        </Card>
      ) : (
        <Alert>
          <ShieldCheckIcon />
          <AlertTitle>当前权限为只读</AlertTitle>
          <AlertDescription>
            修改此页需要 tracker.policy.issue 权限。
          </AlertDescription>
        </Alert>
      )}
    </form>
  )
}

function PolicyCard({
  title,
  description,
  icon,
  children,
}: {
  title: string
  description: string
  icon: React.ReactNode
  children: React.ReactNode
}) {
  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          {icon}
          {title}
        </CardTitle>
        <CardDescription>{description}</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-5">{children}</CardContent>
    </Card>
  )
}

function NumberField({
  id,
  label,
  value,
  min,
  max,
  disabled,
  onChange,
}: {
  id: string
  label: string
  value: number
  min: number
  max: number
  disabled: boolean
  onChange: (value: string) => void
}) {
  return (
    <Field data-disabled={disabled}>
      <FieldLabel htmlFor={id}>{label}</FieldLabel>
      <Input
        id={id}
        type="number"
        inputMode="numeric"
        value={value}
        min={min}
        max={max}
        disabled={disabled}
        onChange={(event) => onChange(event.target.value)}
      />
      <FieldDescription>
        允许范围 {formatInteger(min)}–{formatInteger(max)}
      </FieldDescription>
    </Field>
  )
}

function validatePolicy(
  settings: TrackerPolicySettings,
  reason: string,
  changed: boolean
) {
  if (!changed) return "设置没有变化，无需创建空版本。"
  if (Array.from(reason.trim()).length < 5) return "修改原因至少需要 5 个字符。"
  if (
    settings.announce_interval_seconds < 60 ||
    settings.announce_interval_seconds > 86400 ||
    settings.min_announce_interval_seconds < 30 ||
    settings.min_announce_interval_seconds > settings.announce_interval_seconds
  ) {
    return "请检查 Announce 间隔，最小间隔不能大于常规间隔。"
  }
  if (
    settings.default_numwant < 0 ||
    settings.max_numwant < 1 ||
    settings.max_numwant > 500 ||
    settings.default_numwant > settings.max_numwant
  ) {
    return "默认返回 Peer 数不能大于最大返回数。"
  }
  if (settings.max_scrape_hashes < 1 || settings.max_scrape_hashes > 100) {
    return "Scrape 单次查询数必须介于 1–100。"
  }
  if (
    settings.user_requests_per_minute < 1 ||
    settings.user_requests_per_minute > 600 ||
    settings.user_burst < 1 ||
    settings.user_burst > 1200 ||
    settings.address_requests_per_minute < 1 ||
    settings.address_requests_per_minute > 5000 ||
    settings.address_burst < 1 ||
    settings.address_burst > 10000
  ) {
    return "请检查用户和地址的每分钟请求数与突发上限。"
  }
  if (
    settings.client_mode === "allow_list" &&
    settings.allowed_clients.length === 0
  ) {
    return "启用客户端白名单时至少保留一个客户端。"
  }
  const versionPattern =
    /^$|^([0-9]|[0-2][0-9]|3[0-5])(\.([0-9]|[0-2][0-9]|3[0-5])){1,3}$/
  if (
    settings.allowed_clients.some(
      (rule) => !versionPattern.test(rule.min_version)
    )
  ) {
    return "最低版本应类似 4.6.4，或留空。"
  }
  return ""
}

function cloneSettings(settings: TrackerPolicySettings): TrackerPolicySettings {
  return {
    ...settings,
    allowed_clients: settings.allowed_clients.map((rule) => ({ ...rule })),
  }
}

function formatDuration(seconds: number) {
  if (seconds % 3600 === 0) return `${seconds / 3600} 小时`
  if (seconds % 60 === 0) return `${seconds / 60} 分钟`
  return `${seconds} 秒`
}

function formatDateTime(value: string) {
  return new Intl.DateTimeFormat("zh-CN", {
    dateStyle: "medium",
    timeStyle: "short",
  }).format(new Date(value))
}

function TrackerRuntimeSettingsSkeleton() {
  return (
    <StaffPageFrame aria-label="正在加载 Tracker 设置">
      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
        {Array.from({ length: 4 }, (_, index) => (
          <Skeleton key={index} className="h-36 rounded-lg" />
        ))}
      </div>
      <Skeleton className="h-20 rounded-lg" />
      <div className="grid gap-4 xl:grid-cols-2">
        <Skeleton className="h-96 rounded-lg" />
        <Skeleton className="h-96 rounded-lg" />
      </div>
    </StaffPageFrame>
  )
}

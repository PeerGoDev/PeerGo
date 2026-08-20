import { useMemo, useState } from "react"
import { useQuery } from "@tanstack/react-query"
import {
  BadgeCheckIcon,
  CircleAlertIcon,
  GaugeIcon,
  NetworkIcon,
  RefreshCwIcon,
  SaveIcon,
  ServerIcon,
  ShieldAlertIcon,
} from "lucide-react"

import { Alert, AlertDescription, AlertTitle } from "~/components/ui/alert"
import { Badge } from "~/components/ui/badge"
import { Button } from "~/components/ui/button"
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
  FieldTitle,
} from "~/components/ui/field"
import { Input } from "~/components/ui/input"
import { Skeleton } from "~/components/ui/skeleton"
import { Spinner } from "~/components/ui/spinner"
import { Switch } from "~/components/ui/switch"
import { Textarea } from "~/components/ui/textarea"
import {
  settlementSettingsQueryOptions,
  trackerSettingsQueryOptions,
  type TrackerPolicySettings,
  useIssueTrackerPolicy,
} from "~/features/staff/api/operations.queries"
import { adminSeedboxReportsQueryOptions } from "~/features/seedbox/api/seedbox.queries"
import { StaffAccessGate } from "~/features/staff/components/staff-access-gate"
import { StaffPageFrame } from "~/features/staff/components/staff-page-frame"
import { SeedboxReportQueue } from "~/features/staff/components/seedbox-report-queue"
import { hasCapability } from "~/features/staff/model/capability"
import type { components } from "~/generated/api"
import { ApiProblemError } from "~/shared/api/problem"
import { MetricCard } from "~/shared/components/metric-card"

type CapabilityList = components["schemas"]["CapabilityList"]

export function StaffSeedboxSettingsPage() {
  return (
    <StaffAccessGate
      requiredAction="tracker.policy.read"
      pageHeader={{
        title: "盒子设置",
        description: "管理可信网络、上传折算和速度证据阈值。",
      }}
    >
      {({ session, capabilities }) => (
        <SeedboxSettingsContent
          csrfToken={session.csrf_token}
          capabilities={capabilities}
        />
      )}
    </StaffAccessGate>
  )
}

function SeedboxSettingsContent({
  csrfToken,
  capabilities,
}: {
  csrfToken: string
  capabilities: CapabilityList
}) {
  const tracker = useQuery(trackerSettingsQueryOptions())
  const settlement = useQuery(settlementSettingsQueryOptions())
  const canReadRegistry = hasCapability(
    capabilities,
    "tracker.seedbox.registry.read"
  )
  const reports = useQuery({
    ...adminSeedboxReportsQueryOptions(),
    enabled: canReadRegistry,
  })
  if (tracker.isPending || settlement.isPending) {
    return <SettingsSkeleton label="盒子设置" />
  }
  if (
    tracker.isError ||
    !tracker.data ||
    settlement.isError ||
    !settlement.data
  ) {
    return (
      <SettingsError
        retry={() => {
          void tracker.refetch()
          void settlement.refetch()
        }}
      />
    )
  }
  return (
    <SeedboxPolicyEditor
      key={tracker.data.configured.sequence}
      csrfToken={csrfToken}
      canIssue={hasCapability(capabilities, "tracker.policy.issue")}
      configured={tracker.data.configured.settings}
      effective={tracker.data.effective.settings}
      configuredSequence={tracker.data.configured.sequence}
      activationPending={tracker.data.activation_pending}
      settlementPrimitiveSupported={
        settlement.data.seedbox.settlement_primitive_supported
      }
      classificationConnected={settlement.data.seedbox.classification_connected}
      registryConnected={settlement.data.seedbox.registry_connected}
      speedObservationConnected={
        settlement.data.seedbox.speed_observation_connected
      }
      reportQueue={reports.data}
      reportQueuePending={reports.isPending && canReadRegistry}
      reportQueueError={reports.isError}
      canReadRegistry={canReadRegistry}
      canDecideReports={hasCapability(
        capabilities,
        "tracker.seedbox.report.decide"
      )}
      refreshReports={() => void reports.refetch()}
    />
  )
}

function SeedboxPolicyEditor({
  csrfToken,
  canIssue,
  configured,
  effective,
  configuredSequence,
  activationPending,
  settlementPrimitiveSupported,
  classificationConnected,
  registryConnected,
  speedObservationConnected,
  reportQueue,
  reportQueuePending,
  reportQueueError,
  canReadRegistry,
  canDecideReports,
  refreshReports,
}: {
  csrfToken: string
  canIssue: boolean
  configured: TrackerPolicySettings
  effective: TrackerPolicySettings
  configuredSequence: string
  activationPending: boolean
  settlementPrimitiveSupported: boolean
  classificationConnected: boolean
  registryConnected: boolean
  speedObservationConnected: boolean
  reportQueue?: import("~/features/seedbox/api/seedbox.queries").SeedboxReportPage
  reportQueuePending: boolean
  reportQueueError: boolean
  canReadRegistry: boolean
  canDecideReports: boolean
  refreshReports: () => void
}) {
  const mutation = useIssueTrackerPolicy()
  const [enabled, setEnabled] = useState(configured.seedbox.enabled)
  const [uploadFactor, setUploadFactor] = useState(
    String(configured.seedbox.upload_factor_basis_points / 10_000)
  )
  const [seedboxSpeed, setSeedboxSpeed] = useState(
    bytesToMiB(configured.seedbox.seedbox_speed_limit_bytes_per_second)
  )
  const [standardSpeed, setStandardSpeed] = useState(
    bytesToMiB(configured.seedbox.standard_speed_limit_bytes_per_second)
  )
  const [rules, setRules] = useState(
    configured.seedbox.rules
      .map(
        (rule) =>
          `${rule.id}${rule.user_numeric_id ? ` @ ${rule.user_numeric_id}` : ""} = ${rule.cidr}`
      )
      .join("\n")
  )
  const [reason, setReason] = useState("")
  const [validationError, setValidationError] = useState("")
  const parsedRules = useMemo(() => parseRules(rules), [rules])
  const effectiveReady =
    effective.seedbox.enabled &&
    settlementPrimitiveSupported &&
    classificationConnected

  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const factor = Number(uploadFactor)
    const seedboxSpeedMiB = Number(seedboxSpeed)
    const standardSpeedMiB = Number(standardSpeed)
    if (
      !Number.isFinite(factor) ||
      factor < 0 ||
      factor > 1 ||
      !Number.isFinite(seedboxSpeedMiB) ||
      seedboxSpeedMiB < 0 ||
      !Number.isFinite(standardSpeedMiB) ||
      standardSpeedMiB < 0 ||
      parsedRules.error ||
      reason.trim().length < 10
    ) {
      setValidationError(
        parsedRules.error ||
          "请检查倍率、速度，并填写至少 10 个字符的修改说明。"
      )
      return
    }
    setValidationError("")
    try {
      await mutation.mutateAsync({
        csrfToken,
        idempotencyKey: crypto.randomUUID(),
        body: {
          expected_sequence: configuredSequence,
          reason: reason.trim(),
          settings: {
            ...configured,
            seedbox: {
              enabled,
              upload_factor_basis_points: Math.round(factor * 10_000),
              seedbox_speed_limit_bytes_per_second: miBToBytes(seedboxSpeedMiB),
              standard_speed_limit_bytes_per_second:
                miBToBytes(standardSpeedMiB),
              rules: parsedRules.items,
            },
          },
        },
      })
      setReason("")
    } catch (error) {
      setValidationError(
        error instanceof ApiProblemError
          ? error.detail || error.message
          : "盒子政策签发失败。"
      )
    }
  }

  return (
    <StaffPageFrame className="gap-4">
      <header className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h1 className="font-heading text-xl font-semibold">盒子设置</h1>
          <p className="mt-1 text-sm text-muted-foreground">
            Tracker 只发布分类证据，不把用户 IP 传给 Settlement。
          </p>
        </div>
        <Badge variant={activationPending ? "secondary" : "outline"}>
          {activationPending ? "等待 Tracker 热加载" : "已生效"}
        </Badge>
      </header>

      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
        <MetricCard
          title="整体状态"
          value={effectiveReady ? "已启用" : "未启用"}
          description="Tracker 分类与 Settlement 折算"
          icon={<ServerIcon />}
          tone={effectiveReady ? "positive" : "muted"}
        />
        <MetricCard
          title="当前上传折算"
          value={formatFactor(effective.seedbox.upload_factor_basis_points)}
          description="VIP 盒子不应用此折算"
          icon={<GaugeIcon />}
          tone="primary"
        />
        <MetricCard
          title="可信网段"
          value={String(effective.seedbox.rules.length)}
          description="最长前缀匹配，规则编号进入证据"
          icon={<NetworkIcon />}
          tone={effective.seedbox.rules.length > 0 ? "positive" : "muted"}
        />
        <MetricCard
          title="超速观察"
          value={speedObservationConnected ? "已接入" : "未接入"}
          description="只记录脱敏证据，不自动封禁"
          icon={<BadgeCheckIcon />}
          tone={speedObservationConnected ? "positive" : "warning"}
        />
      </div>

      {!registryConnected ? (
        <Alert>
          <CircleAlertIcon />
          <AlertTitle>用户申报与审核尚未接入</AlertTitle>
          <AlertDescription>
            当前可信网段由管理员直接签发；用户绑定的盒子申请会在下一阶段进入独立审核记录，不会复用全局网段规则。
          </AlertDescription>
        </Alert>
      ) : null}

      {!settlementPrimitiveSupported ? (
        <Alert variant="destructive">
          <ShieldAlertIcon />
          <AlertTitle>结算服务尚未支持盒子证据</AlertTitle>
          <AlertDescription>
            在 Settlement 完成升级前请勿启用，否则 Tracker
            会拒绝不完整的运行状态。
          </AlertDescription>
        </Alert>
      ) : null}

      <form onSubmit={submit}>
        <Card>
          <CardHeader>
            <CardTitle>签发盒子政策</CardTitle>
            <CardDescription>
              保存会创建新的不可变 Tracker 政策版本，不会覆盖历史 announce
              证据。
            </CardDescription>
          </CardHeader>
          <CardContent>
            <FieldGroup>
              <Field orientation="horizontal">
                <FieldTitle>启用盒子识别与上传折算</FieldTitle>
                <Switch
                  checked={enabled}
                  onCheckedChange={setEnabled}
                  disabled={!canIssue || mutation.isPending}
                  aria-label="启用盒子识别与上传折算"
                />
              </Field>
              <div className="grid gap-4 md:grid-cols-3">
                <Field>
                  <FieldLabel htmlFor="seedbox-upload-factor">
                    上传倍率
                  </FieldLabel>
                  <Input
                    id="seedbox-upload-factor"
                    type="number"
                    min="0"
                    max="1"
                    step="0.01"
                    value={uploadFactor}
                    onChange={(event) => setUploadFactor(event.target.value)}
                    disabled={!canIssue || mutation.isPending}
                  />
                  <FieldDescription>
                    例如 0.5 表示上传按 50% 计入。
                  </FieldDescription>
                </Field>
                <Field>
                  <FieldLabel htmlFor="seedbox-speed">
                    盒子速度阈值（MiB/s）
                  </FieldLabel>
                  <Input
                    id="seedbox-speed"
                    type="number"
                    min="0"
                    step="1"
                    value={seedboxSpeed}
                    onChange={(event) => setSeedboxSpeed(event.target.value)}
                    disabled={!canIssue || mutation.isPending}
                  />
                  <FieldDescription>
                    0 表示只分类、不记录超速阈值。
                  </FieldDescription>
                </Field>
                <Field>
                  <FieldLabel htmlFor="standard-speed">
                    普通连接阈值（MiB/s）
                  </FieldLabel>
                  <Input
                    id="standard-speed"
                    type="number"
                    min="0"
                    step="1"
                    value={standardSpeed}
                    onChange={(event) => setStandardSpeed(event.target.value)}
                    disabled={!canIssue || mutation.isPending}
                  />
                  <FieldDescription>
                    用于后续超速观察，不会立即封禁。
                  </FieldDescription>
                </Field>
              </div>
              <Field data-invalid={Boolean(parsedRules.error)}>
                <FieldLabel htmlFor="seedbox-rules">可信网段</FieldLabel>
                <Textarea
                  id="seedbox-rules"
                  rows={8}
                  value={rules}
                  onChange={(event) => setRules(event.target.value)}
                  disabled={!canIssue || mutation.isPending}
                  aria-invalid={Boolean(parsedRules.error)}
                  placeholder={
                    "member-box @ 1234 = 203.0.113.7/32\nprovider-range = 2001:db8:1234::/48"
                  }
                />
                <FieldDescription>
                  用户专属规则使用“规则编号 @ 用户数字 ID = CIDR”；省略用户 ID
                  时为全站可信网段。单个 IPv4 使用 /32，IPv6 使用 /128。
                </FieldDescription>
                {parsedRules.error ? (
                  <FieldError>{parsedRules.error}</FieldError>
                ) : null}
              </Field>
              <Field data-invalid={Boolean(validationError)}>
                <FieldLabel htmlFor="seedbox-reason">修改说明</FieldLabel>
                <Textarea
                  id="seedbox-reason"
                  rows={3}
                  value={reason}
                  onChange={(event) => setReason(event.target.value)}
                  disabled={!canIssue || mutation.isPending}
                  aria-invalid={Boolean(validationError)}
                  placeholder="说明本次新增网段、倍率或速度阈值的依据"
                />
                {validationError ? (
                  <FieldError>{validationError}</FieldError>
                ) : null}
              </Field>
            </FieldGroup>
          </CardContent>
          <CardFooter className="justify-end">
            <Button disabled={!canIssue || mutation.isPending} type="submit">
              {mutation.isPending ? (
                <Spinner data-icon="inline-start" />
              ) : (
                <SaveIcon data-icon="inline-start" />
              )}
              签发政策
            </Button>
          </CardFooter>
        </Card>
      </form>
      <SeedboxReportQueue
        data={reportQueue}
        isPending={reportQueuePending}
        isError={reportQueueError}
        canRead={canReadRegistry}
        canDecide={canDecideReports}
        csrfToken={csrfToken}
        refresh={refreshReports}
      />
    </StaffPageFrame>
  )
}

function parseRules(value: string): {
  items: TrackerPolicySettings["seedbox"]["rules"]
  error: string
} {
  const items: TrackerPolicySettings["seedbox"]["rules"] = []
  const seenIDs = new Set<string>()
  const seenBindings = new Set<string>()
  for (const [index, raw] of value.split("\n").entries()) {
    const line = raw.trim()
    if (!line) continue
    const match = line.match(
      /^([a-z0-9][a-z0-9._-]{0,63})(?:\s*@\s*([1-9][0-9]*))?\s*=\s*(\S+)$/
    )
    if (!match) return { items: [], error: `第 ${index + 1} 行格式不正确。` }
    const [, id, numericID, cidr] = match
    if (!cidr.includes("/"))
      return { items: [], error: `第 ${index + 1} 行必须填写 CIDR 前缀。` }
    const userNumericID = numericID ? Number(numericID) : undefined
    if (userNumericID && !Number.isSafeInteger(userNumericID))
      return { items: [], error: `第 ${index + 1} 行用户数字 ID 无效。` }
    const binding = `${userNumericID ?? 0}:${cidr}`
    if (seenIDs.has(id) || seenBindings.has(binding))
      return { items: [], error: `第 ${index + 1} 行与已有规则重复。` }
    seenIDs.add(id)
    seenBindings.add(binding)
    items.push({
      id,
      cidr,
      ...(userNumericID ? { user_numeric_id: userNumericID } : {}),
    })
  }
  return { items, error: "" }
}

function miBToBytes(value: number) {
  return Math.round(value * 1024 * 1024)
}
function bytesToMiB(value: number) {
  return String(Math.round(value / 1024 / 1024))
}
function formatFactor(basisPoints: number) {
  return `${(basisPoints / 10_000).toFixed(2)}x`
}

function SettingsError({ retry }: { retry: () => void }) {
  return (
    <StaffPageFrame className="gap-4">
      <Alert variant="destructive">
        <CircleAlertIcon />
        <AlertTitle>盒子设置暂时无法读取</AlertTitle>
        <AlertDescription>
          请确认 Tracker 与 Settlement 控制服务已启动并重试。
        </AlertDescription>
      </Alert>
      <Button variant="outline" className="w-fit" onClick={retry}>
        <RefreshCwIcon data-icon="inline-start" />
        重试
      </Button>
    </StaffPageFrame>
  )
}

function SettingsSkeleton({ label }: { label: string }) {
  return (
    <StaffPageFrame aria-label={`正在加载${label}`}>
      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
        {Array.from({ length: 4 }, (_, index) => (
          <Skeleton key={index} className="h-36 rounded-lg" />
        ))}
      </div>
      <Skeleton className="h-20 rounded-lg" />
      <Skeleton className="h-96 rounded-lg" />
    </StaffPageFrame>
  )
}

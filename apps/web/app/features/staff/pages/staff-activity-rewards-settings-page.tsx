import * as React from "react"
import { useQuery } from "@tanstack/react-query"
import {
  CalendarCheck2Icon,
  CircleAlertIcon,
  CircleCheckIcon,
  GiftIcon,
  PlusIcon,
  RefreshCwIcon,
  SaveIcon,
  ShieldCheckIcon,
  Trash2Icon,
} from "lucide-react"

import { Alert, AlertDescription, AlertTitle } from "~/components/ui/alert"
import { Badge } from "~/components/ui/badge"
import { Button } from "~/components/ui/button"
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from "~/components/ui/card"
import {
  Field,
  FieldContent,
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLabel,
  FieldTitle,
} from "~/components/ui/field"
import { Input } from "~/components/ui/input"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "~/components/ui/select"
import { Skeleton } from "~/components/ui/skeleton"
import { Spinner } from "~/components/ui/spinner"
import { Switch } from "~/components/ui/switch"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "~/components/ui/table"
import { Textarea } from "~/components/ui/textarea"
import {
  type AttendancePolicy,
  type AttendancePolicySettings,
  attendancePolicyListQueryOptions,
  useIssueAttendancePolicy,
} from "~/features/staff/api/attendance-administration.queries"
import { economySettingsQueryOptions } from "~/features/staff/api/operations.queries"
import { StaffAccessGate } from "~/features/staff/components/staff-access-gate"
import { StaffPageFrame } from "~/features/staff/components/staff-page-frame"
import { hasCapability } from "~/features/staff/model/capability"
import type { components } from "~/generated/api"
import { requestErrorDescription } from "~/shared/api/problem"
import { formatDateTime } from "~/shared/formatters/date-time"
import { formatInteger } from "~/shared/formatters/integer"

type CapabilityList = components["schemas"]["CapabilityList"]
type MilestoneDraft = { days: string; reward: string }

export function StaffActivityRewardsSettingsPage() {
  return (
    <StaffAccessGate
      requiredAction="economy.attendance.policy.read"
      pageHeader={{
        title: "签到与活动奖励",
        description: "设置每日签到、连续奖励和活动入账规则。",
      }}
    >
      {({ session, capabilities }) => (
        <ActivityRewardsContent
          csrfToken={session.csrf_token}
          capabilities={capabilities}
        />
      )}
    </StaffAccessGate>
  )
}

function ActivityRewardsContent({
  csrfToken,
  capabilities,
}: {
  csrfToken: string
  capabilities: CapabilityList
}) {
  const policies = useQuery(attendancePolicyListQueryOptions())
  const economy = useQuery(economySettingsQueryOptions())
  const canIssue = hasCapability(
    capabilities,
    "economy.attendance.policy.issue"
  )

  if (policies.isPending) return <SettingsSkeleton />
  if (policies.isError || !policies.data) {
    return (
      <SettingsError
        error={policies.error}
        retry={() => void policies.refetch()}
      />
    )
  }
  const latest = policies.data.items[0]
  if (!latest) {
    return (
      <SettingsError
        error={new Error("没有签到政策基线")}
        retry={() => void policies.refetch()}
      />
    )
  }

  return (
    <StaffPageFrame className="gap-4">
      <header className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h1 className="font-heading text-xl font-semibold">签到与活动奖励</h1>
          <p className="mt-1 text-sm text-muted-foreground">
            设置用户每天看到的签到方式、连续奖励与经验奖励。
          </p>
        </div>
        <Button
          variant="outline"
          size="sm"
          disabled={policies.isFetching}
          onClick={() => void policies.refetch()}
        >
          <RefreshCwIcon
            data-icon="inline-start"
            className={policies.isFetching ? "animate-spin" : undefined}
          />
          刷新
        </Button>
      </header>

      <div className="grid gap-3 sm:grid-cols-3">
        <SummaryCard
          title="签到状态"
          value={latest.settings.enabled ? "已开放" : "已关闭"}
          description={
            latest.effective_from > new Date().toISOString()
              ? "存在待生效设置"
              : "当前规则已生效"
          }
          icon={<CalendarCheck2Icon />}
        />
        <SummaryCard
          title="活动奖励账本"
          value={economy.data?.activity.ledger_supported ? "已接通" : "读取中"}
          description="统一使用整数魔力值"
          icon={<ShieldCheckIcon />}
        />
        <SummaryCard
          title="活动奖励记录"
          value={formatInteger(
            economy.data?.transactions.activity_reward ?? "0"
          )}
          description="包含签到等活动来源"
          icon={<GiftIcon />}
        />
      </div>

      {!canIssue ? (
        <Alert>
          <ShieldCheckIcon />
          <AlertTitle>当前权限仅可查看</AlertTitle>
          <AlertDescription>
            可以查看签到规则和历史，但不能签发新设置。
          </AlertDescription>
        </Alert>
      ) : null}

      <AttendancePolicyEditor
        key={latest.revision}
        initialPolicy={latest}
        csrfToken={csrfToken}
        canIssue={canIssue}
        minimumEffectiveFrom={policies.data.minimum_effective_from}
      />

      <PolicyHistory items={policies.data.items} />
    </StaffPageFrame>
  )
}

function AttendancePolicyEditor({
  initialPolicy,
  csrfToken,
  canIssue,
  minimumEffectiveFrom,
}: {
  initialPolicy: AttendancePolicy
  csrfToken: string
  canIssue: boolean
  minimumEffectiveFrom: string
}) {
  const mutation = useIssueAttendancePolicy()
  const attendancePolicyRequestId = React.useRef<string | undefined>(undefined)
  const initial = initialPolicy.settings
  const [enabled, setEnabled] = React.useState(initial.enabled)
  const [timezone, setTimezone] = React.useState(initial.day_boundary_timezone)
  const [fixedEnabled, setFixedEnabled] = React.useState(initial.fixed_enabled)
  const [fixedReward, setFixedReward] = React.useState(initial.fixed_reward)
  const [randomEnabled, setRandomEnabled] = React.useState(
    initial.random_enabled
  )
  const [randomMin, setRandomMin] = React.useState(initial.random_min)
  const [randomMax, setRandomMax] = React.useState(initial.random_max)
  const [streakEnabled, setStreakEnabled] = React.useState(
    initial.streak_enabled
  )
  const [milestones, setMilestones] = React.useState<MilestoneDraft[]>(
    initial.streak_milestones.map((item) => ({
      days: String(item.days),
      reward: item.reward,
    }))
  )
  const [experienceReward, setExperienceReward] = React.useState(
    initial.experience_reward
  )
  const [reason, setReason] = React.useState("")
  const [validationError, setValidationError] = React.useState("")

  function updateMilestone(
    index: number,
    key: keyof MilestoneDraft,
    value: string
  ) {
    setMilestones((current) =>
      current.map((item, position) =>
        position === index ? { ...item, [key]: value } : item
      )
    )
  }

  function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const error = validateSettings({
      enabled,
      fixedEnabled,
      fixedReward,
      randomEnabled,
      randomMin,
      randomMax,
      experienceReward,
      milestones: streakEnabled ? milestones : [],
      reason,
    })
    if (error) {
      setValidationError(error)
      return
    }
    setValidationError("")
    const settings: AttendancePolicySettings = {
      enabled,
      day_boundary_timezone: timezone,
      fixed_enabled: fixedEnabled,
      fixed_reward: fixedReward,
      random_enabled: randomEnabled,
      random_min: randomMin,
      random_max: randomMax,
      streak_enabled: streakEnabled,
      streak_milestones: milestones.map((item) => ({
        days: Number(item.days),
        reward: item.reward,
      })),
      experience_reward: experienceReward,
    }
    const idempotencyKey =
      attendancePolicyRequestId.current ?? crypto.randomUUID()
    attendancePolicyRequestId.current = idempotencyKey
    mutation.mutate(
      {
        csrfToken,
        settings,
        reason: reason.trim(),
        idempotencyKey,
      },
      {
        onSuccess: () => {
          attendancePolicyRequestId.current = undefined
        },
      }
    )
  }

  const disabled = !canIssue || mutation.isPending
  return (
    <Card>
      <CardHeader>
        <CardTitle>每日签到</CardTitle>
        <CardDescription>
          新设置最早于 {formatDateTime(minimumEffectiveFrom)}
          生效，不会改写历史签到。
        </CardDescription>
        <CardAction>
          <Badge variant="outline">当前规则</Badge>
        </CardAction>
      </CardHeader>
      <CardContent>
        <form id="attendance-policy-form" onSubmit={submit} noValidate>
          <FieldGroup>
            <SwitchField
              id="attendance-enabled"
              title="开放每日签到"
              description="关闭后用户不能新增签到，已有账本与统计仍保留。"
              checked={enabled}
              onCheckedChange={setEnabled}
              disabled={disabled}
            />

            <Field>
              <FieldLabel htmlFor="attendance-timezone">
                每日结算时区
              </FieldLabel>
              <Select
                value={timezone}
                onValueChange={(value) => value && setTimezone(value)}
                disabled={disabled}
              >
                <SelectTrigger
                  id="attendance-timezone"
                  className="w-full sm:w-72"
                >
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="Asia/Shanghai">
                    中国标准时间（Asia/Shanghai）
                  </SelectItem>
                  <SelectItem value="UTC">协调世界时（UTC）</SelectItem>
                </SelectContent>
              </Select>
              <FieldDescription>
                每天零点按这里的时区重置签到资格。
              </FieldDescription>
            </Field>

            <div className="grid gap-4 lg:grid-cols-2">
              <RewardModeCard
                title="固定奖励"
                description="用户签到时可选择稳定获得的魔力值。"
                enabled={fixedEnabled}
                onEnabledChange={setFixedEnabled}
                disabled={disabled}
              >
                <NumberField
                  id="attendance-fixed-reward"
                  label="每日魔力值"
                  value={fixedReward}
                  onChange={setFixedReward}
                  disabled={disabled || !fixedEnabled}
                />
              </RewardModeCard>
              <RewardModeCard
                title="随机奖励"
                description="由服务端安全随机抽取，客户端不能指定结果。"
                enabled={randomEnabled}
                onEnabledChange={setRandomEnabled}
                disabled={disabled}
              >
                <div className="grid grid-cols-2 gap-3">
                  <NumberField
                    id="attendance-random-min"
                    label="最小魔力值"
                    value={randomMin}
                    onChange={setRandomMin}
                    disabled={disabled || !randomEnabled}
                  />
                  <NumberField
                    id="attendance-random-max"
                    label="最大魔力值"
                    value={randomMax}
                    onChange={setRandomMax}
                    disabled={disabled || !randomEnabled}
                  />
                </div>
              </RewardModeCard>
            </div>

            <RewardModeCard
              title="连续签到加奖"
              description="达到指定连续天数时额外发放一次魔力值。"
              enabled={streakEnabled}
              onEnabledChange={setStreakEnabled}
              disabled={disabled}
            >
              <div className="space-y-2">
                {milestones.map((item, index) => (
                  <div
                    key={index}
                    className="grid grid-cols-[1fr_1fr_auto] items-end gap-2"
                  >
                    <NumberField
                      id={`attendance-streak-days-${index}`}
                      label="连续天数"
                      value={item.days}
                      onChange={(value) =>
                        updateMilestone(index, "days", value)
                      }
                      disabled={disabled || !streakEnabled}
                    />
                    <NumberField
                      id={`attendance-streak-reward-${index}`}
                      label="额外魔力值"
                      value={item.reward}
                      onChange={(value) =>
                        updateMilestone(index, "reward", value)
                      }
                      disabled={disabled || !streakEnabled}
                    />
                    <Button
                      type="button"
                      variant="ghost"
                      size="icon"
                      aria-label={`删除第 ${index + 1} 个连续奖励`}
                      disabled={
                        disabled || !streakEnabled || milestones.length <= 1
                      }
                      onClick={() =>
                        setMilestones((current) =>
                          current.filter((_, position) => position !== index)
                        )
                      }
                    >
                      <Trash2Icon />
                    </Button>
                  </div>
                ))}
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  disabled={
                    disabled || !streakEnabled || milestones.length >= 32
                  }
                  onClick={() =>
                    setMilestones((current) => [
                      ...current,
                      { days: "", reward: "" },
                    ])
                  }
                >
                  <PlusIcon data-icon="inline-start" />
                  添加连续奖励
                </Button>
              </div>
            </RewardModeCard>

            <NumberField
              id="attendance-experience"
              label="每次签到经验"
              value={experienceReward}
              onChange={setExperienceReward}
              disabled={disabled}
              description="经验只用于等级计算，不进入可消费魔力值。"
            />

            {canIssue ? (
              <Field data-invalid={Boolean(validationError)}>
                <FieldLabel htmlFor="attendance-change-reason">
                  变更原因
                </FieldLabel>
                <Textarea
                  id="attendance-change-reason"
                  value={reason}
                  onChange={(event) => setReason(event.target.value)}
                  minLength={10}
                  maxLength={1000}
                  rows={3}
                  disabled={disabled}
                  placeholder="说明调整依据和影响范围（至少 10 个字符）…"
                  aria-invalid={Boolean(validationError)}
                />
                <FieldError>{validationError}</FieldError>
              </Field>
            ) : null}

            {mutation.isSuccess ? (
              <Alert>
                <CircleCheckIcon />
                <AlertTitle>签到设置已签发</AlertTitle>
                <AlertDescription>
                  新规则将在 {formatDateTime(mutation.data.effective_from)}
                  自动生效。
                </AlertDescription>
              </Alert>
            ) : null}
            {mutation.isError ? (
              <Alert variant="destructive">
                <CircleAlertIcon />
                <AlertTitle>签到设置未保存</AlertTitle>
                <AlertDescription>
                  {requestErrorDescription(
                    mutation.error,
                    "请刷新页面后重试。"
                  )}
                </AlertDescription>
              </Alert>
            ) : null}
          </FieldGroup>
        </form>
      </CardContent>
      {canIssue ? (
        <CardFooter className="justify-end">
          <Button
            type="submit"
            form="attendance-policy-form"
            disabled={mutation.isPending}
          >
            {mutation.isPending ? (
              <Spinner data-icon="inline-start" />
            ) : (
              <SaveIcon data-icon="inline-start" />
            )}
            {mutation.isPending ? "保存中…" : "保存并按期生效"}
          </Button>
        </CardFooter>
      ) : null}
    </Card>
  )
}

function SwitchField({
  id,
  title,
  description,
  checked,
  onCheckedChange,
  disabled,
}: {
  id: string
  title: string
  description: string
  checked: boolean
  onCheckedChange: (value: boolean) => void
  disabled: boolean
}) {
  return (
    <Field
      orientation="horizontal"
      className="rounded-lg border bg-muted/15 p-3"
      data-disabled={disabled}
    >
      <FieldContent>
        <FieldLabel htmlFor={id}>{title}</FieldLabel>
        <FieldDescription>{description}</FieldDescription>
      </FieldContent>
      <Switch
        id={id}
        checked={checked}
        onCheckedChange={onCheckedChange}
        disabled={disabled}
      />
    </Field>
  )
}

function RewardModeCard({
  title,
  description,
  enabled,
  onEnabledChange,
  disabled,
  children,
}: {
  title: string
  description: string
  enabled: boolean
  onEnabledChange: (value: boolean) => void
  disabled: boolean
  children: React.ReactNode
}) {
  return (
    <div className="rounded-lg border p-4">
      <div className="flex items-start justify-between gap-4">
        <div>
          <FieldTitle>{title}</FieldTitle>
          <FieldDescription>{description}</FieldDescription>
        </div>
        <Switch
          checked={enabled}
          onCheckedChange={onEnabledChange}
          disabled={disabled}
          aria-label={title}
        />
      </div>
      <div className="mt-4">{children}</div>
    </div>
  )
}

function NumberField({
  id,
  label,
  value,
  onChange,
  disabled,
  description,
}: {
  id: string
  label: string
  value: string
  onChange: (value: string) => void
  disabled: boolean
  description?: string
}) {
  return (
    <Field data-disabled={disabled}>
      <FieldLabel htmlFor={id}>{label}</FieldLabel>
      <Input
        id={id}
        type="number"
        min={0}
        max={1_000_000}
        step={1}
        inputMode="numeric"
        value={value}
        onChange={(event) => onChange(event.target.value)}
        disabled={disabled}
      />
      {description ? <FieldDescription>{description}</FieldDescription> : null}
    </Field>
  )
}

function PolicyHistory({ items }: { items: AttendancePolicy[] }) {
  return (
    <Card className="gap-0 py-0">
      <CardHeader className="p-6 pb-4">
        <CardTitle>签到设置历史</CardTitle>
        <CardDescription>
          每次保存都会追加新版本，历史结算始终使用当时版本。
        </CardDescription>
      </CardHeader>
      <CardContent className="p-0">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>生效时间</TableHead>
              <TableHead>状态</TableHead>
              <TableHead>奖励方式</TableHead>
              <TableHead>连续奖励</TableHead>
              <TableHead>原因</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {items.map((item) => (
              <TableRow key={item.revision}>
                <TableCell>{formatDateTime(item.effective_from)}</TableCell>
                <TableCell>
                  <Badge
                    variant={item.settings.enabled ? "outline" : "secondary"}
                  >
                    {item.settings.enabled ? "开放" : "关闭"}
                  </Badge>
                </TableCell>
                <TableCell>
                  {[
                    item.settings.fixed_enabled
                      ? `固定 ${formatInteger(item.settings.fixed_reward)}`
                      : "",
                    item.settings.random_enabled
                      ? `随机 ${formatInteger(item.settings.random_min)}–${formatInteger(item.settings.random_max)}`
                      : "",
                  ]
                    .filter(Boolean)
                    .join(" / ") || "无"}
                </TableCell>
                <TableCell>
                  {item.settings.streak_enabled
                    ? `${item.settings.streak_milestones.length} 档`
                    : "关闭"}
                </TableCell>
                <TableCell className="max-w-72 truncate" title={item.reason}>
                  {item.reason}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </CardContent>
    </Card>
  )
}

function SummaryCard({
  title,
  value,
  description,
  icon,
}: {
  title: string
  value: string
  description: string
  icon: React.ReactNode
}) {
  return (
    <Card size="sm">
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          {icon}
          {title}
        </CardTitle>
        <CardDescription>{description}</CardDescription>
        <CardAction className="font-heading text-lg font-semibold">
          {value}
        </CardAction>
      </CardHeader>
    </Card>
  )
}

function validateSettings(input: {
  enabled: boolean
  fixedEnabled: boolean
  fixedReward: string
  randomEnabled: boolean
  randomMin: string
  randomMax: string
  experienceReward: string
  milestones: MilestoneDraft[]
  reason: string
}) {
  const integer = (value: string) =>
    /^\d+$/.test(value) && Number(value) <= 1_000_000
  if (input.enabled && !input.fixedEnabled && !input.randomEnabled)
    return "开放签到时至少保留一种奖励方式。"
  if (
    input.fixedEnabled &&
    (!integer(input.fixedReward) || Number(input.fixedReward) < 1)
  )
    return "固定奖励必须是 1 到 1,000,000 的整数。"
  if (
    input.randomEnabled &&
    (!integer(input.randomMin) ||
      !integer(input.randomMax) ||
      Number(input.randomMin) < 1 ||
      Number(input.randomMin) > Number(input.randomMax))
  )
    return "随机奖励范围无效，最小值必须不大于最大值。"
  if (!integer(input.experienceReward))
    return "签到经验必须是 0 到 1,000,000 的整数。"
  const days = new Set<number>()
  for (const milestone of input.milestones) {
    if (
      !integer(milestone.days) ||
      Number(milestone.days) < 2 ||
      Number(milestone.days) > 365 ||
      !integer(milestone.reward) ||
      Number(milestone.reward) < 1
    )
      return "连续奖励需填写 2–365 天和正整数魔力值。"
    if (days.has(Number(milestone.days))) return "连续奖励天数不能重复。"
    days.add(Number(milestone.days))
  }
  if (input.reason.trim().length < 10) return "请填写至少 10 个字符的变更原因。"
  return ""
}

function SettingsError({ error, retry }: { error: Error; retry: () => void }) {
  return (
    <StaffPageFrame>
      <Alert variant="destructive">
        <CircleAlertIcon />
        <AlertTitle>签到设置暂时无法读取</AlertTitle>
        <AlertDescription>
          {requestErrorDescription(error, "请检查 Core 与后台会话后重试。")}
        </AlertDescription>
        <Button
          variant="outline"
          size="sm"
          className="mt-2 w-fit"
          onClick={retry}
        >
          <RefreshCwIcon data-icon="inline-start" />
          重试
        </Button>
      </Alert>
    </StaffPageFrame>
  )
}

function SettingsSkeleton() {
  return (
    <StaffPageFrame aria-label="正在加载签到与活动奖励设置">
      <div className="grid gap-3 sm:grid-cols-3">
        {Array.from({ length: 3 }, (_, index) => (
          <Skeleton key={index} className="h-28 rounded-lg" />
        ))}
      </div>
      <Skeleton className="h-[720px] rounded-lg" />
      <Skeleton className="h-64 rounded-lg" />
    </StaffPageFrame>
  )
}

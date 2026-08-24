import * as React from "react"
import { useQuery } from "@tanstack/react-query"
import {
  CalculatorIcon,
  CheckCircle2Icon,
  ChevronDownIcon,
  CircleAlertIcon,
  Clock3Icon,
  CoinsIcon,
  FileClockIcon,
  RefreshCwIcon,
  SaveIcon,
  ShieldCheckIcon,
  SlidersHorizontalIcon,
} from "lucide-react"

import {
  Alert,
  AlertAction,
  AlertDescription,
  AlertTitle,
} from "~/components/ui/alert"
import { Badge } from "~/components/ui/badge"
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
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "~/components/ui/collapsible"
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "~/components/ui/empty"
import {
  Field,
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLabel,
  FieldLegend,
  FieldSet,
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
import { Textarea } from "~/components/ui/textarea"
import {
  type SeedingRewardPolicy,
  type SeedingRewardPolicyInput,
  type SeedingRewardPolicyPreview,
  seedingRewardPolicyListQueryOptions,
  useIssueSeedingRewardPolicy,
  usePreviewSeedingRewardPolicy,
} from "~/features/staff/api/seeding-reward-administration.queries"
import { StaffAccessGate } from "~/features/staff/components/staff-access-gate"
import { StaffPageFrame } from "~/features/staff/components/staff-page-frame"
import { hasCapability } from "~/features/staff/model/capability"
import {
  recommendedSeedingRewardPolicy,
  seedingRewardPolicyDraft,
} from "~/features/staff/model/seeding-reward-policy-preset"
import {
  fromSeedingRewardPolicyUnit,
  toSeedingRewardPolicyUnit,
  type SeedingRewardDisplayUnit,
} from "~/features/economy/model/seeding-reward-policy-units"
import type { components } from "~/generated/api"
import { requestErrorDescription } from "~/shared/api/problem"
import { formatCompactDateTime } from "~/shared/formatters/date-time"
import { formatInteger } from "~/shared/formatters/integer"

type CapabilityList = components["schemas"]["CapabilityList"]
type NumericPolicyKey = Exclude<
  keyof SeedingRewardPolicyInput,
  "revision" | "formula_version" | "effective_from"
>

type ParameterDefinition = {
  key: NumericPolicyKey
  label: string
  description: string
  unit: SeedingRewardDisplayUnit
  min: number
  max: number
  step?: number
}

const primaryParameterGroups: Array<{
  title: string
  description: string
  fields: ParameterDefinition[]
}> = [
  {
    title: "基础奖励公式",
    description: "按资源体积、年龄和做种人数计算每小时基础魔力值。",
    fields: [
      {
        key: "curve_hourly_cap_milli",
        label: "基础奖励上限（魔力/小时）",
        description: "单个用户从资源价值曲线获得的每小时最高奖励。",
        unit: "milli",
        min: 0.001,
        max: 10_000_000,
        step: 0.1,
      },
      {
        key: "age_saturation_seconds",
        label: "种子年龄上限（周）",
        description: "达到这个年龄后，资源的年龄加成不再增长。",
        unit: "weeks",
        min: 0.01,
        max: 521,
        step: 1,
      },
      {
        key: "seeder_decay",
        label: "稀缺奖励系数",
        description: "数值越小，越偏向奖励做种人数少的资源。",
        unit: "integer",
        min: 2,
        max: 1_000,
      },
      {
        key: "curve_scale_milli",
        label: "曲线平滑参数",
        description: "控制体积、年龄和稀缺度组合后增长趋缓的速度。",
        unit: "milli",
        min: 0.001,
        max: 10_000_000_000,
        step: 1,
      },
      {
        key: "size_multiplier_bps",
        label: "体积奖励比例（%）",
        description: "整体提高或降低资源体积在基础奖励中的权重。",
        unit: "percent",
        min: 0.01,
        max: 1_000,
        step: 1,
      },
      {
        key: "official_bonus_bps",
        label: "官种额外加成（%）",
        description: "被标记为官种的资源获得的额外奖励。",
        unit: "percent",
        min: 0,
        max: 1_000,
        step: 1,
      },
      {
        key: "upload_contribution_bonus_bps",
        label: "正在供源加成（%）",
        description: "用户对自己发布的资源持续提供上传时获得的加成。",
        unit: "percent",
        min: 0,
        max: 1_000,
        step: 1,
      },
    ],
  },
  {
    title: "按种子数量奖励",
    description: "为每个符合条件的做种任务提供稳定奖励，并限制计算数量。",
    fields: [
      {
        key: "per_torrent_hourly_milli",
        label: "每种固定奖励（魔力/小时）",
        description: "每个符合条件的种子每小时获得的固定奖励。",
        unit: "milli",
        min: 0,
        max: 10_000_000,
        step: 0.1,
      },
      {
        key: "base_linear_torrent_limit",
        label: "最多计算做种数量",
        description: "未计算等级权益前，每小时最多计入多少个种子。",
        unit: "integer",
        min: 0,
        max: 100_000,
      },
      {
        key: "minimum_torrent_bytes",
        label: "最小种子体积（GB）",
        description: "小于该体积的资源不进入做种奖励计算。",
        unit: "gibibytes",
        min: 0,
        max: 8_388_607,
        step: 0.01,
      },
    ],
  },
  {
    title: "VIP、经验与总上限",
    description: "设置常用权益加成，并限制每名用户每小时的总奖励。",
    fields: [
      {
        key: "vip_bonus_bps",
        label: "VIP 做种加成（%）",
        description: "用户 VIP 有效时应用的额外做种奖励。",
        unit: "percent",
        min: 0,
        max: 1_000,
        step: 1,
      },
      {
        key: "maximum_hourly_reward",
        label: "每人总奖励上限（魔力/小时）",
        description: "基础、数量与权益加成合计后的最终上限。",
        unit: "integer",
        min: 1,
        max: 1_000_000_000,
      },
      {
        key: "experience_per_magic_bps",
        label: "每 1 魔力获得经验",
        description: "例如 0.1 表示每获得 1 魔力值，同时获得 0.1 经验。",
        unit: "ratio",
        min: 0,
        max: 10,
        step: 0.01,
      },
    ],
  },
]

const advancedParameterGroup: {
  title: string
  description: string
  fields: ParameterDefinition[]
} = {
  title: "资格与权益边界",
  description:
    "这些限制通常保持默认值，只在 Tracker 采样或权益规则变化时调整。",
  fields: [
    {
      key: "maximum_level_torrent_bonus",
      label: "等级最多增加做种数量",
      description: "等级权益最多可以在基础数量上限之外增加多少个种子。",
      unit: "integer",
      min: 0,
      max: 100_000,
    },
    {
      key: "minimum_active_seconds",
      label: "小时内最少活跃（分钟）",
      description: "低于该活跃时长的做种证据不计入本小时奖励。",
      unit: "minutes",
      min: 0.02,
      max: 60,
      step: 1,
    },
    {
      key: "maximum_snapshot_age_seconds",
      label: "Tracker 状态最长延迟（分钟）",
      description: "超过该延迟的做种状态不会用于奖励计算。",
      unit: "minutes",
      min: 0.02,
      max: 1_440,
      step: 1,
    },
    {
      key: "maximum_medal_bonus_bps",
      label: "勋章加成上限（%）",
      description: "所有有效勋章叠加后允许应用的最高加成。",
      unit: "percent",
      min: 0,
      max: 1_000,
      step: 1,
    },
    {
      key: "maximum_level_bonus_bps",
      label: "等级加成上限（%）",
      description: "用户等级权益允许应用的最高做种奖励加成。",
      unit: "percent",
      min: 0,
      max: 1_000,
      step: 1,
    },
  ],
}

export function StaffSeedingRewardsPage() {
  return (
    <StaffAccessGate
      requiredAction="economy.seedingreward.policy.read"
      pageHeader={{
        title: "做种奖励",
        description: "设置做种魔力值、VIP 加成和经验获取规则。",
      }}
    >
      {({ session, capabilities }) => (
        <SeedingRewardsContent
          csrfToken={session.csrf_token}
          capabilities={capabilities}
        />
      )}
    </StaffAccessGate>
  )
}

function SeedingRewardsContent({
  csrfToken,
  capabilities,
}: {
  csrfToken: string
  capabilities: CapabilityList
}) {
  const policies = useQuery(seedingRewardPolicyListQueryOptions())
  if (policies.isPending) return <SeedingRewardsSkeleton />
  if (policies.isError || !policies.data) {
    return (
      <StaffPageFrame>
        <Alert variant="destructive">
          <CircleAlertIcon />
          <AlertTitle>做种奖励政策暂时无法读取</AlertTitle>
          <AlertDescription>
            {requestErrorDescription(policies.error, "请稍后重试。")}
          </AlertDescription>
          <AlertAction>
            <Button
              variant="outline"
              size="sm"
              onClick={() => void policies.refetch()}
            >
              <RefreshCwIcon data-icon="inline-start" />
              重试
            </Button>
          </AlertAction>
        </Alert>
      </StaffPageFrame>
    )
  }

  return (
    <SeedingRewardPolicyEditor
      page={policies.data}
      csrfToken={csrfToken}
      canIssue={hasCapability(
        capabilities,
        "economy.seedingreward.policy.issue"
      )}
    />
  )
}

function SeedingRewardPolicyEditor({
  page,
  csrfToken,
  canIssue,
}: {
  page: components["schemas"]["SeedingRewardPolicyPage"]
  csrfToken: string
  canIssue: boolean
}) {
  const latest = page.items[0]
  const [policy, setPolicy] = React.useState<SeedingRewardPolicyInput>(() =>
    seedingRewardPolicyDraft(latest, page.minimum_effective_from)
  )
  const [reason, setReason] = React.useState("")
  const [preview, setPreview] = React.useState<SeedingRewardPolicyPreview>()
  const [success, setSuccess] = React.useState("")
  const previewMutation = usePreviewSeedingRewardPolicy()
  const issueMutation = useIssueSeedingRewardPolicy()
  const reasonError = [...reason.trim()].length > 1000
  const disabled = previewMutation.isPending || issueMutation.isPending

  function updatePolicy<K extends keyof SeedingRewardPolicyInput>(
    key: K,
    value: SeedingRewardPolicyInput[K]
  ) {
    setPolicy((current) => ({ ...current, [key]: value }))
    setPreview(undefined)
    setSuccess("")
    previewMutation.reset()
    issueMutation.reset()
  }

  function applyRecommendedPolicy() {
    setPolicy((current) =>
      recommendedSeedingRewardPolicy(current.revision, current.effective_from)
    )
    setPreview(undefined)
    setSuccess("")
    previewMutation.reset()
    issueMutation.reset()
  }

  async function handlePreview() {
    setSuccess("")
    try {
      setPreview(await previewMutation.mutateAsync(policy))
    } catch {
      setPreview(undefined)
    }
  }

  async function handleIssue() {
    if (!preview || reasonError || !canIssue) return
    try {
      const issued = await issueMutation.mutateAsync({
        csrfToken,
        policy,
        reason: reason.trim(),
      })
      setSuccess(
        `做种奖励设置已保存，将于 ${formatCompactDateTime(issued.effective_from)} 生效。`
      )
      setPreview(undefined)
      setReason("")
    } catch {
      // Mutation state renders the actionable error below.
    }
  }

  return (
    <StaffPageFrame className="gap-4">
      <header className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h1 className="font-heading text-xl font-semibold">做种奖励</h1>
          <p className="text-sm text-muted-foreground">
            按种子体积、年龄、稀缺度、做种数量和用户权益计算魔力值。
          </p>
        </div>
        <Badge variant="outline">已调整 {formatInteger(page.total)} 次</Badge>
      </header>

      <Alert>
        <Clock3Icon />
        <AlertTitle>
          本次调整最早可于 {formatCompactDateTime(page.minimum_effective_from)}{" "}
          生效
        </AlertTitle>
        <AlertDescription>
          调整只影响生效时间之后的做种奖励，不会改动已经入账的魔力值和经验。
        </AlertDescription>
      </Alert>

      {success ? (
        <Alert>
          <CheckCircle2Icon />
          <AlertTitle>设置已保存</AlertTitle>
          <AlertDescription>{success}</AlertDescription>
        </Alert>
      ) : null}

      <div className="grid items-start gap-4 2xl:grid-cols-[minmax(0,1fr)_25rem]">
        <Card>
          <CardHeader>
            <CardTitle>奖励参数</CardTitle>
            <CardDescription>
              推荐值沿用 Rousi 当前奖励曲线，并增加 PeerGo
              的做种证据和总额保护；保存时会转换为精确账本参数。
            </CardDescription>
            <CardAction>
              <Button
                type="button"
                variant="outline"
                size="sm"
                disabled={disabled}
                onClick={applyRecommendedPolicy}
              >
                <RefreshCwIcon data-icon="inline-start" />
                恢复推荐值
              </Button>
            </CardAction>
          </CardHeader>
          <CardContent>
            <FieldGroup>
              {primaryParameterGroups.map((group) => (
                <FieldSet key={group.title} className="rounded-lg border p-4">
                  <FieldLegend>{group.title}</FieldLegend>
                  <FieldDescription>{group.description}</FieldDescription>
                  <FieldGroup className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
                    {group.fields.map((definition) => (
                      <NumericPolicyField
                        key={definition.key}
                        definition={definition}
                        value={policy[definition.key]}
                        disabled={disabled}
                        onChange={(value) =>
                          updatePolicy(definition.key, value)
                        }
                      />
                    ))}
                  </FieldGroup>
                </FieldSet>
              ))}

              <AdvancedPolicySettings
                page={page}
                policy={policy}
                disabled={disabled}
                updatePolicy={updatePolicy}
              />
            </FieldGroup>
          </CardContent>
        </Card>

        <div className="grid gap-4 2xl:sticky 2xl:top-20">
          <Card>
            <CardHeader>
              <CardTitle>保存前检查</CardTitle>
              <CardDescription>
                先查看代表场景的奖励结果，确认后再保存设置。
              </CardDescription>
              <CardAction>
                <CalculatorIcon className="text-muted-foreground" />
              </CardAction>
            </CardHeader>
            <CardContent className="grid gap-4">
              <Button
                variant="outline"
                disabled={disabled}
                onClick={() => void handlePreview()}
              >
                {previewMutation.isPending ? (
                  <Spinner />
                ) : (
                  <CalculatorIcon data-icon="inline-start" />
                )}
                {previewMutation.isPending ? "计算中…" : "预览奖励效果"}
              </Button>

              {previewMutation.isError ? (
                <Alert variant="destructive">
                  <CircleAlertIcon />
                  <AlertTitle>预览失败</AlertTitle>
                  <AlertDescription>
                    {requestErrorDescription(
                      previewMutation.error,
                      "请核对生效时间和参数范围。"
                    )}
                  </AlertDescription>
                </Alert>
              ) : null}

              {preview ? <PreviewResults preview={preview} /> : null}

              <Field data-invalid={reasonError}>
                <FieldLabel htmlFor="reward-policy-reason">调整说明</FieldLabel>
                <Textarea
                  id="reward-policy-reason"
                  rows={4}
                  maxLength={1000}
                  value={reason}
                  disabled={disabled || !canIssue}
                  placeholder="可留空；系统会自动记录调整说明"
                  onChange={(event) => setReason(event.target.value)}
                />
                <FieldDescription>
                  调整说明会随本次设置保存，方便以后核对奖励账目。
                </FieldDescription>
                <FieldError
                  errors={
                    reasonError
                      ? [{ message: "调整说明不能超过 1000 个字符。" }]
                      : []
                  }
                />
              </Field>

              {!canIssue ? (
                <Alert>
                  <ShieldCheckIcon />
                  <AlertTitle>当前账号只能读取与预览</AlertTitle>
                  <AlertDescription>
                    保存设置需要做种奖励管理权限。
                  </AlertDescription>
                </Alert>
              ) : null}

              {issueMutation.isError ? (
                <Alert variant="destructive">
                  <CircleAlertIcon />
                  <AlertTitle>做种奖励设置保存失败</AlertTitle>
                  <AlertDescription>
                    {requestErrorDescription(
                      issueMutation.error,
                      "请重新读取当前设置后再试。"
                    )}
                  </AlertDescription>
                </Alert>
              ) : null}

              <Button
                disabled={!canIssue || !preview || reasonError || disabled}
                onClick={() => void handleIssue()}
              >
                {issueMutation.isPending ? (
                  <Spinner />
                ) : (
                  <SaveIcon data-icon="inline-start" />
                )}
                {issueMutation.isPending ? "保存中…" : "保存奖励设置"}
              </Button>
            </CardContent>
          </Card>
        </div>
      </div>

      <PolicyTimeline policies={page.items} />
    </StaffPageFrame>
  )
}

function AdvancedPolicySettings({
  page,
  policy,
  disabled,
  updatePolicy,
}: {
  page: components["schemas"]["SeedingRewardPolicyPage"]
  policy: SeedingRewardPolicyInput
  disabled: boolean
  updatePolicy: <K extends keyof SeedingRewardPolicyInput>(
    key: K,
    value: SeedingRewardPolicyInput[K]
  ) => void
}) {
  return (
    <Collapsible>
      <CollapsibleTrigger
        render={
          <Button
            variant="outline"
            type="button"
            className="w-full justify-start"
          />
        }
      >
        <SlidersHorizontalIcon data-icon="inline-start" />
        高级设置
        <ChevronDownIcon className="ml-auto transition-transform in-data-open:rotate-180" />
      </CollapsibleTrigger>
      <CollapsibleContent className="pt-4">
        <FieldGroup>
          <FieldSet className="rounded-lg border p-4">
            <FieldLegend>生效安排</FieldLegend>
            <FieldDescription>
              系统会自动生成唯一账务标识，管理员只需要选择生效时间。
            </FieldDescription>
            <FieldGroup className="grid gap-4 md:grid-cols-2">
              <Field>
                <FieldLabel htmlFor="reward-policy-effective">
                  生效时间
                </FieldLabel>
                <Input
                  id="reward-policy-effective"
                  type="datetime-local"
                  min={toDateTimeLocal(page.minimum_effective_from)}
                  value={toDateTimeLocal(policy.effective_from)}
                  disabled={disabled}
                  onChange={(event) => {
                    const value = new Date(event.target.value)
                    if (!Number.isNaN(value.valueOf()))
                      updatePolicy("effective_from", value.toISOString())
                  }}
                />
                <FieldDescription>
                  按本地时间填写，系统会对齐到可结算的整点。
                </FieldDescription>
              </Field>
              <Field>
                <FieldLabel>计算方式</FieldLabel>
                <Input value="Rousi 兼容做种奖励曲线" readOnly disabled />
                <FieldDescription>
                  公式实现由系统维护，后台只调整业务参数。
                </FieldDescription>
              </Field>
            </FieldGroup>
          </FieldSet>

          <FieldSet className="rounded-lg border p-4">
            <FieldLegend>{advancedParameterGroup.title}</FieldLegend>
            <FieldDescription>
              {advancedParameterGroup.description}
            </FieldDescription>
            <FieldGroup className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
              {advancedParameterGroup.fields.map((definition) => (
                <NumericPolicyField
                  key={definition.key}
                  definition={definition}
                  value={policy[definition.key]}
                  disabled={disabled}
                  onChange={(value) => updatePolicy(definition.key, value)}
                />
              ))}
            </FieldGroup>
          </FieldSet>
        </FieldGroup>
      </CollapsibleContent>
    </Collapsible>
  )
}

function NumericPolicyField({
  definition,
  value,
  disabled,
  onChange,
}: {
  definition: ParameterDefinition
  value: number
  disabled: boolean
  onChange: (value: number) => void
}) {
  const id = `reward-policy-${definition.key}`
  const displayValue = fromSeedingRewardPolicyUnit(value, definition.unit)
  return (
    <Field>
      <FieldLabel htmlFor={id}>{definition.label}</FieldLabel>
      <Input
        id={id}
        type="number"
        min={definition.min}
        max={definition.max}
        step={definition.step ?? 1}
        value={displayValue}
        disabled={disabled}
        onChange={(event) =>
          onChange(
            toSeedingRewardPolicyUnit(
              Number(event.target.value),
              definition.unit
            )
          )
        }
      />
      <FieldDescription className="text-xs">
        {definition.description}
      </FieldDescription>
    </Field>
  )
}

function PreviewResults({ preview }: { preview: SeedingRewardPolicyPreview }) {
  return (
    <div className="grid gap-2">
      {preview.results.map((result) => (
        <div key={result.name} className="rounded-lg border bg-muted/20 p-3">
          <div className="flex items-center justify-between gap-2">
            <span className="font-medium">{result.name}</span>
            <span className="font-medium tabular-nums">
              {formatInteger(result.reward)} 魔力
            </span>
          </div>
          <p className="text-xs text-muted-foreground">{result.description}</p>
          <div className="flex flex-wrap gap-2 text-xs text-muted-foreground">
            <span>{result.eligible_torrent_count} 个合格种子</span>
            <span>经验 {result.experience_amount}</span>
            {result.capped ? (
              <Badge variant="secondary">触发硬上限</Badge>
            ) : null}
          </div>
        </div>
      ))}
    </div>
  )
}

function PolicyTimeline({ policies }: { policies: SeedingRewardPolicy[] }) {
  return (
    <Collapsible>
      <Card className="gap-0 py-0">
        <CardHeader className="p-6">
          <CardTitle className="flex items-center gap-2">
            <FileClockIcon data-icon="inline-start" />
            变更记录与审计
          </CardTitle>
          <CardDescription>
            查看历次生效时间、调整说明和用于账目复核的摘要。
          </CardDescription>
          <CardAction>
            <CollapsibleTrigger
              render={<Button variant="outline" size="sm" type="button" />}
            >
              查看记录
              <ChevronDownIcon
                data-icon="inline-end"
                className="transition-transform in-data-open:rotate-180"
              />
            </CollapsibleTrigger>
          </CardAction>
        </CardHeader>
        <CollapsibleContent>
          <CardContent className="p-0">
            {policies.length === 0 ? (
              <Empty>
                <EmptyHeader>
                  <EmptyMedia variant="icon">
                    <CoinsIcon />
                  </EmptyMedia>
                  <EmptyTitle>尚未保存做种奖励设置</EmptyTitle>
                  <EmptyDescription>
                    先填写并预览首个设置，再保存到未来生效时间。
                  </EmptyDescription>
                </EmptyHeader>
              </Empty>
            ) : (
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>规则</TableHead>
                    <TableHead>生效时间</TableHead>
                    <TableHead>每小时总上限</TableHead>
                    <TableHead>每魔力经验</TableHead>
                    <TableHead>调整说明</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {policies.map((item, index) => (
                    <TableRow key={item.snapshot_sha256}>
                      <TableCell>
                        <div className="flex items-center gap-2 font-medium">
                          {item.issued_by ? "管理员规则" : "系统首版"}
                          {index === 0 ? (
                            <Badge variant="secondary">最新</Badge>
                          ) : null}
                        </div>
                      </TableCell>
                      <TableCell>
                        {formatCompactDateTime(item.effective_from)}
                      </TableCell>
                      <TableCell>
                        {formatInteger(item.maximum_hourly_reward)} 魔力
                      </TableCell>
                      <TableCell>
                        {formatExperienceRatio(item.experience_per_magic_bps)}
                      </TableCell>
                      <TableCell className="max-w-80 whitespace-normal text-muted-foreground">
                        {item.reason}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            )}
          </CardContent>
        </CollapsibleContent>
      </Card>
    </Collapsible>
  )
}

function toDateTimeLocal(value: string) {
  const date = new Date(value)
  const local = new Date(date.valueOf() - date.getTimezoneOffset() * 60_000)
  return local.toISOString().slice(0, 16)
}

function formatExperienceRatio(value: number) {
  return new Intl.NumberFormat("zh-CN", {
    maximumFractionDigits: 4,
  }).format(fromSeedingRewardPolicyUnit(value, "ratio"))
}

function SeedingRewardsSkeleton() {
  return (
    <StaffPageFrame aria-label="正在加载做种奖励政策">
      <Skeleton className="h-20 rounded-lg" />
      <div className="grid gap-4 2xl:grid-cols-[minmax(0,1fr)_25rem]">
        <Skeleton className="h-[70rem] rounded-lg" />
        <Skeleton className="h-[32rem] rounded-lg" />
      </div>
      <Skeleton className="h-80 rounded-lg" />
    </StaffPageFrame>
  )
}

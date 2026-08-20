import { useQuery } from "@tanstack/react-query"
import {
  CalendarClockIcon,
  CalendarDaysIcon,
  ChartNoAxesColumnIncreasingIcon,
  CircleAlertIcon,
  FilePlus2Icon,
  RefreshCwIcon,
  ShieldCheckIcon,
  UploadIcon,
  UsersRoundIcon,
} from "lucide-react"

import { Alert, AlertDescription, AlertTitle } from "~/components/ui/alert"
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
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "~/components/ui/empty"
import { Skeleton } from "~/components/ui/skeleton"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "~/components/ui/table"
import {
  contributionExperiencePolicyListQueryOptions,
  levelPolicyListQueryOptions,
  type ContributionExperiencePolicy,
} from "~/features/staff/api/seeding-reward-administration.queries"
import { ContributionExperiencePolicyDialog } from "~/features/staff/components/contribution-experience-policy-dialog"
import { LevelPolicyDialog } from "~/features/staff/components/level-policy-dialog"
import { StaffAccessGate } from "~/features/staff/components/staff-access-gate"
import { StaffPageFrame } from "~/features/staff/components/staff-page-frame"
import { hasCapability } from "~/features/staff/model/capability"
import type { components } from "~/generated/api"
import { formatDateTime } from "~/shared/formatters/date-time"
import { MetricCard } from "~/shared/components/metric-card"
import { formatInteger } from "~/shared/formatters/integer"

type CapabilityList = components["schemas"]["CapabilityList"]

export function StaffLevelPoliciesPage() {
  return (
    <StaffAccessGate
      requiredAction="progression.level.policy.read"
      pageHeader={{
        title: "经验与等级",
        description: "查看用户升级所需经验，以及每一级实际获得的做种权益。",
      }}
    >
      {({ session, capabilities }) => (
        <LevelPoliciesContent
          csrfToken={session.csrf_token}
          capabilities={capabilities}
        />
      )}
    </StaffAccessGate>
  )
}

function LevelPoliciesContent({
  csrfToken,
  capabilities,
}: {
  csrfToken: string
  capabilities: CapabilityList
}) {
  const policies = useQuery(levelPolicyListQueryOptions())
  const contributions = useQuery(contributionExperiencePolicyListQueryOptions())
  if (policies.isPending || contributions.isPending)
    return <LevelPoliciesSkeleton />
  if (
    policies.isError ||
    !policies.data ||
    contributions.isError ||
    !contributions.data
  ) {
    return (
      <StaffPageFrame className="gap-4">
        <Alert variant="destructive">
          <CircleAlertIcon />
          <AlertTitle>经验与等级规则暂时无法读取</AlertTitle>
          <AlertDescription>
            请检查 Core 数据库与后台会话后重试。
          </AlertDescription>
        </Alert>
        <Button
          variant="outline"
          className="w-fit"
          onClick={() => {
            void policies.refetch()
            void contributions.refetch()
          }}
        >
          <RefreshCwIcon data-icon="inline-start" />
          重试
        </Button>
      </StaffPageFrame>
    )
  }

  const totalUsers = policies.data.items.reduce(
    (sum, policy) => sum + BigInt(policy.user_count),
    0n
  )
  const latest = policies.data.items.reduce<
    (typeof policies.data.items)[number] | undefined
  >(
    (current, item) =>
      !current || item.sequence > current.sequence ? item : current,
    undefined
  )
  const scheduledCount = policies.data.items.filter(
    (policy) => policy.activation_status === "scheduled"
  ).length
  const canIssue = hasCapability(capabilities, "progression.level.policy.issue")
  const canIssueContributions = hasCapability(
    capabilities,
    "progression.contribution.policy.issue"
  )
  const currentContribution = contributions.data.items.find(
    (policy) => new Date(policy.effective_from) <= new Date()
  )

  return (
    <StaffPageFrame className="gap-4">
      <header className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h1 className="font-heading text-xl font-semibold">经验与等级</h1>
          <p className="mt-1 text-sm text-muted-foreground">
            以下内容直接来自实际等级表，不是前端默认值。
          </p>
        </div>
        <div className="flex flex-wrap gap-2">
          <ContributionExperiencePolicyDialog
            policies={contributions.data}
            csrfToken={csrfToken}
            disabled={!canIssueContributions}
          />
          {policies.data.items.length ? (
            <LevelPolicyDialog
              policies={policies.data}
              csrfToken={csrfToken}
              disabled={!canIssue}
            />
          ) : null}
          <Button
            variant="outline"
            size="sm"
            disabled={policies.isFetching || contributions.isFetching}
            onClick={() => {
              void policies.refetch()
              void contributions.refetch()
            }}
          >
            <RefreshCwIcon
              data-icon="inline-start"
              className={
                policies.isFetching || contributions.isFetching
                  ? "animate-spin"
                  : undefined
              }
            />
            刷新
          </Button>
        </div>
      </header>

      <div className="grid gap-3 sm:grid-cols-3">
        <MetricCard
          title="等级版本"
          value={formatInteger(policies.data.items.length)}
          description="历史版本会保留"
          icon={<ShieldCheckIcon />}
          tone="primary"
        />
        <MetricCard
          title="等级档位"
          value={formatInteger(latest?.levels.length ?? 0)}
          description="最新完整规则"
          icon={<ChartNoAxesColumnIncreasingIcon />}
          tone="default"
        />
        <MetricCard
          title={scheduledCount > 0 ? "待生效版本" : "已纳入用户"}
          value={
            scheduledCount > 0
              ? formatInteger(scheduledCount)
              : formatInteger(totalUsers)
          }
          description={scheduledCount > 0 ? "等待整点自动生效" : "已按规则定级"}
          icon={scheduledCount > 0 ? <CalendarClockIcon /> : <UsersRoundIcon />}
          tone={scheduledCount > 0 ? "warning" : "positive"}
        />
      </div>

      <ContributionExperienceCard policy={currentContribution} />

      <Alert>
        <ShieldCheckIcon />
        <AlertTitle>等级规则按版本保留</AlertTitle>
        <AlertDescription>
          修改会新增完整版本并安排整点生效；Core
          会按用户现有经验统一重算等级，不改经验账本，也不重复发放升级奖励。
        </AlertDescription>
      </Alert>

      {policies.data.items.length === 0 ? (
        <Card>
          <CardContent>
            <Empty>
              <EmptyHeader>
                <EmptyMedia variant="icon">
                  <ChartNoAxesColumnIncreasingIcon />
                </EmptyMedia>
                <EmptyTitle>尚未建立等级规则</EmptyTitle>
                <EmptyDescription>请先完成初始等级政策部署。</EmptyDescription>
              </EmptyHeader>
            </Empty>
          </CardContent>
        </Card>
      ) : (
        policies.data.items.map((policy) => (
          <Card key={policy.policy_version} className="gap-0 py-0">
            <CardHeader className="p-6 pb-4">
              <div className="flex flex-wrap items-start justify-between gap-3">
                <div>
                  <CardTitle>
                    等级规则 #{formatInteger(policy.sequence)}
                  </CardTitle>
                  <CardDescription>
                    {policy.activation_status === "scheduled"
                      ? `${formatDateTime(policy.effective_at)} 自动生效，届时全站重算等级`
                      : `${formatInteger(policy.user_count)} 名用户当前使用，生效于 ${formatDateTime(policy.effective_at)}`}
                  </CardDescription>
                </div>
                <Badge
                  variant={
                    policy.activation_status === "scheduled"
                      ? "secondary"
                      : "outline"
                  }
                >
                  {policy.activation_status === "scheduled"
                    ? "待生效"
                    : "已生效"}
                </Badge>
              </div>
              <p className="text-sm text-muted-foreground">{policy.reason}</p>
              {policy.activation_status === "applied" &&
              BigInt(policy.affected_user_count) > 0n ? (
                <p className="text-xs text-muted-foreground">
                  生效时检查 {formatInteger(policy.affected_user_count)}{" "}
                  名用户，其中 {formatInteger(policy.changed_level_count)}{" "}
                  名等级发生变化。
                </p>
              ) : null}
            </CardHeader>
            <CardContent className="p-0">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>等级</TableHead>
                    <TableHead className="text-right">最低经验</TableHead>
                    <TableHead className="text-right">做种魔力加成</TableHead>
                    <TableHead className="text-right">额外计奖种子</TableHead>
                    <TableHead className="text-right">当前用户</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {policy.levels.map((level) => (
                    <TableRow key={level.level}>
                      <TableCell className="font-medium">
                        Lv.{level.level}
                      </TableCell>
                      <TableCell className="text-right tabular-nums">
                        {formatExperience(level.minimum_experience)}
                      </TableCell>
                      <TableCell className="text-right tabular-nums">
                        {formatPercent(level.karma_bonus_bps)}
                      </TableCell>
                      <TableCell className="text-right tabular-nums">
                        +{formatInteger(level.seeding_count_bonus)}
                      </TableCell>
                      <TableCell className="text-right tabular-nums">
                        {formatInteger(level.current_user_count)}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </CardContent>
          </Card>
        ))
      )}
    </StaffPageFrame>
  )
}

function ContributionExperienceCard({
  policy,
}: {
  policy: ContributionExperiencePolicy | undefined
}) {
  const sources = policy
    ? [
        {
          label: "每实际上传 1 GiB",
          value: `+${formatMilliExperience(policy.experience_per_upload_gib_milli)}`,
          icon: <UploadIcon />,
        },
        {
          label: "每发布 1 个种子",
          value: `+${formatMilliExperience(policy.experience_per_torrent_milli)}`,
          icon: <FilePlus2Icon />,
        },
        {
          label: "账号每存续 1 天",
          value: `+${formatMilliExperience(policy.experience_per_account_day_milli)}`,
          icon: <CalendarDaysIcon />,
        },
      ]
    : []
  return (
    <Card>
      <CardHeader>
        <CardTitle>经验获取基线</CardTitle>
        <CardDescription>
          做种经验在“做种奖励”中配置，签到经验在“签到设置”中配置；这里管理其余三项。
        </CardDescription>
        {policy ? (
          <CardAction>
            <Badge variant="outline">
              {policy.issued_by ? "管理员版本" : "系统首版"}
            </Badge>
          </CardAction>
        ) : null}
      </CardHeader>
      <CardContent>
        {policy ? (
          <div className="grid gap-3 sm:grid-cols-3">
            {sources.map((source) => (
              <div
                key={source.label}
                className="flex items-center gap-3 rounded-lg border bg-muted/20 p-4"
              >
                <div className="flex size-9 shrink-0 items-center justify-center rounded-full bg-background text-muted-foreground [&>svg]:size-4">
                  {source.icon}
                </div>
                <div className="min-w-0">
                  <p className="text-sm text-muted-foreground">
                    {source.label}
                  </p>
                  <p className="font-medium tabular-nums">
                    {source.value} 经验
                  </p>
                </div>
              </div>
            ))}
          </div>
        ) : (
          <p className="text-sm text-muted-foreground">
            当前没有已经生效的经验获取基线；上线前检查会阻止这种状态进入生产环境。
          </p>
        )}
      </CardContent>
    </Card>
  )
}

function formatExperience(value: string) {
  const [integer, fraction = ""] = value.split(".")
  const decimal = fraction.replace(/0+$/, "")
  const formattedInteger = new Intl.NumberFormat("zh-CN").format(
    BigInt(integer)
  )
  return decimal ? `${formattedInteger}.${decimal}` : formattedInteger
}

function formatPercent(basisPoints: number) {
  return `${new Intl.NumberFormat("zh-CN", { maximumFractionDigits: 2 }).format(basisPoints / 100)}%`
}

function formatMilliExperience(value: number) {
  return new Intl.NumberFormat("zh-CN", {
    maximumFractionDigits: 3,
  }).format(value / 1_000)
}

function LevelPoliciesSkeleton() {
  return (
    <StaffPageFrame aria-label="正在加载经验与等级规则">
      <div className="grid gap-3 sm:grid-cols-3">
        {Array.from({ length: 3 }, (_, index) => (
          <Skeleton key={index} className="h-36 rounded-lg" />
        ))}
      </div>
      <Skeleton className="h-20 rounded-lg" />
      <Skeleton className="h-96 rounded-lg" />
    </StaffPageFrame>
  )
}

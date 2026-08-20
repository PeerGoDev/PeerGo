import { Badge } from "~/components/ui/badge"
import type { ReactNode } from "react"
import {
  Progress,
  ProgressLabel,
  ProgressValue,
} from "~/components/ui/progress"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "~/components/ui/table"
import type { WorkgroupContributionCycle } from "~/features/workgroups/api/workgroups.queries"
import {
  contributionMetricLabel,
  contributionPercent,
  formatContributionValue,
} from "~/features/workgroups/model/contribution-format"
import { formatDateTime } from "~/shared/formatters/date-time"

export function ContributionCycleHistory({
  items,
  renderAction,
}: {
  items: WorkgroupContributionCycle[]
  renderAction?: (cycle: WorkgroupContributionCycle) => ReactNode
}) {
  return (
    <div className="overflow-x-auto rounded-md border">
      <Table className="min-w-3xl">
        <TableHeader>
          <TableRow>
            <TableHead>月份</TableHead>
            <TableHead className="min-w-56">贡献</TableHead>
            <TableHead>成员覆盖</TableHead>
            <TableHead>证据</TableHead>
            <TableHead>评估</TableHead>
            {renderAction ? <TableHead>操作</TableHead> : null}
          </TableRow>
        </TableHeader>
        <TableBody>
          {items.map((cycle) => (
            <ContributionCycleRow
              key={`${cycle.group_kind}-${cycle.period_starts_at}`}
              cycle={cycle}
              renderAction={renderAction}
            />
          ))}
        </TableBody>
      </Table>
    </div>
  )
}

function ContributionCycleRow({
  cycle,
  renderAction,
}: {
  cycle: WorkgroupContributionCycle
  renderAction?: (cycle: WorkgroupContributionCycle) => ReactNode
}) {
  const current = formatContributionValue(cycle.metric, cycle.current_value)
  const target = formatContributionValue(cycle.metric, cycle.target_value)

  return (
    <TableRow>
      <TableCell className="align-top">
        <div className="font-medium">
          {formatCycleMonth(cycle.period_starts_at)}
        </div>
        <div className="mt-1 text-xs text-muted-foreground">
          第 {cycle.policy_revision} 版目标
        </div>
      </TableCell>
      <TableCell className="align-top">
        <Progress
          value={contributionPercent(cycle.current_value, cycle.target_value)}
        >
          <ProgressLabel className="text-xs">
            {contributionMetricLabel(cycle.metric)}
          </ProgressLabel>
          <ProgressValue className="text-xs">
            {() => `${current} / ${target}`}
          </ProgressValue>
        </Progress>
      </TableCell>
      <TableCell className="align-top">
        <div className="text-sm">{membershipCoverageLabel(cycle)}</div>
        <div className="mt-1 text-xs text-muted-foreground">
          有效 {formatActiveSeconds(cycle.active_seconds)}
        </div>
      </TableCell>
      <TableCell className="align-top">
        <EvidenceBadge state={cycle.evidence_state} />
        <div className="mt-1 max-w-44 text-xs text-muted-foreground">
          {evidenceDescription(cycle)}
        </div>
      </TableCell>
      <TableCell className="align-top">
        <AssessmentBadge state={cycle.assessment_state} />
        <div className="mt-1 max-w-48 text-xs text-muted-foreground">
          {explanationLabel(cycle.explanation_code)}
        </div>
        {cycle.reminder ? (
          <div className="mt-2">
            <Badge variant="outline">
              已提醒{cycle.reminder.read_at ? " · 已读" : " · 未读"}
            </Badge>
            <p className="mt-1 max-w-52 text-xs text-muted-foreground">
              {cycle.reminder.reason}
            </p>
          </div>
        ) : null}
      </TableCell>
      {renderAction ? (
        <TableCell className="align-top">{renderAction(cycle)}</TableCell>
      ) : null}
    </TableRow>
  )
}

function EvidenceBadge({
  state,
}: {
  state: WorkgroupContributionCycle["evidence_state"]
}) {
  if (state === "complete") return <Badge variant="outline">证据完整</Badge>
  if (state === "collecting") return <Badge variant="secondary">采集中</Badge>
  if (state === "incomplete") {
    return <Badge variant="destructive">证据有缺口</Badge>
  }
  return <Badge variant="secondary">暂无证据</Badge>
}

function AssessmentBadge({
  state,
}: {
  state: WorkgroupContributionCycle["assessment_state"]
}) {
  if (state === "met") return <Badge variant="outline">已达标</Badge>
  if (state === "not_met") return <Badge variant="destructive">未达标</Badge>
  if (state === "in_progress") return <Badge variant="secondary">进行中</Badge>
  if (state === "indeterminate") {
    return <Badge variant="secondary">待补证据</Badge>
  }
  return <Badge variant="secondary">不参与评估</Badge>
}

function membershipCoverageLabel(cycle: WorkgroupContributionCycle) {
  if (cycle.full_period_active) {
    return new Date(cycle.observed_at) < new Date(cycle.period_ends_at)
      ? "本月至今持续有效"
      : "整月有效"
  }
  if (cycle.active_seconds > 0) return "部分月份有效"
  return "该月未生效"
}

function evidenceDescription(cycle: WorkgroupContributionCycle) {
  if (!cycle.evidence_through) {
    return cycle.evidence_state === "unavailable"
      ? "没有可核验的业务证据"
      : "尚无证据截止时间"
  }
  return `证据截至 ${formatDateTime(cycle.evidence_through)}`
}

export function explanationLabel(
  code: WorkgroupContributionCycle["explanation_code"]
) {
  switch (code) {
    case "target_met":
      return "该周期贡献已达到目标。"
    case "period_in_progress":
      return "本月尚未结束，当前数值不是最终结果。"
    case "below_target":
      return "完整周期贡献低于目标。"
    case "no_contribution":
      return "完整周期内没有记录到有效贡献。"
    case "partial_membership":
      return "成员资格未覆盖整月，不作达标判断。"
    case "membership_inactive":
      return "该月没有有效成员资格。"
    case "evidence_incomplete":
      return "证据时间窗不连续，暂不能形成结论。"
    case "evidence_unavailable":
      return "缺少可核验证据，暂不能形成结论。"
  }
}

export function formatCycleMonth(value: string) {
  return new Intl.DateTimeFormat("zh-CN", {
    year: "numeric",
    month: "long",
    timeZone: "UTC",
  }).format(new Date(value))
}

function formatActiveSeconds(value: number) {
  const safe = Math.max(0, Math.floor(value))
  const days = Math.floor(safe / 86400)
  const hours = Math.floor((safe % 86400) / 3600)
  if (days > 0) return hours > 0 ? `${days} 天 ${hours} 小时` : `${days} 天`
  if (hours > 0) return `${hours} 小时`
  return `${Math.floor(safe / 60)} 分钟`
}

import * as React from "react"
import { useQuery } from "@tanstack/react-query"
import {
  CheckCircle2Icon,
  CircleAlertIcon,
  Clock3Icon,
  FileClockIcon,
  GaugeIcon,
  RefreshCwIcon,
  ShieldCheckIcon,
  SlidersHorizontalIcon,
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
  hnrPolicyRevisionListQueryOptions,
  type HNRPolicyRevision,
} from "~/features/staff/api/hnr-policy-administration.queries"
import {
  hnrAppealListQueryOptions,
  type HNRAppeal,
} from "~/features/staff/api/hnr-appeal-administration.queries"
import { HNRAppealDecisionDialog } from "~/features/staff/components/hnr-appeal-decision-dialog"
import { HNRPolicyDialog } from "~/features/staff/components/hnr-policy-dialog"
import { RatioAssessmentClearDialog } from "~/features/staff/components/ratio-assessment-clear-dialog"
import { RatioAppealDecisionDialog } from "~/features/staff/components/ratio-appeal-decision-dialog"
import {
  ratioWatchAppealListQueryOptions,
  ratioWatchAssessmentListQueryOptions,
  ratioWatchPolicyListQueryOptions,
  type RatioWatchAppeal,
  type RatioWatchAssessment,
  type RatioWatchPolicyRevision,
} from "~/features/staff/api/ratio-watch-administration.queries"
import { RatioWatchPolicyDialog } from "~/features/staff/components/ratio-watch-policy-dialog"
import { StaffAccessGate } from "~/features/staff/components/staff-access-gate"
import { StaffPageFrame } from "~/features/staff/components/staff-page-frame"
import { StaffSettingsValueTable } from "~/features/staff/components/staff-settings-value-table"
import { hasCapability } from "~/features/staff/model/capability"
import type { components } from "~/generated/api"
import { requestErrorDescription } from "~/shared/api/problem"
import { MetricCard } from "~/shared/components/metric-card"
import { formatCompactDateTime } from "~/shared/formatters/date-time"
import { formatBytes } from "~/shared/formatters/bytes"
import { formatInteger } from "~/shared/formatters/integer"

type CapabilityList = components["schemas"]["CapabilityList"]

export function StaffRatioHNRSettingsPage() {
  return (
    <StaffAccessGate
      requiredAction="hnr.policy.read"
      pageHeader={{
        title: "分享率与 H&R",
        description: "设置单次下载后的保种义务，并查看版本投递状态。",
      }}
    >
      {({ session, capabilities }) => (
        <RatioHNRSettingsContent
          csrfToken={session.csrf_token}
          capabilities={capabilities}
        />
      )}
    </StaffAccessGate>
  )
}

function RatioHNRSettingsContent({
  csrfToken,
  capabilities,
}: {
  csrfToken: string
  capabilities: CapabilityList
}) {
  const policies = useQuery(hnrPolicyRevisionListQueryOptions())
  const ratioPolicies = useQuery(ratioWatchPolicyListQueryOptions())
  const ratioAssessments = useQuery(
    ratioWatchAssessmentListQueryOptions("active")
  )
  const ratioAppeals = useQuery(ratioWatchAppealListQueryOptions("all"))
  const hnrAppeals = useQuery(hnrAppealListQueryOptions("all"))
  const [dialogOpen, setDialogOpen] = React.useState(false)
  const [ratioDialogOpen, setRatioDialogOpen] = React.useState(false)
  const [success, setSuccess] = React.useState("")
  if (
    policies.isPending ||
    ratioPolicies.isPending ||
    ratioAssessments.isPending ||
    ratioAppeals.isPending ||
    hnrAppeals.isPending
  )
    return <SettingsSkeleton />
  if (
    policies.isError ||
    !policies.data ||
    ratioPolicies.isError ||
    !ratioPolicies.data ||
    ratioAssessments.isError ||
    !ratioAssessments.data ||
    ratioAppeals.isError ||
    !ratioAppeals.data ||
    hnrAppeals.isError ||
    !hnrAppeals.data
  ) {
    return (
      <StaffPageFrame className="gap-4">
        <Alert variant="destructive">
          <CircleAlertIcon />
          <AlertTitle>分享率与 H&R 设置暂时无法读取</AlertTitle>
          <AlertDescription>
            {requestErrorDescription(
              policies.error ??
                ratioPolicies.error ??
                ratioAssessments.error ??
                ratioAppeals.error ??
                hnrAppeals.error,
              "请确认 Core 与 Settlement 控制服务已经启动。"
            )}
          </AlertDescription>
        </Alert>
        <Button
          variant="outline"
          className="w-fit"
          onClick={() =>
            void Promise.all([
              policies.refetch(),
              ratioPolicies.refetch(),
              ratioAssessments.refetch(),
              ratioAppeals.refetch(),
              hnrAppeals.refetch(),
            ])
          }
        >
          <RefreshCwIcon data-icon="inline-start" />
          重试
        </Button>
      </StaffPageFrame>
    )
  }

  const page = policies.data
  const ratioPage = ratioPolicies.data
  const assessmentPage = ratioAssessments.data
  const appealPage = ratioAppeals.data
  const hnrAppealPage = hnrAppeals.data
  const hnr = page.current
  const canIssue = hasCapability(capabilities, "hnr.policy.issue")
  const canIssueRatio = hasCapability(capabilities, "ratio.policy.issue")
  const canManageRatio = hasCapability(capabilities, "ratio.assessment.manage")
  const canManageHNR = hasCapability(capabilities, "hnr.assessment.manage")
  return (
    <StaffPageFrame className="gap-4">
      <header className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h1 className="font-heading text-xl font-semibold">分享率与 H&R</h1>
          <p className="mt-1 text-sm text-muted-foreground">
            H&R
            约束单次下载后的保种义务；长期总分享率观察是另一套规则，两者不混算。
          </p>
        </div>
        <div className="flex flex-wrap gap-2">
          <Button
            variant="outline"
            size="sm"
            disabled={
              policies.isFetching ||
              ratioPolicies.isFetching ||
              ratioAssessments.isFetching ||
              ratioAppeals.isFetching ||
              hnrAppeals.isFetching
            }
            onClick={() =>
              void Promise.all([
                policies.refetch(),
                ratioPolicies.refetch(),
                ratioAssessments.refetch(),
                ratioAppeals.refetch(),
                hnrAppeals.refetch(),
              ])
            }
          >
            <RefreshCwIcon
              data-icon="inline-start"
              className={
                policies.isFetching ||
                ratioPolicies.isFetching ||
                ratioAssessments.isFetching ||
                ratioAppeals.isFetching ||
                hnrAppeals.isFetching
                  ? "animate-spin"
                  : undefined
              }
            />
            刷新
          </Button>
          {canIssueRatio ? (
            <Button
              variant="outline"
              size="sm"
              onClick={() => setRatioDialogOpen(true)}
            >
              <GaugeIcon data-icon="inline-start" />
              调整分享率
            </Button>
          ) : null}
          {canIssue ? (
            <Button size="sm" onClick={() => setDialogOpen(true)}>
              <SlidersHorizontalIcon data-icon="inline-start" />
              调整 H&amp;R
            </Button>
          ) : null}
        </div>
      </header>

      {success ? (
        <Alert>
          <CheckCircle2Icon />
          <AlertTitle>规则已保存</AlertTitle>
          <AlertDescription>{success}</AlertDescription>
        </Alert>
      ) : null}

      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
        <MetricCard
          title="长期分享率考核"
          value={
            ratioPage.current
              ? ratioPage.current.enabled
                ? "已启用"
                : "已停用"
              : "未配置"
          }
          description={
            ratioPage.current
              ? `第 ${ratioPage.current.rule_version} 版规则 · VIP 豁免`
              : "不会套用隐藏默认值"
          }
          icon={<ShieldCheckIcon />}
          tone={ratioPage.current?.enabled ? "primary" : "warning"}
        />
        <MetricCard
          title="观察中 / 已逾期"
          value={`${formatInteger(ratioPage.summary.watching)} / ${formatInteger(ratioPage.summary.warning)}`}
          description="观察期内 / 已过期但未到限制线"
          icon={<UsersRoundIcon />}
          tone="default"
        />
        <MetricCard
          title="自动下载受限"
          value={formatInteger(ratioPage.summary.download_restricted)}
          description="仍可向 Tracker 汇报做种上传"
          icon={<CircleAlertIcon />}
          tone="warning"
        />
        <MetricCard
          title="考核 Worker"
          value={ratioPage.worker.last_error_code ? "异常" : "正常"}
          description={
            ratioPage.worker.last_completed_at
              ? `最近 ${formatCompactDateTime(ratioPage.worker.last_completed_at)}`
              : "尚未完成首次巡检"
          }
          icon={<FileClockIcon />}
          tone={ratioPage.worker.last_error_code ? "warning" : "muted"}
        />
      </div>

      <RatioCurrentPolicy policy={ratioPage.current} />
      <RatioAssessmentTable
        items={assessmentPage.items}
        canManage={canManageRatio}
        csrfToken={csrfToken}
      />
      <RatioAppealTable
        items={appealPage.items}
        canManage={canManageRatio}
        csrfToken={csrfToken}
      />
      <RatioPolicyHistory items={ratioPage.items} />

      <div className="border-t pt-4">
        <h2 className="font-heading text-lg font-semibold">单种 H&amp;R</h2>
        <p className="mt-1 text-sm text-muted-foreground">
          H&amp;R 只处理一次下载后的保种义务，不与上面的长期总分享率混算。
        </p>
      </div>

      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
        <MetricCard
          title="当前 H&R"
          value={hnr.configured ? modeLabel(hnr.mode) : "未配置"}
          description={
            hnr.configured ? `第 ${hnr.rule_version} 版规则` : "尚无全站覆盖"
          }
          icon={<ShieldCheckIcon />}
          tone={hnr.configured ? "primary" : "warning"}
        />
        <MetricCard
          title="最低做种时间"
          value={
            hnr.configured ? formatDuration(hnr.required_seed_seconds) : "—"
          }
          description="与达标分享率二选一"
          icon={<Clock3Icon />}
          tone="default"
        />
        <MetricCard
          title="达标分享率"
          value={
            hnr.configured ? formatRatio(hnr.required_ratio_basis_points) : "—"
          }
          description="单种原始上传 ÷ 下载"
          icon={<GaugeIcon />}
          tone="default"
        />
        <MetricCard
          title="已签发版本"
          value={formatInteger(page.total)}
          description="Core 后台不可变记录"
          icon={<FileClockIcon />}
          tone="muted"
        />
      </div>

      <Card className="gap-0 py-0">
        <CardHeader className="p-6 pb-4">
          <CardTitle>当前实际生效规则</CardTitle>
          <CardDescription>
            数据直接来自 Settlement；待投递或未来版本不会提前显示为当前规则。
          </CardDescription>
          <CardAction>
            {hnr.configured ? (
              <Badge variant="outline">Settlement 已确认</Badge>
            ) : null}
          </CardAction>
        </CardHeader>
        <CardContent className="p-0">
          {hnr.configured ? (
            <StaffSettingsValueTable
              rows={[
                { label: "模式", value: <Badge>{modeLabel(hnr.mode)}</Badge> },
                {
                  label: "达标条件",
                  value: `${formatDuration(hnr.required_seed_seconds)} 或分享率 ${formatRatio(hnr.required_ratio_basis_points)}`,
                },
                {
                  label: "考察期 / 宽限期",
                  value: `${formatDuration(hnr.assessment_window_seconds)} / ${formatDuration(hnr.grace_period_seconds)}`,
                },
                {
                  label: "单次心跳计时上限",
                  value: formatDuration(hnr.max_interval_credit_seconds),
                },
                {
                  label: "规则版本",
                  value: `第 ${hnr.rule_version} 版`,
                },
                {
                  label: "生效时间",
                  value: hnr.effective_at
                    ? formatCompactDateTime(hnr.effective_at)
                    : "—",
                },
              ]}
            />
          ) : (
            <div className="p-6 text-sm text-muted-foreground">
              尚无覆盖全站的 H&R
              政策。完成下载事件会保持“无法判定”，不会套用隐藏默认值。
            </div>
          )}
        </CardContent>
      </Card>

      <HNRAppealTable
        items={hnrAppealPage.items}
        canManage={canManageHNR}
        csrfToken={csrfToken}
      />
      <HNRPolicyHistory items={page.items} />

      <RatioWatchPolicyDialog
        open={ratioDialogOpen}
        current={ratioPage.current}
        minimumEffectiveFrom={ratioPage.minimum_effective_from}
        csrfToken={csrfToken}
        onOpenChange={setRatioDialogOpen}
        onIssued={setSuccess}
      />

      <HNRPolicyDialog
        open={dialogOpen}
        current={page.current}
        minimumEffectiveFrom={page.minimum_effective_from}
        csrfToken={csrfToken}
        onOpenChange={setDialogOpen}
        onIssued={setSuccess}
      />
    </StaffPageFrame>
  )
}

function HNRAppealTable({
  items,
  canManage,
  csrfToken,
}: {
  items: HNRAppeal[]
  canManage: boolean
  csrfToken: string
}) {
  const [selected, setSelected] = React.useState<HNRAppeal | null>(null)
  return (
    <>
      <Card className="gap-0 py-0">
        <CardHeader className="p-6 pb-4">
          <CardTitle>H&amp;R 申诉</CardTitle>
          <CardDescription>
            待处理优先；批准只豁免对应义务，驳回不改变做种进度和限制状态。
          </CardDescription>
          <CardAction>
            <Badge variant="outline">
              本页待处理{" "}
              {items.filter((item) => item.status === "pending").length}
            </Badge>
          </CardAction>
        </CardHeader>
        <CardContent className="p-0">
          {items.length === 0 ? (
            <div className="border-t p-6 text-sm text-muted-foreground">
              目前没有用户提交过 H&amp;R 申诉。
            </div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className="pl-6">用户 / 种子</TableHead>
                  <TableHead>申诉说明</TableHead>
                  <TableHead>做种进度 / 分享率</TableHead>
                  <TableHead>状态</TableHead>
                  <TableHead>提交时间</TableHead>
                  {canManage ? (
                    <TableHead className="pr-6">操作</TableHead>
                  ) : null}
                </TableRow>
              </TableHeader>
              <TableBody>
                {items.map((item) => (
                  <TableRow key={item.id}>
                    <TableCell className="max-w-xs pl-6 align-top">
                      <div className="font-medium">{item.username}</div>
                      <div className="mt-1 text-xs text-muted-foreground">
                        ID {item.user_numeric_id} · 种子 #{item.torrent.id}
                      </div>
                      <div className="mt-1 line-clamp-2 text-xs">
                        {item.torrent.title}
                      </div>
                    </TableCell>
                    <TableCell className="max-w-lg align-top whitespace-normal">
                      <p className="line-clamp-3 text-sm leading-5">
                        {item.statement}
                      </p>
                      {item.response ? (
                        <p className="mt-2 line-clamp-2 text-xs text-muted-foreground">
                          处理意见：{item.response}
                        </p>
                      ) : null}
                    </TableCell>
                    <TableCell className="align-top whitespace-nowrap">
                      <div className="font-medium">
                        {formatDuration(Number(item.seeded_seconds))} /{" "}
                        {formatDuration(Number(item.required_seed_seconds))}
                      </div>
                      <div className="mt-1 text-xs text-muted-foreground">
                        {formatRatio(Number(item.raw_ratio_basis_points))} /{" "}
                        {formatRatio(Number(item.required_ratio_basis_points))}
                      </div>
                    </TableCell>
                    <TableCell className="align-top">
                      {hnrAppealBadge(item.status)}
                    </TableCell>
                    <TableCell className="align-top whitespace-nowrap">
                      {formatCompactDateTime(item.created_at)}
                    </TableCell>
                    {canManage ? (
                      <TableCell className="pr-6 align-top">
                        {item.status === "pending" ? (
                          <Button
                            variant="outline"
                            size="xs"
                            onClick={() => setSelected(item)}
                          >
                            处理
                          </Button>
                        ) : (
                          <span className="text-xs text-muted-foreground">
                            已完成
                          </span>
                        )}
                      </TableCell>
                    ) : null}
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>
      <HNRAppealDecisionDialog
        appeal={selected}
        csrfToken={csrfToken}
        onOpenChange={(open) => {
          if (!open) setSelected(null)
        }}
      />
    </>
  )
}

function hnrAppealBadge(status: HNRAppeal["status"]) {
  if (status === "pending") return <Badge variant="secondary">待处理</Badge>
  if (status === "approved") return <Badge variant="outline">已批准</Badge>
  if (status === "rejected") return <Badge variant="destructive">已驳回</Badge>
  return <Badge variant="outline">义务已达标</Badge>
}

function RatioAppealTable({
  items,
  canManage,
  csrfToken,
}: {
  items: RatioWatchAppeal[]
  canManage: boolean
  csrfToken: string
}) {
  const [selected, setSelected] = React.useState<RatioWatchAppeal | null>(null)
  return (
    <>
      <Card className="gap-0 py-0">
        <CardHeader className="p-6 pb-4">
          <CardTitle>分享率申诉</CardTitle>
          <CardDescription>
            待处理优先；批准会解除本期考核，驳回只记录意见并保持考核不变。
          </CardDescription>
          <CardAction>
            <Badge variant="outline">
              本页待处理{" "}
              {items.filter((item) => item.status === "pending").length}
            </Badge>
          </CardAction>
        </CardHeader>
        <CardContent className="p-0">
          {items.length === 0 ? (
            <div className="border-t p-6 text-sm text-muted-foreground">
              目前没有用户提交过分享率申诉。
            </div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className="pl-6">用户</TableHead>
                  <TableHead>申诉说明</TableHead>
                  <TableHead>分享率 / 流量</TableHead>
                  <TableHead>状态</TableHead>
                  <TableHead>提交时间</TableHead>
                  {canManage ? (
                    <TableHead className="pr-6">操作</TableHead>
                  ) : null}
                </TableRow>
              </TableHeader>
              <TableBody>
                {items.map((item) => (
                  <TableRow key={item.id}>
                    <TableCell className="pl-6 align-top">
                      <div className="font-medium">{item.username}</div>
                      <div className="mt-1 text-xs text-muted-foreground">
                        ID {item.user_numeric_id}
                      </div>
                    </TableCell>
                    <TableCell className="max-w-lg align-top whitespace-normal">
                      <p className="line-clamp-3 text-sm leading-5">
                        {item.statement}
                      </p>
                      {item.response ? (
                        <p className="mt-2 line-clamp-2 text-xs text-muted-foreground">
                          处理意见：{item.response}
                        </p>
                      ) : null}
                    </TableCell>
                    <TableCell className="align-top whitespace-nowrap">
                      <div className="font-medium">
                        {formatRatio(item.current_ratio_basis_points)}
                      </div>
                      <div className="mt-1 text-xs text-muted-foreground">
                        <span className="text-success">
                          {formatBytes(item.current_credited_uploaded_bytes)}
                        </span>
                        {" / "}
                        <span className="text-destructive">
                          {formatBytes(item.current_charged_downloaded_bytes)}
                        </span>
                      </div>
                    </TableCell>
                    <TableCell className="align-top">
                      {ratioAppealBadge(item.status)}
                    </TableCell>
                    <TableCell className="align-top whitespace-nowrap">
                      {formatCompactDateTime(item.created_at)}
                    </TableCell>
                    {canManage ? (
                      <TableCell className="pr-6 align-top">
                        {item.status === "pending" ? (
                          <Button
                            variant="outline"
                            size="xs"
                            onClick={() => setSelected(item)}
                          >
                            处理
                          </Button>
                        ) : (
                          <span className="text-xs text-muted-foreground">
                            已完成
                          </span>
                        )}
                      </TableCell>
                    ) : null}
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>
      <RatioAppealDecisionDialog
        appeal={selected}
        csrfToken={csrfToken}
        onOpenChange={(open) => {
          if (!open) setSelected(null)
        }}
      />
    </>
  )
}

function ratioAppealBadge(status: RatioWatchAppeal["status"]) {
  if (status === "pending") return <Badge variant="secondary">待处理</Badge>
  if (status === "approved") return <Badge variant="outline">已批准</Badge>
  if (status === "rejected") return <Badge variant="destructive">已驳回</Badge>
  return <Badge variant="outline">考核已结束</Badge>
}

function RatioCurrentPolicy({
  policy,
}: {
  policy: RatioWatchPolicyRevision | null
}) {
  return (
    <Card className="gap-0 py-0">
      <CardHeader className="p-6 pb-4">
        <CardTitle>长期分享率当前规则</CardTitle>
        <CardDescription>
          只使用 Core 已结算的累计上传与下载；规则切换不会回算历史流量。
        </CardDescription>
        <CardAction>
          {policy ? (
            <Badge variant={policy.enabled ? "outline" : "secondary"}>
              {policy.enabled ? "执行中" : "已停用"}
            </Badge>
          ) : null}
        </CardAction>
      </CardHeader>
      <CardContent className="p-0">
        {policy ? (
          <StaffSettingsValueTable
            rows={[
              {
                label: "启用考核",
                value: policy.enabled ? "是" : "否",
              },
              {
                label: "下载量阈值",
                value: policy.enabled
                  ? formatBytes(policy.download_threshold_bytes)
                  : "—",
              },
              {
                label: "最低分享率",
                value: policy.enabled
                  ? formatRatio(policy.minimum_ratio_basis_points)
                  : "—",
              },
              {
                label: "观察天数",
                value: policy.enabled
                  ? formatDuration(policy.watch_period_seconds)
                  : "—",
              },
              {
                label: "到期下载限制线",
                value: policy.enabled
                  ? formatRatio(policy.restriction_ratio_basis_points)
                  : "—",
              },
              { label: "VIP 豁免", value: policy.vip_exempt ? "是" : "否" },
              {
                label: "规则版本 / 生效时间",
                value: `第 ${policy.rule_version} 版 · ${formatCompactDateTime(policy.effective_at)}`,
              },
            ]}
          />
        ) : (
          <div className="border-t p-6 text-sm text-muted-foreground">
            尚未签发长期分享率规则。系统不会擅自套用 PtYes
            默认值；请先预览影响再签发初始规则。
          </div>
        )}
      </CardContent>
    </Card>
  )
}

function RatioAssessmentTable({
  items,
  canManage,
  csrfToken,
}: {
  items: RatioWatchAssessment[]
  canManage: boolean
  csrfToken: string
}) {
  const [selected, setSelected] = React.useState<RatioWatchAssessment | null>(
    null
  )
  return (
    <>
      <Card className="gap-0 py-0">
        <CardHeader className="p-6 pb-4">
          <CardTitle>当前考核用户</CardTitle>
          <CardDescription>
            只显示结构化考核证据；邮箱、Tracker 密钥和账本明细不会进入此表。
          </CardDescription>
        </CardHeader>
        <CardContent className="p-0">
          {items.length === 0 ? (
            <div className="border-t p-6 text-sm text-muted-foreground">
              当前没有观察中、逾期或自动下载受限的用户。
            </div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className="pl-6">用户</TableHead>
                  <TableHead>状态</TableHead>
                  <TableHead>当前分享率</TableHead>
                  <TableHead>累计上传 / 下载</TableHead>
                  <TableHead className="pr-6">截止时间</TableHead>
                  {canManage ? (
                    <TableHead className="pr-6">操作</TableHead>
                  ) : null}
                </TableRow>
              </TableHeader>
              <TableBody>
                {items.map((item) => (
                  <TableRow key={item.id}>
                    <TableCell className="pl-6">
                      <div className="font-medium">{item.username}</div>
                      <div className="mt-1 text-xs text-muted-foreground">
                        ID {item.user_numeric_id} · 第 {item.policy_version}{" "}
                        版规则
                      </div>
                    </TableCell>
                    <TableCell>{ratioAssessmentBadge(item.status)}</TableCell>
                    <TableCell className="font-medium">
                      {formatRatio(item.current_ratio_basis_points)}
                    </TableCell>
                    <TableCell>
                      <span className="text-success">
                        {formatBytes(item.current_credited_uploaded_bytes)}
                      </span>
                      {" / "}
                      <span className="text-destructive">
                        {formatBytes(item.current_charged_downloaded_bytes)}
                      </span>
                    </TableCell>
                    <TableCell className="pr-6 whitespace-nowrap">
                      {formatCompactDateTime(item.deadline_at)}
                    </TableCell>
                    {canManage ? (
                      <TableCell className="pr-6">
                        <Button
                          variant="outline"
                          size="xs"
                          onClick={() => setSelected(item)}
                        >
                          人工解除
                        </Button>
                      </TableCell>
                    ) : null}
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>
      <RatioAssessmentClearDialog
        assessment={selected}
        csrfToken={csrfToken}
        onOpenChange={(open) => {
          if (!open) setSelected(null)
        }}
      />
    </>
  )
}

function RatioPolicyHistory({ items }: { items: RatioWatchPolicyRevision[] }) {
  return (
    <Card className="gap-0 py-0">
      <CardHeader className="p-6 pb-4">
        <CardTitle>长期分享率版本记录</CardTitle>
        <CardDescription>
          每条考核固定绑定进入时的规则版本，后续调整不会改变原截止时间和阈值。
        </CardDescription>
      </CardHeader>
      <CardContent className="p-0">
        {items.length === 0 ? (
          <div className="border-t p-6 text-sm text-muted-foreground">
            后台尚未签发版本。
          </div>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="pl-6">版本</TableHead>
                <TableHead>状态</TableHead>
                <TableHead>主要条件</TableHead>
                <TableHead>生效时间</TableHead>
                <TableHead className="pr-6">调整原因</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {items.map((item) => (
                <TableRow key={item.id}>
                  <TableCell className="pl-6 font-medium">
                    第 {item.rule_version} 版
                  </TableCell>
                  <TableCell>
                    <Badge variant="outline">
                      {item.enabled ? "启用" : "停用"} ·{" "}
                      {item.timeline_state === "active" ? "已生效" : "待生效"}
                    </Badge>
                  </TableCell>
                  <TableCell>
                    {item.enabled
                      ? `${formatBytes(item.download_threshold_bytes)} / ${formatRatio(item.minimum_ratio_basis_points)} / ${formatDuration(item.watch_period_seconds)}`
                      : "停止新考核"}
                  </TableCell>
                  <TableCell className="whitespace-nowrap">
                    {formatCompactDateTime(item.effective_at)}
                  </TableCell>
                  <TableCell className="max-w-md pr-6 whitespace-normal">
                    {item.reason}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </CardContent>
    </Card>
  )
}

function ratioAssessmentBadge(status: RatioWatchAssessment["status"]) {
  if (status === "download_restricted") {
    return <Badge variant="destructive">下载受限</Badge>
  }
  if (status === "warning") return <Badge variant="secondary">已逾期</Badge>
  if (status === "watching") return <Badge variant="outline">观察中</Badge>
  if (status === "satisfied") return <Badge variant="outline">已达标</Badge>
  if (status === "manually_cleared")
    return <Badge variant="outline">人工解除</Badge>
  if (status === "vip_exempted")
    return <Badge variant="outline">VIP 豁免</Badge>
  return <Badge variant="secondary">不再适用</Badge>
}

function HNRPolicyHistory({ items }: { items: HNRPolicyRevision[] }) {
  return (
    <Card className="gap-0 py-0">
      <CardHeader className="p-6 pb-4">
        <CardTitle>H&amp;R 规则版本记录</CardTitle>
        <CardDescription>
          这里显示从 PeerGo 后台签发的版本；旧 CLI
          导入规则仍以当前实际规则为准。
        </CardDescription>
      </CardHeader>
      <CardContent className="p-0">
        {items.length === 0 ? (
          <div className="border-t p-6 text-sm text-muted-foreground">
            后台尚未签发过新版本。点击“调整规则”创建首个后台版本。
          </div>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="pl-6">版本</TableHead>
                <TableHead>规则</TableHead>
                <TableHead>生效时间</TableHead>
                <TableHead>投递</TableHead>
                <TableHead className="pr-6">调整原因</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {items.map((item) => (
                <TableRow key={item.id}>
                  <TableCell className="pl-6 font-medium">
                    第 {item.rule_version} 版
                    <div className="mt-1 text-xs text-muted-foreground">
                      {item.timeline_state === "scheduled"
                        ? "等待生效"
                        : "已到生效时间"}
                    </div>
                  </TableCell>
                  <TableCell>
                    <div>{modeLabel(item.mode)}</div>
                    {item.mode === "enforced" ? (
                      <div className="mt-1 text-xs text-muted-foreground">
                        {formatDuration(item.required_seed_seconds)} 或{" "}
                        {formatRatio(item.required_ratio_basis_points)}
                      </div>
                    ) : null}
                  </TableCell>
                  <TableCell className="whitespace-nowrap">
                    {formatCompactDateTime(item.effective_at)}
                  </TableCell>
                  <TableCell>{deliveryBadge(item)}</TableCell>
                  <TableCell className="max-w-md pr-6 whitespace-normal">
                    {item.reason}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </CardContent>
    </Card>
  )
}

function deliveryBadge(revision: HNRPolicyRevision) {
  if (revision.delivery_state === "delivered") {
    return <Badge variant="outline">已送达</Badge>
  }
  if (revision.delivery_state === "retrying") {
    return <Badge variant="destructive">重试中</Badge>
  }
  return <Badge variant="secondary">待投递</Badge>
}

function modeLabel(mode: string) {
  if (mode === "enforced") return "执行中"
  if (mode === "exempt") return "全站豁免"
  if (mode === "disabled") return "已停用"
  return "未配置"
}

function formatRatio(basisPoints: number) {
  return Number((basisPoints / 10_000).toFixed(2)).toString()
}

function formatDuration(seconds: number) {
  if (seconds === 0) return "无"
  if (seconds % 86_400 === 0) return `${seconds / 86_400} 天`
  if (seconds % 3600 === 0) return `${seconds / 3600} 小时`
  if (seconds % 60 === 0) return `${seconds / 60} 分钟`
  return `${seconds} 秒`
}

function SettingsSkeleton() {
  return (
    <StaffPageFrame aria-label="正在加载分享率与 H&R 设置">
      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
        {Array.from({ length: 4 }, (_, index) => (
          <Skeleton key={index} className="h-36 rounded-lg" />
        ))}
      </div>
      <Skeleton className="h-20 rounded-lg" />
      <Skeleton className="h-[30rem] rounded-lg" />
    </StaffPageFrame>
  )
}

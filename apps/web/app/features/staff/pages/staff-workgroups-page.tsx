import * as React from "react"
import { useQuery } from "@tanstack/react-query"
import {
  CheckIcon,
  BellRingIcon,
  CalendarClockIcon,
  CircleAlertIcon,
  HistoryIcon,
  PauseIcon,
  PlusIcon,
  RefreshCwIcon,
  RotateCcwIcon,
  StopCircleIcon,
  TargetIcon,
  UsersRoundIcon,
  XIcon,
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
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "~/components/ui/dialog"
import {
  Field,
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLabel,
} from "~/components/ui/field"
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "~/components/ui/empty"
import { Input } from "~/components/ui/input"
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "~/components/ui/select"
import { Skeleton } from "~/components/ui/skeleton"
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "~/components/ui/sheet"
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
  type MembershipTransition,
  type WorkgroupApplication,
  type WorkgroupKind,
  type WorkgroupMembership,
  type WorkgroupContributionPolicyPage,
  type WorkgroupContributionCycle,
  type WorkgroupContributionSummary,
  adminWorkgroupContributionCyclesQueryOptions,
  adminWorkgroupApplicationsQueryOptions,
  adminWorkgroupContributionPoliciesQueryOptions,
  adminWorkgroupMembershipsQueryOptions,
  adminWorkgroupOverviewQueryOptions,
  useChangeWorkgroupMembership,
  useDecideWorkgroupApplication,
  useGrantWorkgroupMembership,
  useIssueWorkgroupContributionPolicy,
  useIssueWorkgroupContributionReminder,
} from "~/features/staff/api/workgroup-administration.queries"
import { StaffAccessGate } from "~/features/staff/components/staff-access-gate"
import { StaffPageFrame } from "~/features/staff/components/staff-page-frame"
import { StaffWorkgroupTaskPanel } from "~/features/staff/components/staff-workgroup-task-panel"
import { hasCapability } from "~/features/staff/model/capability"
import { requestErrorDescription } from "~/shared/api/problem"
import { formatBytes } from "~/shared/formatters/bytes"
import { formatDateTime } from "~/shared/formatters/date-time"
import {
  contributionMetricLabel,
  formatContributionValue,
} from "~/features/workgroups/model/contribution-format"
import {
  ContributionCycleHistory,
  formatCycleMonth,
} from "~/features/workgroups/components/contribution-cycle-history"

const groupOptions: { value: WorkgroupKind; label: string }[] = [
  { value: "reseed", label: "转种组" },
  { value: "review", label: "种审组" },
  { value: "retention", label: "保种组" },
]

type ApplicationDialogState = {
  application: WorkgroupApplication
  decision: "approve" | "reject"
}

type MembershipDialogState = {
  membership: WorkgroupMembership
  transition: MembershipTransition
}

export function StaffWorkgroupsPage() {
  return (
    <StaffAccessGate
      requiredAction="workgroup.manage.read"
      pageHeader={{
        title: "工作组管理",
        description:
          "集中管理转种、种审和保种职责；成员状态变化保留不可变历史。",
      }}
    >
      {({ session, capabilities }) => (
        <WorkgroupsContent
          csrfToken={session.csrf_token}
          canDecide={hasCapability(
            capabilities,
            "workgroup.application.decide"
          )}
          canManage={hasCapability(capabilities, "workgroup.membership.manage")}
          canIssuePolicy={hasCapability(
            capabilities,
            "workgroup.contribution.policy.issue"
          )}
          canIssueReminder={hasCapability(
            capabilities,
            "workgroup.contribution.reminder.issue"
          )}
          canPublishTask={hasCapability(capabilities, "workgroup.task.publish")}
          canReviewTask={hasCapability(capabilities, "workgroup.task.review")}
        />
      )}
    </StaffAccessGate>
  )
}

function WorkgroupsContent({
  csrfToken,
  canDecide,
  canManage,
  canIssuePolicy,
  canIssueReminder,
  canPublishTask,
  canReviewTask,
}: {
  csrfToken: string
  canDecide: boolean
  canManage: boolean
  canIssuePolicy: boolean
  canIssueReminder: boolean
  canPublishTask: boolean
  canReviewTask: boolean
}) {
  const [groupKind, setGroupKind] = React.useState<WorkgroupKind>("review")
  const [applicationDialog, setApplicationDialog] =
    React.useState<ApplicationDialogState>()
  const [membershipDialog, setMembershipDialog] =
    React.useState<MembershipDialogState>()
  const [historyMembership, setHistoryMembership] =
    React.useState<WorkgroupMembership>()
  const [grantOpen, setGrantOpen] = React.useState(false)
  const [policyOpen, setPolicyOpen] = React.useState(false)
  const overview = useQuery(adminWorkgroupOverviewQueryOptions)
  const applications = useQuery(
    adminWorkgroupApplicationsQueryOptions("pending")
  )
  const memberships = useQuery(
    adminWorkgroupMembershipsQueryOptions(groupKind, "")
  )
  const policies = useQuery(
    adminWorkgroupContributionPoliciesQueryOptions(groupKind)
  )

  const refresh = () => {
    void Promise.all([
      overview.refetch(),
      applications.refetch(),
      memberships.refetch(),
      policies.refetch(),
    ])
  }

  return (
    <StaffPageFrame>
      {overview.isPending ? (
        <OverviewSkeleton />
      ) : overview.isError || !overview.data ? (
        <ReadError error={overview.error} onRetry={refresh} />
      ) : (
        <>
          <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
            <Metric title="待审批" value={overview.data.pending_applications} />
            <Metric
              title="转种组"
              value={overview.data.active_reseed_members}
            />
            <Metric
              title="种审组"
              value={overview.data.active_review_members}
            />
            <Metric
              title="保种组"
              value={overview.data.active_retention_members}
            />
          </div>
          <ContributionSummaryGrid
            items={overview.data.contribution_summaries}
          />
        </>
      )}

      <Card>
        <CardHeader className="flex-row items-center justify-between gap-3">
          <CardTitle className="text-lg">待处理申请</CardTitle>
          <Button variant="outline" size="sm" onClick={refresh}>
            <RefreshCwIcon data-icon="inline-start" />
            刷新
          </Button>
        </CardHeader>
        <CardContent>
          {applications.isPending ? (
            <Skeleton className="h-36 w-full" />
          ) : applications.isError || !applications.data ? (
            <ReadError error={applications.error} onRetry={refresh} />
          ) : (
            <ApplicationTable
              items={applications.data.items}
              canDecide={canDecide}
              onDecide={(application, decision) =>
                setApplicationDialog({ application, decision })
              }
            />
          )}
        </CardContent>
      </Card>

      <StaffWorkgroupTaskPanel
        groupKind={groupKind}
        csrfToken={csrfToken}
        canPublish={canPublishTask}
        canReview={canReviewTask}
      />

      <Card>
        <CardHeader>
          <CardTitle className="text-lg">
            贡献目标 · {groupLabel(groupKind)}
          </CardTitle>
          <CardDescription>
            当前目标用于本月展示；新目标只能从后续完整自然月生效，历史版本不会被覆盖。
          </CardDescription>
          <CardAction className="flex items-center gap-2">
            <Select
              items={groupOptions}
              value={groupKind}
              onValueChange={(value) => value && setGroupKind(value)}
            >
              <SelectTrigger size="sm" aria-label="选择工作组">
                <SelectValue />
              </SelectTrigger>
              <SelectContent align="end" alignItemWithTrigger={false}>
                <SelectGroup>
                  {groupOptions.map((option) => (
                    <SelectItem key={option.value} value={option.value}>
                      {option.label}
                    </SelectItem>
                  ))}
                </SelectGroup>
              </SelectContent>
            </Select>
            {canIssuePolicy && policies.data ? (
              <Button size="sm" onClick={() => setPolicyOpen(true)}>
                <CalendarClockIcon data-icon="inline-start" />
                设置后续目标
              </Button>
            ) : null}
          </CardAction>
        </CardHeader>
        <CardContent>
          {policies.isPending ? (
            <Skeleton className="h-40 w-full" />
          ) : policies.isError || !policies.data ? (
            <ReadError error={policies.error} onRetry={refresh} />
          ) : (
            <ContributionPolicyPanel page={policies.data} />
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-lg">
            成员资格 · {groupLabel(groupKind)}
          </CardTitle>
          <CardDescription>
            资格状态按实际变更时刻生效，历史记录保留供后续核对。
          </CardDescription>
          {canManage ? (
            <CardAction>
              <Button size="sm" onClick={() => setGrantOpen(true)}>
                <PlusIcon data-icon="inline-start" />
                添加成员
              </Button>
            </CardAction>
          ) : null}
        </CardHeader>
        <CardContent>
          {memberships.isPending ? (
            <Skeleton className="h-40 w-full" />
          ) : memberships.isError || !memberships.data ? (
            <ReadError error={memberships.error} onRetry={refresh} />
          ) : (
            <MembershipTable
              items={memberships.data.items}
              canManage={canManage}
              onViewHistory={setHistoryMembership}
              onChange={(membership, transition) =>
                setMembershipDialog({ membership, transition })
              }
            />
          )}
        </CardContent>
      </Card>

      {applicationDialog ? (
        <ApplicationDecisionDialog
          state={applicationDialog}
          csrfToken={csrfToken}
          onOpenChange={(open) => !open && setApplicationDialog(undefined)}
        />
      ) : null}
      {membershipDialog ? (
        <MembershipChangeDialog
          state={membershipDialog}
          csrfToken={csrfToken}
          onOpenChange={(open) => !open && setMembershipDialog(undefined)}
        />
      ) : null}
      <StaffContributionHistorySheet
        membership={historyMembership}
        csrfToken={csrfToken}
        canIssueReminder={canIssueReminder}
        onOpenChange={(open) => !open && setHistoryMembership(undefined)}
      />
      <GrantMembershipDialog
        open={grantOpen}
        groupKind={groupKind}
        csrfToken={csrfToken}
        onOpenChange={setGrantOpen}
      />
      {policies.data ? (
        <ContributionPolicyDialog
          open={policyOpen}
          groupKind={groupKind}
          page={policies.data}
          csrfToken={csrfToken}
          onOpenChange={setPolicyOpen}
        />
      ) : null}
    </StaffPageFrame>
  )
}

function ContributionSummaryGrid({
  items,
}: {
  items: WorkgroupContributionSummary[]
}) {
  return (
    <div className="grid gap-3 lg:grid-cols-3">
      {items.map((item) => (
        <Card key={item.group_kind} size="sm">
          <CardHeader>
            <CardTitle>{groupLabel(item.group_kind)} · 本月贡献</CardTitle>
            <CardDescription>
              {contributionMetricLabel(item.metric)}累计{" "}
              {formatContributionValue(item.metric, item.total_value)}
            </CardDescription>
            <CardAction>
              <TargetIcon className="size-4 text-muted-foreground" />
            </CardAction>
          </CardHeader>
          <CardContent className="grid grid-cols-3 gap-3 border-t pt-3 text-sm">
            <SummaryValue label="有效成员" value={item.active_members} />
            <SummaryValue label="已有贡献" value={item.contributing_members} />
            <SummaryValue label="达到目标" value={item.met_members} />
          </CardContent>
        </Card>
      ))}
    </div>
  )
}

function SummaryValue({ label, value }: { label: string; value: number }) {
  return (
    <div>
      <div className="font-medium tabular-nums">
        {value.toLocaleString("zh-CN")}
      </div>
      <div className="text-xs text-muted-foreground">{label}</div>
    </div>
  )
}

function ContributionPolicyPanel({
  page,
}: {
  page: WorkgroupContributionPolicyPage
}) {
  const current = page.current

  return (
    <div className="flex flex-col gap-4">
      <div className="grid gap-3 rounded-md border bg-muted/20 p-4 sm:grid-cols-3">
        <div>
          <div className="text-xs text-muted-foreground">本月目标</div>
          <div className="mt-1 font-medium tabular-nums">
            {current
              ? formatContributionValue(current.metric, current.target_value)
              : "尚未设置"}
          </div>
        </div>
        <div>
          <div className="text-xs text-muted-foreground">统计方式</div>
          <div className="mt-1 font-medium">自然月 · 仅观察</div>
        </div>
        <div>
          <div className="text-xs text-muted-foreground">下个可生效月份</div>
          <div className="mt-1 font-medium">
            {formatPolicyMonth(page.minimum_effective_from)}
          </div>
        </div>
      </div>

      {page.items.length === 0 ? (
        <Empty className="border">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <CalendarClockIcon />
            </EmptyMedia>
            <EmptyTitle>还没有贡献目标</EmptyTitle>
            <EmptyDescription>
              首个目标建立后，后续调整会按月份保留完整历史。
            </EmptyDescription>
          </EmptyHeader>
        </Empty>
      ) : (
        <div className="overflow-x-auto rounded-md border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>版本</TableHead>
                <TableHead>目标</TableHead>
                <TableHead>生效月份</TableHead>
                <TableHead>状态</TableHead>
                <TableHead>设置理由</TableHead>
                <TableHead>登记时间</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {page.items.map((policy) => (
                <TableRow key={policy.revision}>
                  <TableCell className="tabular-nums">
                    第 {policy.revision} 版
                  </TableCell>
                  <TableCell className="font-medium tabular-nums">
                    {formatContributionValue(
                      policy.metric,
                      policy.target_value
                    )}
                  </TableCell>
                  <TableCell>
                    {policy.opening || !policy.effective_from
                      ? "初始规则"
                      : formatPolicyMonth(policy.effective_from)}
                  </TableCell>
                  <TableCell>
                    <Badge
                      variant={
                        policy.timeline_state === "active"
                          ? "outline"
                          : "secondary"
                      }
                    >
                      {policy.timeline_state === "active"
                        ? "当前生效"
                        : "等待生效"}
                    </Badge>
                  </TableCell>
                  <TableCell className="max-w-72 whitespace-normal">
                    {policy.reason}
                  </TableCell>
                  <TableCell>{formatDateTime(policy.created_at)}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}
    </div>
  )
}

function ContributionPolicyDialog({
  open,
  groupKind,
  page,
  csrfToken,
  onOpenChange,
}: {
  open: boolean
  groupKind: WorkgroupKind
  page: WorkgroupContributionPolicyPage
  csrfToken: string
  onOpenChange: (open: boolean) => void
}) {
  const mutation = useIssueWorkgroupContributionPolicy()
  const minimumMonth = monthInputValue(page.minimum_effective_from)
  const [effectiveMonth, setEffectiveMonth] = React.useState(minimumMonth)
  const [target, setTarget] = React.useState("")
  const [reason, setReason] = React.useState("")
  const targetNumber = Number(target)
  const targetValidationError = contributionTargetError(
    groupKind,
    target,
    targetNumber
  )
  const targetError = target ? targetValidationError : undefined
  const reasonError =
    [...reason.trim()].length > 1000
      ? "设置理由不能超过 1000 个字符。"
      : undefined

  React.useEffect(() => {
    if (!open) return
    setEffectiveMonth(minimumMonth)
    setTarget("")
    setReason("")
    mutation.reset()
    // Reset only when the dialog opens for a new group or policy page.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, groupKind, minimumMonth])

  const submit = async (event: React.FormEvent) => {
    event.preventDefault()
    if (targetValidationError || reasonError) return
    try {
      await mutation.mutateAsync({
        csrfToken,
        idempotencyKey: crypto.randomUUID(),
        groupKind,
        targetValue: contributionTargetValue(groupKind, targetNumber),
        effectiveFrom: monthToEffectiveFrom(effectiveMonth),
        reason: reason.trim(),
      })
      onOpenChange(false)
    } catch {
      // Keep the dialog open so the typed API error remains visible.
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>设置{groupLabel(groupKind)}后续贡献目标</DialogTitle>
          <DialogDescription>
            仅允许从 {formatPolicyMonth(page.minimum_effective_from)}{" "}
            起按月追加； 已有版本不能覆盖或回填。
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={submit} className="flex flex-col gap-5">
          <FieldGroup>
            <MutationError error={mutation.error} />
            <Field>
              <FieldLabel htmlFor="contribution-policy-month">
                生效月份
              </FieldLabel>
              <Input
                id="contribution-policy-month"
                type="month"
                min={minimumMonth}
                value={effectiveMonth}
                onChange={(event) => setEffectiveMonth(event.target.value)}
              />
              <FieldDescription>
                从该月 1 日 00:00 UTC 开始用于整月统计。
              </FieldDescription>
            </Field>
            <Field data-invalid={Boolean(targetError)}>
              <FieldLabel htmlFor="contribution-policy-target">
                每位成员目标（{contributionTargetUnit(groupKind)}）
              </FieldLabel>
              <Input
                id="contribution-policy-target"
                type="number"
                inputMode="numeric"
                min={1}
                step={1}
                value={target}
                aria-invalid={Boolean(targetError)}
                onChange={(event) => setTarget(event.target.value)}
              />
              <FieldDescription>
                当前阶段只展示完成情况，不会自动暂停成员资格。
              </FieldDescription>
              <FieldError>{targetError}</FieldError>
            </Field>
            <Field data-invalid={Boolean(reasonError)}>
              <FieldLabel htmlFor="contribution-policy-reason">
                设置理由
              </FieldLabel>
              <Textarea
                id="contribution-policy-reason"
                value={reason}
                maxLength={1000}
                aria-invalid={Boolean(reasonError)}
                onChange={(event) => setReason(event.target.value)}
              />
              <FieldDescription>
                理由会和签发人、请求编号一起保存在不可变历史中。
              </FieldDescription>
              <FieldError>{reasonError}</FieldError>
            </Field>
          </FieldGroup>
          <DialogFooter>
            <DialogClose render={<Button type="button" variant="outline" />}>
              取消
            </DialogClose>
            <Button
              type="submit"
              disabled={
                !effectiveMonth ||
                !target ||
                Boolean(targetValidationError) ||
                Boolean(reasonError) ||
                mutation.isPending
              }
            >
              {mutation.isPending ? (
                <Spinner data-icon="inline-start" />
              ) : (
                <CalendarClockIcon data-icon="inline-start" />
              )}
              确认设置
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

function ApplicationTable({
  items,
  canDecide,
  onDecide,
}: {
  items: WorkgroupApplication[]
  canDecide: boolean
  onDecide: (
    application: WorkgroupApplication,
    decision: "approve" | "reject"
  ) => void
}) {
  if (items.length === 0) {
    return (
      <p className="py-8 text-center text-sm text-muted-foreground">
        当前没有待处理申请。
      </p>
    )
  }
  return (
    <div className="overflow-x-auto rounded-md border">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>用户</TableHead>
            <TableHead>条件快照</TableHead>
            <TableHead>申请说明</TableHead>
            <TableHead>提交时间</TableHead>
            <TableHead className="text-right">操作</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {items.map((application) => (
            <TableRow key={application.id}>
              <TableCell>
                <div className="font-medium">
                  {application.applicant_username}
                </div>
                <div className="text-xs text-muted-foreground">
                  ID {application.applicant_numeric_id} ·{" "}
                  {application.applicant_display_name}
                </div>
              </TableCell>
              <TableCell className="text-xs">
                Lv.{application.eligibility.level} ·{" "}
                {formatBytes(application.eligibility.credited_uploaded_bytes)} ·{" "}
                {application.eligibility.account_age_days} 天
              </TableCell>
              <TableCell className="max-w-80 whitespace-normal">
                {application.statement}
              </TableCell>
              <TableCell>{formatDateTime(application.submitted_at)}</TableCell>
              <TableCell>
                {canDecide ? (
                  <div className="flex justify-end gap-2">
                    <Button
                      size="xs"
                      onClick={() => onDecide(application, "approve")}
                    >
                      <CheckIcon data-icon="inline-start" />
                      批准
                    </Button>
                    <Button
                      size="xs"
                      variant="outline"
                      onClick={() => onDecide(application, "reject")}
                    >
                      <XIcon data-icon="inline-start" />
                      驳回
                    </Button>
                  </div>
                ) : (
                  <span className="text-xs text-muted-foreground">只读</span>
                )}
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  )
}

function MembershipTable({
  items,
  canManage,
  onViewHistory,
  onChange,
}: {
  items: WorkgroupMembership[]
  canManage: boolean
  onViewHistory: (membership: WorkgroupMembership) => void
  onChange: (
    membership: WorkgroupMembership,
    transition: MembershipTransition
  ) => void
}) {
  if (items.length === 0) {
    return (
      <p className="py-8 text-center text-sm text-muted-foreground">
        这个工作组还没有成员。
      </p>
    )
  }
  return (
    <div className="overflow-x-auto rounded-md border">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>用户</TableHead>
            <TableHead>状态</TableHead>
            <TableHead>来源</TableHead>
            <TableHead>本月贡献</TableHead>
            <TableHead>加入时间</TableHead>
            <TableHead className="text-right">操作</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {items.map((membership) => (
            <TableRow key={membership.id}>
              <TableCell>
                <div className="font-medium">{membership.username}</div>
                <div className="text-xs text-muted-foreground">
                  ID {membership.user_numeric_id} · {membership.display_name}
                </div>
              </TableCell>
              <TableCell>
                <Badge
                  variant={
                    membership.status === "active" ? "outline" : "secondary"
                  }
                >
                  {membershipStatusLabel(membership.status)}
                </Badge>
              </TableCell>
              <TableCell>
                {membership.source === "application"
                  ? "申请批准"
                  : membership.source === "legacy_migration"
                    ? membership.legacy_reviewer
                      ? "Rousi 种审迁移"
                      : "Rousi 勋章迁移"
                    : "管理员授予"}
              </TableCell>
              <TableCell>
                {membership.contribution || membership.legacy_reviewer ? (
                  <div className="flex flex-col gap-2">
                    {membership.legacy_reviewer ? (
                      <div className="flex flex-col gap-0.5 text-xs">
                        <span className="text-muted-foreground">
                          旧站审核记录
                        </span>
                        <span className="tabular-nums">
                          {membership.legacy_reviewer.total_reviews.toLocaleString(
                            "zh-CN"
                          )}{" "}
                          次 · 准确率{" "}
                          {membership.legacy_reviewer.total_reviews > 0
                            ? (
                                (membership.legacy_reviewer.accurate_count /
                                  membership.legacy_reviewer.total_reviews) *
                                100
                              ).toFixed(1) + "%"
                            : "—"}
                        </span>
                      </div>
                    ) : null}
                    {membership.contribution ? (
                      <div className="flex flex-col gap-1">
                        <span className="text-xs text-muted-foreground">
                          {contributionMetricLabel(
                            membership.contribution.metric
                          )}
                        </span>
                        <span className="text-sm tabular-nums">
                          {formatContributionValue(
                            membership.contribution.metric,
                            membership.contribution.current_value
                          )}{" "}
                          /{" "}
                          {formatContributionValue(
                            membership.contribution.metric,
                            membership.contribution.target_value
                          )}
                        </span>
                        <Badge
                          variant={
                            membership.contribution.met
                              ? "outline"
                              : "secondary"
                          }
                        >
                          {membership.contribution.met ? "已达标" : "观察中"}
                        </Badge>
                      </div>
                    ) : null}
                  </div>
                ) : (
                  <span className="text-xs text-muted-foreground">—</span>
                )}
              </TableCell>
              <TableCell>{formatDateTime(membership.started_at)}</TableCell>
              <TableCell>
                <div className="flex justify-end gap-2">
                  <Button
                    size="xs"
                    variant="ghost"
                    onClick={() => onViewHistory(membership)}
                  >
                    <HistoryIcon data-icon="inline-start" />
                    历史
                  </Button>
                  {canManage ? (
                    <>
                      {membership.status === "active" ? (
                        <Button
                          size="xs"
                          variant="outline"
                          onClick={() => onChange(membership, "suspended")}
                        >
                          <PauseIcon data-icon="inline-start" />
                          暂停
                        </Button>
                      ) : membership.status === "suspended" ? (
                        <Button
                          size="xs"
                          variant="outline"
                          onClick={() => onChange(membership, "reactivated")}
                        >
                          <RotateCcwIcon data-icon="inline-start" />
                          恢复
                        </Button>
                      ) : null}
                      {membership.status !== "ended" ? (
                        <Button
                          size="xs"
                          variant="ghost"
                          onClick={() => onChange(membership, "ended")}
                        >
                          <StopCircleIcon data-icon="inline-start" />
                          结束
                        </Button>
                      ) : null}
                    </>
                  ) : null}
                </div>
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  )
}

function StaffContributionHistorySheet({
  membership,
  csrfToken,
  canIssueReminder,
  onOpenChange,
}: {
  membership?: WorkgroupMembership
  csrfToken: string
  canIssueReminder: boolean
  onOpenChange: (open: boolean) => void
}) {
  const [reminderCycle, setReminderCycle] =
    React.useState<WorkgroupContributionCycle>()
  const kind = membership?.group_kind ?? "review"
  const membershipId = membership?.id ?? "00000000-0000-0000-0000-000000000000"
  React.useEffect(() => setReminderCycle(undefined), [membershipId])
  const cycles = useQuery({
    ...adminWorkgroupContributionCyclesQueryOptions(kind, membershipId),
    enabled: Boolean(membership),
  })

  return (
    <>
      <Sheet open={Boolean(membership)} onOpenChange={onOpenChange}>
        <SheetContent className="w-full overflow-y-auto sm:max-w-3xl">
          <SheetHeader className="border-b pr-12">
            <SheetTitle>{membership?.username} · 贡献历史</SheetTitle>
            <SheetDescription>
              {membership
                ? `${groupLabel(membership.group_kind)} · 用户 ID ${membership.user_numeric_id}`
                : "工作组成员贡献历史"}
              。历史按成员状态变化与业务证据重建，仅供观察和人工核对。
            </SheetDescription>
          </SheetHeader>
          <div className="flex flex-1 flex-col gap-4 px-4 pb-4">
            {cycles.isPending ? <Skeleton className="h-56 w-full" /> : null}
            {cycles.isError ? (
              <Alert variant="destructive">
                <CircleAlertIcon />
                <AlertTitle>贡献历史暂时无法读取</AlertTitle>
                <AlertDescription>
                  {requestErrorDescription(
                    cycles.error,
                    "请稍后重试，历史证据不会被修改。"
                  )}
                </AlertDescription>
                <AlertAction>
                  <Button
                    size="sm"
                    variant="outline"
                    onClick={() => void cycles.refetch()}
                  >
                    <RefreshCwIcon data-icon="inline-start" />
                    重试
                  </Button>
                </AlertAction>
              </Alert>
            ) : null}
            {cycles.data?.items.length === 0 ? (
              <Empty className="border">
                <EmptyHeader>
                  <EmptyMedia variant="icon">
                    <HistoryIcon />
                  </EmptyMedia>
                  <EmptyTitle>还没有贡献周期</EmptyTitle>
                  <EmptyDescription>
                    成员资格生效后会从对应自然月开始形成历史。
                  </EmptyDescription>
                </EmptyHeader>
              </Empty>
            ) : null}
            {cycles.data?.items.length ? (
              <ContributionCycleHistory
                items={cycles.data.items}
                renderAction={
                  canIssueReminder
                    ? (cycle) =>
                        contributionReminderAllowed(cycle) ? (
                          <Button
                            size="xs"
                            variant="outline"
                            onClick={() => setReminderCycle(cycle)}
                          >
                            <BellRingIcon data-icon="inline-start" />
                            提醒
                          </Button>
                        ) : (
                          <span className="text-xs text-muted-foreground">
                            —
                          </span>
                        )
                    : undefined
                }
              />
            ) : null}
          </div>
        </SheetContent>
      </Sheet>
      {membership && reminderCycle ? (
        <ContributionReminderDialog
          membership={membership}
          cycle={reminderCycle}
          csrfToken={csrfToken}
          onOpenChange={(open) => !open && setReminderCycle(undefined)}
        />
      ) : null}
    </>
  )
}

function contributionReminderAllowed(cycle: WorkgroupContributionCycle) {
  return (
    cycle.reminder === null &&
    cycle.full_period_active &&
    cycle.current_value < cycle.target_value &&
    (cycle.evidence_state === "collecting" ||
      cycle.evidence_state === "complete") &&
    (cycle.assessment_state === "in_progress" ||
      cycle.assessment_state === "not_met")
  )
}

function ContributionReminderDialog({
  membership,
  cycle,
  csrfToken,
  onOpenChange,
}: {
  membership: WorkgroupMembership
  cycle: WorkgroupContributionCycle
  csrfToken: string
  onOpenChange: (open: boolean) => void
}) {
  const mutation = useIssueWorkgroupContributionReminder()
  const [reason, setReason] = React.useState("")
  const submit = async (event: React.FormEvent) => {
    event.preventDefault()
    try {
      await mutation.mutateAsync({
        csrfToken,
        idempotencyKey: crypto.randomUUID(),
        groupKind: membership.group_kind,
        membershipId: membership.id,
        periodStartsAt: cycle.period_starts_at,
        reason: reason.trim(),
      })
      onOpenChange(false)
    } catch {
      // Keep the immutable snapshot visible while staff reviews the API error.
    }
  }
  return (
    <Dialog open onOpenChange={onOpenChange}>
      <DialogContent>
        <form className="contents" onSubmit={submit}>
          <DialogHeader>
            <DialogTitle>发送贡献提醒</DialogTitle>
            <DialogDescription>
              将向 {membership.username} 发送一条站内消息，并冻结
              {formatCycleMonth(cycle.period_starts_at)}
              当时的贡献与证据快照。同一周期只能提醒一次。
            </DialogDescription>
          </DialogHeader>
          <FieldGroup>
            <div className="rounded-md border bg-muted/30 p-3 text-sm">
              <p>
                {contributionMetricLabel(cycle.metric)}：
                {formatContributionValue(cycle.metric, cycle.current_value)} /{" "}
                {formatContributionValue(cycle.metric, cycle.target_value)}
              </p>
              <p className="mt-1 text-muted-foreground">
                证据状态：
                {cycle.evidence_state === "complete" ? "完整" : "采集中"}
              </p>
            </div>
            <Field>
              <FieldLabel htmlFor="workgroup-contribution-reminder-reason">
                提醒说明
              </FieldLabel>
              <Textarea
                id="workgroup-contribution-reminder-reason"
                value={reason}
                onChange={(event) => setReason(event.target.value)}
                maxLength={1000}
                rows={5}
                placeholder="可留空；系统会自动记录提醒说明"
              />
              <FieldDescription>
                该说明会向成员显示；提醒不会自动暂停或结束成员资格。
              </FieldDescription>
            </Field>
            {mutation.isError ? (
              <FieldError>
                {requestErrorDescription(
                  mutation.error,
                  "提醒发送失败，请刷新贡献历史后重试。"
                )}
              </FieldError>
            ) : null}
          </FieldGroup>
          <DialogFooter>
            <DialogClose render={<Button type="button" variant="outline" />}>
              取消
            </DialogClose>
            <Button
              type="submit"
              disabled={mutation.isPending || [...reason.trim()].length > 1000}
            >
              {mutation.isPending ? (
                <Spinner data-icon="inline-start" />
              ) : (
                <BellRingIcon data-icon="inline-start" />
              )}
              发送提醒
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

function ApplicationDecisionDialog({
  state,
  csrfToken,
  onOpenChange,
}: {
  state: ApplicationDialogState
  csrfToken: string
  onOpenChange: (open: boolean) => void
}) {
  const mutation = useDecideWorkgroupApplication()
  const [reason, setReason] = React.useState("")
  const submit = async (event: React.FormEvent) => {
    event.preventDefault()
    try {
      await mutation.mutateAsync({
        csrfToken,
        idempotencyKey: crypto.randomUUID(),
        application: state.application,
        decision: state.decision,
        reason: reason.trim(),
      })
      onOpenChange(false)
    } catch {
      // Keep the dialog open so the typed API error remains visible.
    }
  }
  return (
    <Dialog open onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>
            {state.decision === "approve" ? "批准种审申请" : "驳回种审申请"}
          </DialogTitle>
          <DialogDescription>
            {state.application.applicant_username}（ID{" "}
            {state.application.applicant_numeric_id}）
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={submit} className="space-y-4">
          <MutationError error={mutation.error} />
          <Field>
            <FieldLabel htmlFor="application-decision-reason">
              审批理由
            </FieldLabel>
            <Textarea
              id="application-decision-reason"
              value={reason}
              maxLength={1000}
              onChange={(event) => setReason(event.target.value)}
            />
            <FieldDescription>
              可留空；系统会自动记录审批理由，决定与理由不可覆盖。
            </FieldDescription>
          </Field>
          <DialogFooter>
            <DialogClose render={<Button type="button" variant="outline" />}>
              取消
            </DialogClose>
            <Button
              type="submit"
              disabled={[...reason.trim()].length > 1000 || mutation.isPending}
            >
              {mutation.isPending ? "正在保存…" : "确认决定"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

function GrantMembershipDialog({
  open,
  groupKind,
  csrfToken,
  onOpenChange,
}: {
  open: boolean
  groupKind: WorkgroupKind
  csrfToken: string
  onOpenChange: (open: boolean) => void
}) {
  const mutation = useGrantWorkgroupMembership()
  const [userId, setUserId] = React.useState("")
  const [reason, setReason] = React.useState("")
  const submit = async (event: React.FormEvent) => {
    event.preventDefault()
    try {
      await mutation.mutateAsync({
        csrfToken,
        idempotencyKey: crypto.randomUUID(),
        groupKind,
        userNumericId: Number(userId),
        reason: reason.trim(),
      })
      setUserId("")
      setReason("")
      onOpenChange(false)
    } catch {
      // Keep the dialog open so the typed API error remains visible.
    }
  }
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>添加{groupLabel(groupKind)}成员</DialogTitle>
          <DialogDescription>
            使用后台显示的正整数用户 ID，不需要输入 UUID。
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={submit}>
          <FieldGroup>
            <MutationError error={mutation.error} />
            <Field>
              <FieldLabel htmlFor="workgroup-user-id">用户 ID</FieldLabel>
              <Input
                id="workgroup-user-id"
                inputMode="numeric"
                value={userId}
                onChange={(event) =>
                  setUserId(event.target.value.replace(/\D/g, ""))
                }
              />
            </Field>
            <Field>
              <FieldLabel htmlFor="workgroup-grant-reason">授予理由</FieldLabel>
              <Textarea
                id="workgroup-grant-reason"
                value={reason}
                maxLength={1000}
                onChange={(event) => setReason(event.target.value)}
              />
              <FieldDescription>
                可留空；系统会自动记录并作为成员历史的一部分。
              </FieldDescription>
            </Field>
          </FieldGroup>
          <DialogFooter className="mt-5">
            <DialogClose render={<Button type="button" variant="outline" />}>
              取消
            </DialogClose>
            <Button
              type="submit"
              disabled={
                !userId ||
                [...reason.trim()].length > 1000 ||
                mutation.isPending
              }
            >
              {mutation.isPending ? "正在保存…" : "确认添加"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

function MembershipChangeDialog({
  state,
  csrfToken,
  onOpenChange,
}: {
  state: MembershipDialogState
  csrfToken: string
  onOpenChange: (open: boolean) => void
}) {
  const mutation = useChangeWorkgroupMembership()
  const [reason, setReason] = React.useState("")
  const submit = async (event: React.FormEvent) => {
    event.preventDefault()
    try {
      await mutation.mutateAsync({
        csrfToken,
        idempotencyKey: crypto.randomUUID(),
        membership: state.membership,
        transition: state.transition,
        reason: reason.trim(),
      })
      onOpenChange(false)
    } catch {
      // Keep the dialog open so the typed API error remains visible.
    }
  }
  return (
    <Dialog open onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{transitionLabel(state.transition)}成员资格</DialogTitle>
          <DialogDescription>
            {state.membership.username}（ID {state.membership.user_numeric_id}）
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={submit} className="space-y-4">
          <MutationError error={mutation.error} />
          <Field>
            <FieldLabel htmlFor="membership-change-reason">变更理由</FieldLabel>
            <Textarea
              id="membership-change-reason"
              value={reason}
              maxLength={1000}
              onChange={(event) => setReason(event.target.value)}
            />
            <FieldDescription>
              成员权益按变更发生时间精确生效或停止。
            </FieldDescription>
          </Field>
          <DialogFooter>
            <DialogClose render={<Button type="button" variant="outline" />}>
              取消
            </DialogClose>
            <Button
              type="submit"
              disabled={[...reason.trim()].length > 1000 || mutation.isPending}
            >
              {mutation.isPending ? "正在保存…" : "确认变更"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

function Metric({ title, value }: { title: string; value: number }) {
  return (
    <Card size="sm">
      <CardHeader>
        <CardTitle className="text-2xl font-semibold tabular-nums">
          {value.toLocaleString("zh-CN")}
        </CardTitle>
        <CardDescription>{title}</CardDescription>
        <CardAction>
          <UsersRoundIcon className="size-5 text-muted-foreground" />
        </CardAction>
      </CardHeader>
    </Card>
  )
}

function ReadError({
  error,
  onRetry,
}: {
  error: unknown
  onRetry: () => void
}) {
  return (
    <Alert variant="destructive">
      <CircleAlertIcon />
      <AlertTitle>工作组数据暂时无法读取</AlertTitle>
      <AlertDescription>
        {requestErrorDescription(error, "请稍后重试。")}
      </AlertDescription>
      <Button variant="outline" size="sm" onClick={onRetry}>
        <RefreshCwIcon data-icon="inline-start" />
        重试
      </Button>
    </Alert>
  )
}

function MutationError({ error }: { error: unknown }) {
  return error ? (
    <Alert variant="destructive">
      <CircleAlertIcon />
      <AlertTitle>保存失败</AlertTitle>
      <AlertDescription>
        {requestErrorDescription(error, "请刷新数据后重试。")}
      </AlertDescription>
    </Alert>
  ) : null
}

function OverviewSkeleton() {
  return (
    <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
      {Array.from({ length: 4 }).map((_, index) => (
        <Skeleton key={index} className="h-24" />
      ))}
    </div>
  )
}

function groupLabel(kind: WorkgroupKind) {
  return groupOptions.find((option) => option.value === kind)?.label ?? kind
}

function monthInputValue(value: string) {
  const date = new Date(value)
  const month = String(date.getUTCMonth() + 1).padStart(2, "0")
  return date.getUTCFullYear() + "-" + month
}

function monthToEffectiveFrom(value: string) {
  return value + "-01T00:00:00.000Z"
}

function formatPolicyMonth(value: string) {
  const date = new Date(value)
  return date.getUTCFullYear() + " 年 " + (date.getUTCMonth() + 1) + " 月"
}

function contributionTargetUnit(kind: WorkgroupKind) {
  return kind === "reseed"
    ? "发布种子数"
    : kind === "review"
      ? "有效审核票数"
      : "累计做种天数"
}

function contributionTargetValue(kind: WorkgroupKind, value: number) {
  return kind === "retention" ? value * 86_400 : value
}

function contributionTargetError(
  kind: WorkgroupKind,
  rawValue: string,
  value: number
) {
  if (!rawValue) return "请输入贡献目标。"
  if (!Number.isSafeInteger(value) || value < 1) return "目标必须是正整数。"
  const maximum =
    kind === "reseed" ? 100_000 : kind === "review" ? 1_000_000 : 36_500
  if (value > maximum) {
    return "目标不能超过 " + maximum.toLocaleString("zh-CN") + "。"
  }
  return undefined
}

function membershipStatusLabel(status: string) {
  return status === "active"
    ? "有效"
    : status === "suspended"
      ? "已暂停"
      : "已结束"
}

function transitionLabel(transition: MembershipTransition) {
  return transition === "suspended"
    ? "暂停"
    : transition === "reactivated"
      ? "恢复"
      : "结束"
}

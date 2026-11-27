import * as React from "react"
import { useQuery } from "@tanstack/react-query"
import { Link } from "react-router"
import {
  CalendarRangeIcon,
  CheckCircle2Icon,
  CircleAlertIcon,
  Clock3Icon,
  FileCheck2Icon,
  LogInIcon,
  RefreshCwIcon,
  Repeat2Icon,
  SendIcon,
  ShieldCheckIcon,
  XCircleIcon,
} from "lucide-react"

import {
  Alert,
  AlertAction,
  AlertDescription,
  AlertTitle,
} from "~/components/ui/alert"
import { Badge } from "~/components/ui/badge"
import { Button, buttonVariants } from "~/components/ui/button"
import {
  Card,
  CardDescription,
  CardFooter,
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
import { Field, FieldDescription, FieldLabel } from "~/components/ui/field"
import {
  Progress,
  ProgressLabel,
  ProgressValue,
} from "~/components/ui/progress"
import { Skeleton } from "~/components/ui/skeleton"
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "~/components/ui/empty"
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "~/components/ui/sheet"
import { Spinner } from "~/components/ui/spinner"
import { Textarea } from "~/components/ui/textarea"
import { useWebSession } from "~/features/auth/api/session.mutations"
import {
  type MyWorkgroupOverview,
  myWorkgroupContributionCyclesQueryOptions,
  useCreateWorkgroupApplication,
  useMyWorkgroups,
} from "~/features/workgroups/api/workgroups.queries"
import { ContributionCycleHistory } from "~/features/workgroups/components/contribution-cycle-history"
import {
  RetentionProgressPanel,
  RetentionTargetList,
} from "~/features/workgroups/components/workgroup-monthly"
import { WorkgroupTaskPanel } from "~/features/workgroups/components/workgroup-task-panel"
import {
  contributionMetricLabel,
  contributionPercent,
  formatContributionValue,
} from "~/features/workgroups/model/contribution-format"
import { cn } from "~/lib/utils"
import { PageHeader, PageLayout } from "~/shared/components/page-layout"
import { requestErrorDescription } from "~/shared/api/problem"
import { formatBytes } from "~/shared/formatters/bytes"
import { formatDateTime } from "~/shared/formatters/date-time"

type MyWorkgroup = MyWorkgroupOverview["items"][number]

const groupIcons = {
  reseed: Repeat2Icon,
  review: FileCheck2Icon,
  retention: ShieldCheckIcon,
}

export function WorkgroupsPage() {
  const session = useWebSession()
  const workgroups = useMyWorkgroups(session.data?.user.id)

  return (
    <PageLayout className="gap-5">
      <PageHeader
        title="工作组"
        description="查看转种、种审和保种职责；工作组权益只在有效成员期内生效。"
      />

      {session.isPending ? <WorkgroupSkeleton /> : null}
      {session.isError ? (
        <Alert variant="destructive">
          <CircleAlertIcon />
          <AlertTitle>会话状态暂时无法读取</AlertTitle>
          <AlertDescription>
            {requestErrorDescription(
              session.error,
              "会话请求未能完成，请稍后刷新页面。"
            )}
          </AlertDescription>
        </Alert>
      ) : null}
      {!session.isPending && !session.isError && !session.data ? (
        <Card>
          <CardHeader>
            <CardTitle>需要登录</CardTitle>
            <CardDescription>
              登录后可查看工作组资格和申请条件。
            </CardDescription>
          </CardHeader>
          <CardFooter>
            <Link to="/login" className={buttonVariants()}>
              <LogInIcon data-icon="inline-start" />
              前往登录
            </Link>
          </CardFooter>
        </Card>
      ) : null}
      {session.data && workgroups.isPending ? <WorkgroupSkeleton /> : null}
      {session.data && workgroups.isError ? (
        <Alert variant="destructive">
          <CircleAlertIcon />
          <AlertTitle>工作组状态暂时无法读取</AlertTitle>
          <AlertDescription>
            {requestErrorDescription(
              workgroups.error,
              "暂时无法取得工作组数据，请稍后重试。"
            )}
          </AlertDescription>
          <AlertAction>
            <Button
              variant="outline"
              size="sm"
              onClick={() => void workgroups.refetch()}
            >
              <RefreshCwIcon data-icon="inline-start" />
              重试
            </Button>
          </AlertAction>
        </Alert>
      ) : null}
      {session.data && workgroups.data ? (
        <WorkgroupContent
          items={workgroups.data.items}
          csrfToken={session.data.csrf_token}
          userId={session.data.user.id}
        />
      ) : null}
    </PageLayout>
  )
}

function WorkgroupContent({
  items,
  csrfToken,
  userId,
}: {
  items: MyWorkgroup[]
  csrfToken: string
  userId: string
}) {
  const [historyItem, setHistoryItem] = React.useState<MyWorkgroup>()
  const [applicationItem, setApplicationItem] = React.useState<MyWorkgroup>()
  const memberships = items.filter(
    (item) =>
      item.membership?.status === "active" ||
      item.membership?.status === "suspended"
  )
  const retention = items.find(
    (item) => item.definition.kind === "retention"
  )?.membership

  return (
    <>
      {memberships.length > 0 ? (
        <section
          aria-labelledby="my-workgroups-title"
          className="flex flex-col gap-3"
        >
          <SectionTitle id="my-workgroups-title">我的工作组</SectionTitle>
          <div
            className={cn(
              "grid gap-3 sm:gap-4",
              memberships.length > 1 && "lg:grid-cols-2"
            )}
          >
            {memberships.map((item) => (
              <MembershipCard
                key={item.definition.kind}
                item={item}
                onViewHistory={() => setHistoryItem(item)}
              />
            ))}
          </div>
        </section>
      ) : null}

      {retention ? <RetentionProgressPanel userKey={userId} /> : null}
      {retention?.status === "active" ? (
        <RetentionTargetList
          csrfToken={csrfToken}
          staff={false}
          userKey={userId}
        />
      ) : null}
      <WorkgroupTaskPanel userId={userId} csrfToken={csrfToken} />

      <section
        aria-labelledby="all-workgroups-title"
        className="flex flex-col gap-3"
      >
        <SectionTitle id="all-workgroups-title">全部工作组</SectionTitle>
        <Card className="gap-0 py-0">
          <ul className="divide-y">
            {items.map((item) => (
              <WorkgroupRow
                key={item.definition.kind}
                item={item}
                onApply={() => setApplicationItem(item)}
              />
            ))}
          </ul>
        </Card>
      </section>

      <MyContributionHistorySheet
        item={historyItem}
        userId={userId}
        onOpenChange={(open) => !open && setHistoryItem(undefined)}
      />
      <ReviewApplicationDialog
        item={applicationItem}
        userId={userId}
        csrfToken={csrfToken}
        onOpenChange={(open) => !open && setApplicationItem(undefined)}
      />
    </>
  )
}

function SectionTitle({
  id,
  children,
}: {
  id: string
  children: React.ReactNode
}) {
  return (
    <h2 id={id} className="text-md font-semibold tracking-title">
      {children}
    </h2>
  )
}

function WorkgroupIcon({
  item,
  className,
}: {
  item: MyWorkgroup
  className?: string
}) {
  const Icon = groupIcons[item.definition.kind]
  return (
    <span
      aria-hidden="true"
      className={cn(
        "flex shrink-0 items-center justify-center rounded-lg bg-primary/10 text-link",
        className
      )}
    >
      <Icon />
    </span>
  )
}

function MembershipCard({
  item,
  onViewHistory,
}: {
  item: MyWorkgroup
  onViewHistory: () => void
}) {
  const membership = item.membership
  if (!membership) return null

  return (
    <Card className="gap-0 py-0">
      <div className="flex flex-wrap items-start gap-3 p-4 sm:p-5">
        <WorkgroupIcon item={item} className="size-10 [&_svg]:size-5" />
        <div className="flex min-w-0 flex-1 flex-col gap-0.5">
          <div className="flex flex-wrap items-center gap-2">
            <h3 className="text-md font-semibold tracking-title">
              {item.definition.display_name}
            </h3>
            <WorkgroupStateBadge item={item} />
          </div>
          <p className="text-xs text-muted-foreground">
            成员资格{membershipStatusLabel(membership.status)} · 自{" "}
            {formatDateTime(membership.started_at)}
          </p>
        </div>
        <Button
          variant="outline"
          size="sm"
          className="max-sm:w-full"
          onClick={onViewHistory}
        >
          <CalendarRangeIcon data-icon="inline-start" />
          查看贡献历史
        </Button>
      </div>
      {membership.contribution ? (
        <div className="border-t px-4 py-4 sm:px-5">
          <ContributionSummary contribution={membership.contribution} />
        </div>
      ) : null}
      <p className="border-t bg-muted/50 px-4 py-3 text-xs text-muted-foreground sm:px-5">
        <span className="font-medium text-foreground">成员权益</span>
        <span aria-hidden="true" className="mx-1.5 text-faint">
          ·
        </span>
        {entitlementLabel(item.definition.entitlement)}
      </p>
    </Card>
  )
}

function ContributionSummary({
  contribution,
}: {
  contribution: NonNullable<MyWorkgroup["membership"]>["contribution"]
}) {
  if (!contribution) return null
  const current = formatContributionValue(
    contribution.metric,
    contribution.current_value
  )
  const target = formatContributionValue(
    contribution.metric,
    contribution.target_value
  )
  return (
    <div className="flex flex-col gap-2.5">
      <Progress
        value={contributionPercent(
          contribution.current_value,
          contribution.target_value
        )}
      >
        <ProgressLabel>
          {contributionMetricLabel(contribution.metric)}
        </ProgressLabel>
        <ProgressValue className="font-mono tabular-nums">
          {() => `${current} / ${target}`}
        </ProgressValue>
      </Progress>
      <div className="flex flex-wrap items-center justify-between gap-2 text-xs text-faint">
        <span>
          周期至 {formatDateTime(contribution.period_ends_at)}
          {contribution.evidence_through
            ? ` · 证据截至 ${formatDateTime(contribution.evidence_through)}`
            : null}
        </span>
        <Badge variant={contribution.met ? "success" : "primary"}>
          {contribution.met ? "本月已达标" : "进行中"}
        </Badge>
      </div>
      <p className="text-xs text-muted-foreground">
        {contribution.enforcement_mode === "miss_limit"
          ? `完整自然月未达标将记录标记；当前 ${contribution.miss_count}/${contribution.allowed_misses} 次，第 ${contribution.allowed_misses + 1} 次自动结束资格。`
          : contribution.metric === "seeding_active_seconds"
            ? "保种考核的达标种数和体积可在逐种保种进度查看。"
            : "当前为观察目标，未达标不会自动变更工作组权益。"}
      </p>
    </div>
  )
}

function WorkgroupRow({
  item,
  onApply,
}: {
  item: MyWorkgroup
  onApply: () => void
}) {
  const active = item.membership?.status === "active"
  const pending = item.application?.status === "pending"
  const showEligibility =
    item.definition.kind === "review" &&
    Boolean(item.eligibility) &&
    !active &&
    !pending

  return (
    <li className="flex flex-col gap-3 p-4 sm:flex-row sm:items-start sm:gap-4 sm:px-5">
      <div className="flex min-w-0 flex-1 gap-3">
        <WorkgroupIcon item={item} className="size-9 [&_svg]:size-4" />
        <div className="flex min-w-0 flex-1 flex-col gap-1">
          <div className="flex flex-wrap items-center gap-2">
            <h3 className="text-sm font-semibold">
              {item.definition.display_name}
            </h3>
            <WorkgroupStateBadge item={item} />
          </div>
          <p className="text-xs text-muted-foreground">
            {item.definition.description}
          </p>
          <p className="text-xs text-muted-foreground">
            <span className="text-faint">权益：</span>
            {entitlementLabel(item.definition.entitlement)}
          </p>
          {item.application ? (
            <p className="text-xs text-faint">
              最近申请：{applicationStatusLabel(item.application.status)} ·{" "}
              {formatDateTime(item.application.submitted_at)}
            </p>
          ) : null}
          {showEligibility ? <ReviewerEligibility item={item} /> : null}
        </div>
      </div>
      <WorkgroupRowAction item={item} onApply={onApply} />
    </li>
  )
}

function WorkgroupRowAction({
  item,
  onApply,
}: {
  item: MyWorkgroup
  onApply: () => void
}) {
  if (item.membership?.status === "active") {
    return (
      <span className="flex shrink-0 items-center gap-1 text-xs text-success-foreground sm:self-center">
        <CheckCircle2Icon className="size-3.5" />
        已加入
      </span>
    )
  }
  if (item.application?.status === "pending") {
    return (
      <span className="shrink-0 text-xs text-faint sm:self-center">
        等待管理员审批
      </span>
    )
  }
  if (item.definition.kind === "review" && item.eligibility) {
    return (
      <Button
        size="sm"
        className="shrink-0 max-sm:w-full sm:self-center"
        disabled={!item.eligibility.eligible}
        onClick={onApply}
      >
        {item.eligibility.eligible ? "申请加入" : "暂不满足条件"}
      </Button>
    )
  }
  return (
    <span className="shrink-0 text-xs text-faint sm:self-center">
      {item.definition.join_mode === "staff_only"
        ? "由管理员任命"
        : "暂未开放申请"}
    </span>
  )
}

function MyContributionHistorySheet({
  item,
  userId,
  onOpenChange,
}: {
  item?: MyWorkgroup
  userId: string
  onOpenChange: (open: boolean) => void
}) {
  const kind = item?.definition.kind ?? "review"
  const cycles = useQuery({
    ...myWorkgroupContributionCyclesQueryOptions(userId, kind),
    enabled: Boolean(item),
  })

  return (
    <Sheet open={Boolean(item)} onOpenChange={onOpenChange}>
      <SheetContent className="w-full overflow-y-auto sm:max-w-3xl">
        <SheetHeader className="border-b pr-12">
          <SheetTitle>{item?.definition.display_name}贡献历史</SheetTitle>
          <SheetDescription>
            按 UTC 自然月重建成员有效期、业务证据和达标结果。
            {item?.membership?.contribution?.enforcement_mode === "miss_limit"
              ? `未达标会累计标记，超过 ${item.membership.contribution.allowed_misses} 次后结束资格。`
              : "当前仅观察，不会自动变更资格。"}
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
                  "请稍后重试，历史评估不会因此改变。"
                )}
              </AlertDescription>
              <AlertAction>
                <Button
                  variant="outline"
                  size="sm"
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
                  <CalendarRangeIcon />
                </EmptyMedia>
                <EmptyTitle>还没有贡献周期</EmptyTitle>
                <EmptyDescription>
                  成员资格生效后会从对应自然月开始形成历史。
                </EmptyDescription>
              </EmptyHeader>
            </Empty>
          ) : null}
          {cycles.data?.items.length ? (
            <ContributionCycleHistory items={cycles.data.items} />
          ) : null}
        </div>
      </SheetContent>
    </Sheet>
  )
}

function ReviewerEligibility({ item }: { item: MyWorkgroup }) {
  const eligibility = item.eligibility
  if (!eligibility) return null
  const checks = [
    {
      label: `等级 Lv.${eligibility.minimum_level}`,
      met: eligibility.level >= eligibility.minimum_level,
      current: `当前 Lv.${eligibility.level}`,
    },
    {
      label: `有效上传 ${formatBytes(eligibility.minimum_credited_uploaded_bytes)}`,
      met:
        BigInt(eligibility.credited_uploaded_bytes) >=
        BigInt(eligibility.minimum_credited_uploaded_bytes),
      current: `当前 ${formatBytes(eligibility.credited_uploaded_bytes)}`,
    },
    {
      label: `注册满 ${eligibility.minimum_account_age_days} 天`,
      met: eligibility.account_age_days >= eligibility.minimum_account_age_days,
      current: `当前 ${eligibility.account_age_days} 天`,
    },
    {
      label: "邮箱已验证",
      met: eligibility.email_verified,
      current: eligibility.email_verified ? "已验证" : "未验证",
    },
    {
      label: "下载状态正常",
      met: !eligibility.download_restricted && eligibility.account_active,
      current: eligibility.download_restricted ? "下载受限" : "正常",
    },
  ]
  return (
    <div className="mt-1.5 flex flex-col gap-1.5">
      <p className="text-xs font-medium text-foreground">种审申请条件</p>
      <ul className="grid gap-x-4 gap-y-1 text-xs sm:grid-cols-2">
        {checks.map((check) => (
          <li key={check.label} className="flex items-start gap-1.5">
            {check.met ? (
              <CheckCircle2Icon
                aria-label="已满足"
                className="mt-0.5 size-3.5 shrink-0 text-success-foreground"
              />
            ) : (
              <XCircleIcon
                aria-label="未满足"
                className="mt-0.5 size-3.5 shrink-0 text-destructive"
              />
            )}
            <span>
              {check.label}
              <span className="ml-1 text-faint">（{check.current}）</span>
            </span>
          </li>
        ))}
      </ul>
    </div>
  )
}

function ReviewApplicationDialog({
  item,
  userId,
  csrfToken,
  onOpenChange,
}: {
  item?: MyWorkgroup
  userId: string
  csrfToken: string
  onOpenChange: (open: boolean) => void
}) {
  const createApplication = useCreateWorkgroupApplication(userId)
  const [statement, setStatement] = React.useState("")
  const valid = statement.trim().length >= 20

  React.useEffect(() => {
    if (item) {
      setStatement("")
      createApplication.reset()
    }
  }, [item]) // eslint-disable-line react-hooks/exhaustive-deps

  return (
    <Dialog open={Boolean(item)} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>申请加入种审组</DialogTitle>
          <DialogDescription>
            提交后由管理员审批，审批记录会长期保留。
          </DialogDescription>
        </DialogHeader>
        {createApplication.isError ? (
          <Alert variant="destructive">
            <CircleAlertIcon />
            <AlertTitle>申请提交失败</AlertTitle>
            <AlertDescription>
              {requestErrorDescription(
                createApplication.error,
                "请检查申请说明后重试。"
              )}
            </AlertDescription>
          </Alert>
        ) : null}
        <Field data-invalid={statement.length > 0 && !valid}>
          <FieldLabel htmlFor="review-workgroup-statement">申请说明</FieldLabel>
          <Textarea
            id="review-workgroup-statement"
            value={statement}
            minLength={20}
            maxLength={1000}
            rows={5}
            placeholder="说明你对站点规则、资源质量和审核工作的理解。"
            disabled={createApplication.isPending}
            onChange={(event) => setStatement(event.target.value)}
          />
          <FieldDescription>
            20 至 1000 个字符，当前 {statement.trim().length} 个。
          </FieldDescription>
        </Field>
        <DialogFooter>
          <DialogClose
            render={
              <Button
                variant="outline"
                disabled={createApplication.isPending}
              />
            }
          >
            取消
          </DialogClose>
          <Button
            disabled={!valid || createApplication.isPending}
            onClick={() => {
              void createApplication
                .mutateAsync({
                  csrfToken,
                  idempotencyKey: globalThis.crypto.randomUUID(),
                  groupKind: "review",
                  statement: statement.trim(),
                })
                .then(() => onOpenChange(false))
                .catch(() => undefined)
            }}
          >
            {createApplication.isPending ? (
              <Spinner data-icon="inline-start" />
            ) : (
              <SendIcon data-icon="inline-start" />
            )}
            {createApplication.isPending ? "正在提交…" : "提交申请"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function WorkgroupStateBadge({ item }: { item: MyWorkgroup }) {
  if (item.membership?.status === "active") {
    return <Badge variant="success">有效成员</Badge>
  }
  if (item.membership?.status === "suspended") {
    return <Badge variant="destructive">已暂停</Badge>
  }
  if (item.application?.status === "pending") {
    return (
      <Badge variant="warning">
        <Clock3Icon data-icon="inline-start" />
        审批中
      </Badge>
    )
  }
  if (item.membership?.status === "ended") {
    return <Badge variant="secondary">已结束</Badge>
  }
  return <Badge variant="secondary">未加入</Badge>
}

function entitlementLabel(value: string) {
  switch (value) {
    case "torrent.publish.trusted":
      return "通过机器校验后跳过人工审核并保留直发证据"
    case "torrent.review.vote":
      return "参与多人种子审核投票；不包含管理员最终裁决"
    case "traffic.download.charge_exempt":
      return "成员有效期内原始下载照常记录，计费下载为零"
    default:
      return value
  }
}

function membershipStatusLabel(value: string) {
  return value === "active"
    ? "有效"
    : value === "suspended"
      ? "已暂停"
      : "已结束"
}

function applicationStatusLabel(value: string) {
  return value === "pending"
    ? "审批中"
    : value === "approved"
      ? "已批准"
      : "已驳回"
}

function WorkgroupSkeleton() {
  return (
    <div className="flex flex-col gap-4" aria-busy="true">
      <Skeleton className="h-5 w-24" />
      <Skeleton className="h-44 w-full rounded-xl" />
      <Skeleton className="h-5 w-28" />
      <Skeleton className="h-56 w-full rounded-xl" />
    </div>
  )
}

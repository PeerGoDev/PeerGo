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
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from "~/components/ui/card"
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
import { Textarea } from "~/components/ui/textarea"
import { useWebSession } from "~/features/auth/api/session.mutations"
import {
  type MyWorkgroupOverview,
  myWorkgroupContributionCyclesQueryOptions,
  useCreateWorkgroupApplication,
  useMyWorkgroups,
} from "~/features/workgroups/api/workgroups.queries"
import { ContributionCycleHistory } from "~/features/workgroups/components/contribution-cycle-history"
import { WorkgroupTaskPanel } from "~/features/workgroups/components/workgroup-task-panel"
import {
  contributionMetricLabel,
  contributionPercent,
  formatContributionValue,
} from "~/features/workgroups/model/contribution-format"
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
    <PageLayout>
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

  return (
    <>
      <div className="grid gap-4 xl:grid-cols-3">
        {items.map((item) => (
          <WorkgroupCard
            key={item.definition.kind}
            item={item}
            csrfToken={csrfToken}
            userId={userId}
            onViewHistory={() => setHistoryItem(item)}
          />
        ))}
      </div>
      <MyContributionHistorySheet
        item={historyItem}
        userId={userId}
        onOpenChange={(open) => !open && setHistoryItem(undefined)}
      />
      <WorkgroupTaskPanel userId={userId} csrfToken={csrfToken} />
    </>
  )
}

function WorkgroupCard({
  item,
  csrfToken,
  userId,
  onViewHistory,
}: {
  item: MyWorkgroup
  csrfToken: string
  userId: string
  onViewHistory: () => void
}) {
  const Icon = groupIcons[item.definition.kind]
  return (
    <Card className="h-fit">
      <CardHeader>
        <div className="flex items-start justify-between gap-3">
          <span className="flex size-10 items-center justify-center rounded-lg bg-primary/10 text-primary">
            <Icon className="size-5" />
          </span>
          <WorkgroupStateBadge item={item} />
        </div>
        <CardTitle>{item.definition.display_name}</CardTitle>
        <CardDescription>{item.definition.description}</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <div className="rounded-md border bg-muted/35 p-3 text-sm">
          <p className="font-medium">生效权益</p>
          <p className="mt-1 text-muted-foreground">
            {entitlementLabel(item.definition.entitlement)}
          </p>
        </div>
        {item.membership ? <MembershipSummary item={item} /> : null}
        {item.membership?.contribution ? (
          <ContributionSummary contribution={item.membership.contribution} />
        ) : null}
        {item.membership ? (
          <Button variant="outline" size="sm" onClick={onViewHistory}>
            <CalendarRangeIcon data-icon="inline-start" />
            查看贡献历史
          </Button>
        ) : null}
        {item.application ? <ApplicationSummary item={item} /> : null}
        {item.definition.kind === "review" && item.eligibility ? (
          <ReviewerEligibility item={item} />
        ) : null}
      </CardContent>
      {item.definition.kind === "review" && item.eligibility ? (
        <ReviewApplicationFooter
          item={item}
          csrfToken={csrfToken}
          userId={userId}
        />
      ) : null}
    </Card>
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

function MembershipSummary({ item }: { item: MyWorkgroup }) {
  if (!item.membership) return null
  return (
    <div className="flex flex-col gap-1 text-sm">
      <p className="font-medium">成员资格</p>
      <p className="text-muted-foreground">
        {membershipStatusLabel(item.membership.status)} · 自{" "}
        {formatDateTime(item.membership.started_at)}
      </p>
    </div>
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
    <div className="flex flex-col gap-2 rounded-md border p-3">
      <Progress
        value={contributionPercent(
          contribution.current_value,
          contribution.target_value
        )}
      >
        <ProgressLabel>
          {contributionMetricLabel(contribution.metric)}
        </ProgressLabel>
        <ProgressValue>{() => `${current} / ${target}`}</ProgressValue>
      </Progress>
      <div className="flex flex-wrap items-center justify-between gap-2 text-xs text-muted-foreground">
        <span>
          周期至 {formatDateTime(contribution.period_ends_at)}
          {contribution.evidence_through
            ? ` · 证据截至 ${formatDateTime(contribution.evidence_through)}`
            : null}
        </span>
        <Badge variant={contribution.met ? "outline" : "secondary"}>
          {contribution.met ? "本月已达标" : "进行中"}
        </Badge>
      </div>
      <p className="text-xs text-muted-foreground">
        {contribution.enforcement_mode === "miss_limit"
          ? `完整自然月未达标将记录标记；当前 ${contribution.miss_count}/${contribution.allowed_misses} 次，第 ${contribution.allowed_misses + 1} 次自动结束资格。`
          : "当前为观察目标，未达标不会自动变更工作组权益。"}
      </p>
    </div>
  )
}

function ApplicationSummary({ item }: { item: MyWorkgroup }) {
  if (!item.application) return null
  return (
    <div className="flex flex-col gap-1 text-sm">
      <p className="font-medium">最近申请</p>
      <p className="text-muted-foreground">
        {applicationStatusLabel(item.application.status)} ·{" "}
        {formatDateTime(item.application.submitted_at)}
      </p>
    </div>
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
    <div className="flex flex-col gap-2">
      <p className="text-sm font-medium">种审申请条件</p>
      <ul className="flex flex-col gap-2 text-sm">
        {checks.map((check) => (
          <li key={check.label} className="flex items-start gap-2">
            {check.met ? (
              <CheckCircle2Icon className="mt-0.5 size-4 shrink-0 text-success" />
            ) : (
              <XCircleIcon className="mt-0.5 size-4 shrink-0 text-destructive" />
            )}
            <span>
              {check.label}
              <span className="ml-1 text-muted-foreground">
                （{check.current}）
              </span>
            </span>
          </li>
        ))}
      </ul>
    </div>
  )
}

function ReviewApplicationFooter({
  item,
  csrfToken,
  userId,
}: {
  item: MyWorkgroup
  csrfToken: string
  userId: string
}) {
  const createApplication = useCreateWorkgroupApplication(userId)
  const [statement, setStatement] = React.useState("")
  const pending = item.application?.status === "pending"
  const active = item.membership?.status === "active"
  if (pending || active || !item.eligibility?.eligible) {
    return null
  }
  return (
    <CardFooter className="flex-col items-stretch gap-3 border-t">
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
      <Field>
        <FieldLabel htmlFor="review-workgroup-statement">申请说明</FieldLabel>
        <Textarea
          id="review-workgroup-statement"
          value={statement}
          minLength={20}
          maxLength={1000}
          rows={4}
          placeholder="说明你对站点规则、资源质量和审核工作的理解。"
          onChange={(event) => setStatement(event.target.value)}
        />
        <FieldDescription>
          20 至 1000 个字符，审批记录会长期保留。
        </FieldDescription>
      </Field>
      <Button
        disabled={statement.trim().length < 20 || createApplication.isPending}
        onClick={() => {
          void createApplication
            .mutateAsync({
              csrfToken,
              idempotencyKey: globalThis.crypto.randomUUID(),
              groupKind: "review",
              statement: statement.trim(),
            })
            .catch(() => undefined)
        }}
      >
        {createApplication.isPending ? "正在提交…" : "申请加入种审组"}
      </Button>
    </CardFooter>
  )
}

function WorkgroupStateBadge({ item }: { item: MyWorkgroup }) {
  if (item.membership?.status === "active") {
    return <Badge variant="outline">有效成员</Badge>
  }
  if (item.membership?.status === "suspended") {
    return <Badge variant="destructive">已暂停</Badge>
  }
  if (item.application?.status === "pending") {
    return (
      <Badge variant="secondary">
        <Clock3Icon data-icon="inline-start" />
        审批中
      </Badge>
    )
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
    <div className="grid gap-4 xl:grid-cols-3">
      {Array.from({ length: 3 }).map((_, index) => (
        <Card key={index}>
          <CardHeader>
            <Skeleton className="size-10 rounded-lg" />
            <Skeleton className="h-6 w-24" />
            <Skeleton className="h-4 w-full" />
          </CardHeader>
          <CardContent className="flex flex-col gap-3">
            <Skeleton className="h-16 w-full" />
            <Skeleton className="h-4 w-2/3" />
          </CardContent>
        </Card>
      ))}
    </div>
  )
}

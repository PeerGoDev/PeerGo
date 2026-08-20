import { useState, type ReactNode } from "react"
import { Link } from "react-router"
import {
  ArrowDownToLineIcon,
  ArrowUpFromLineIcon,
  CircleAlertIcon,
  CircleCheckBigIcon,
  ClockAlertIcon,
  GaugeIcon,
  InfoIcon,
  LogInIcon,
  MessageSquareWarningIcon,
  RefreshCwIcon,
  ShieldCheckIcon,
  ShieldXIcon,
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
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "~/components/ui/card"
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "~/components/ui/empty"
import {
  Progress,
  ProgressLabel,
  ProgressValue,
} from "~/components/ui/progress"
import { Skeleton } from "~/components/ui/skeleton"
import { useWebSession } from "~/features/auth/api/session.mutations"
import { useCapabilities } from "~/features/authz/api/capabilities.queries"
import {
  type MyRatioWatch,
  useMyRatioWatch,
} from "~/features/traffic/api/ratio-watch.queries"
import { RatioWatchAppealDialog } from "~/features/traffic/components/ratio-watch-appeal-dialog"
import { formatShareRatio } from "~/features/traffic/model/format"
import {
  formatDeadlineRemaining,
  formatRatioBasisPoints,
  formatWatchPeriod,
} from "~/features/traffic/model/ratio-watch-format"
import { requestErrorDescription } from "~/shared/api/problem"
import { MetricCard } from "~/shared/components/metric-card"
import { PageHeader, PageLayout } from "~/shared/components/page-layout"
import { formatBytes } from "~/shared/formatters/bytes"
import { formatDateTime } from "~/shared/formatters/date-time"

export function RatioWatchPage() {
  const session = useWebSession()
  const capabilities = useCapabilities(session.data?.user.id)
  const canRead = Boolean(
    capabilities.data?.items.some(
      (capability) => capability.action === "ratio.assessment.read.self"
    )
  )
  const canCreateAppeal = Boolean(
    capabilities.data?.items.some(
      (capability) => capability.action === "ratio.appeal.create.self"
    )
  )
  const ratioWatch = useMyRatioWatch(
    session.data?.user.id,
    Boolean(capabilities.data && canRead)
  )

  return (
    <PageLayout>
      <PageHeader
        title="分享率考核"
        description="查看全站有效分享率规则、当前考核进度和恢复目标。"
      />

      {session.isPending && <RatioWatchSkeleton />}

      {session.isError && (
        <ErrorAlert
          title="会话状态暂时无法读取"
          description={requestErrorDescription(
            session.error,
            "会话请求未能完成，请稍后刷新页面。"
          )}
          retry={() => void session.refetch()}
        />
      )}

      {!session.isPending && !session.isError && !session.data && (
        <AccessCard
          icon={<LogInIcon />}
          title="登录后查看分享率考核"
          description="考核进度只对本人可见。"
          action={
            <Link to="/login" className={buttonVariants()}>
              <LogInIcon data-icon="inline-start" />
              前往登录
            </Link>
          }
        />
      )}

      {session.data && capabilities.isPending && <RatioWatchSkeleton />}

      {session.data && capabilities.isError && (
        <ErrorAlert
          title="暂时无法读取查看权限"
          description={requestErrorDescription(
            capabilities.error,
            "权限请求未能完成，请稍后再试。"
          )}
          retry={() => void capabilities.refetch()}
        />
      )}

      {session.data && capabilities.data && !canRead && (
        <AccessCard
          icon={<ShieldXIcon />}
          title="当前账户不能查看分享率考核"
          description="如有疑问，请联系站点管理人员。"
          action={
            <Link to="/" className={buttonVariants({ variant: "outline" })}>
              返回首页
            </Link>
          }
        />
      )}

      {session.data && canRead && ratioWatch.isPending && (
        <RatioWatchSkeleton />
      )}

      {session.data && canRead && ratioWatch.isError && (
        <ErrorAlert
          title="分享率考核暂时无法读取"
          description={requestErrorDescription(
            ratioWatch.error,
            "分享率考核请求未能完成，请稍后再试。"
          )}
          retry={() => void ratioWatch.refetch()}
        />
      )}

      {session.data && canRead && ratioWatch.data && (
        <RatioWatchContent
          status={ratioWatch.data}
          userId={session.data.user.id}
          csrfToken={session.data.csrf_token}
          canCreateAppeal={canCreateAppeal}
        />
      )}
    </PageLayout>
  )
}

function RatioWatchContent({
  status,
  userId,
  csrfToken,
  canCreateAppeal,
}: {
  status: MyRatioWatch
  userId: string
  csrfToken: string
  canCreateAppeal: boolean
}) {
  const [appealOpen, setAppealOpen] = useState(false)
  const ratio = formatShareRatio(
    status.credited_uploaded_bytes,
    status.charged_downloaded_bytes
  )
  return (
    <>
      <RatioStatusAlert status={status} />

      <section aria-labelledby="ratio-summary-title">
        <h2 id="ratio-summary-title" className="sr-only">
          分享率汇总
        </h2>
        <div className="grid gap-4 md:grid-cols-3">
          <MetricCard
            title="有效上传"
            value={formatBytes(status.credited_uploaded_bytes)}
            description="优惠结算后的计入上传"
            icon={<ArrowUpFromLineIcon />}
            tone="positive"
          />
          <MetricCard
            title="有效下载"
            value={formatBytes(status.charged_downloaded_bytes)}
            description="优惠结算后的计入下载"
            icon={<ArrowDownToLineIcon />}
            tone="primary"
          />
          <MetricCard
            title="当前分享率"
            value={ratio}
            description={`统计于 ${formatDateTime(status.observed_at)}`}
            icon={<GaugeIcon />}
            tone="warning"
          />
        </div>
      </section>

      {status.policy?.enabled ? (
        <PolicyCard status={status} />
      ) : (
        <Card>
          <CardContent>
            <Empty>
              <EmptyHeader>
                <EmptyMedia variant="icon">
                  <ShieldCheckIcon />
                </EmptyMedia>
                <EmptyTitle>当前未启用长期分享率考核</EmptyTitle>
                <EmptyDescription>
                  有效上传、有效下载和 H&amp;R 仍会按各自规则正常记录。
                </EmptyDescription>
              </EmptyHeader>
            </Empty>
          </CardContent>
        </Card>
      )}

      {status.assessment && (
        <AssessmentCard
          status={status}
          canCreateAppeal={canCreateAppeal}
          onAppeal={() => setAppealOpen(true)}
        />
      )}
      {status.appeal && <AppealCard appeal={status.appeal} />}
      <RatioWatchAppealDialog
        open={appealOpen}
        userId={userId}
        csrfToken={csrfToken}
        onOpenChange={setAppealOpen}
      />
    </>
  )
}

function RatioStatusAlert({ status }: { status: MyRatioWatch }) {
  const assessment = status.assessment
  if (status.download_restricted) {
    const source = status.restriction_source
    const title =
      source === "ratio_watch"
        ? "分享率考核已限制下载"
        : source === "both"
          ? "分享率考核与账户状态均限制下载"
          : "账户当前下载受限"
    const description = status.minimum_ratio_reached
      ? "当前有效分享率已经达到恢复目标，系统完成下一次考核刷新后会解除分享率来源的限制；其他账户限制仍需单独处理。"
      : source === "account"
        ? "这项限制不是由长期分享率考核单独造成，请查看站内消息或联系站点管理人员。"
        : `继续做种仍可增加有效上传；按当前有效下载计算，还需 ${formatBytes(status.recovery_uploaded_bytes)} 有效上传达到恢复目标。`
    return (
      <Alert variant="destructive">
        <ShieldXIcon />
        <AlertTitle>{title}</AlertTitle>
        <AlertDescription>{description}</AlertDescription>
      </Alert>
    )
  }
  if (assessment?.status === "warning") {
    return (
      <Alert>
        <ClockAlertIcon />
        <AlertTitle>观察期已结束，分享率仍需恢复</AlertTitle>
        <AlertDescription>
          当前尚未限制下载；若分享率跌破限制线将进入下载受限。还需
          {` ${formatBytes(status.recovery_uploaded_bytes)} `}
          有效上传达到最低目标。
        </AlertDescription>
      </Alert>
    )
  }
  if (assessment?.status === "watching") {
    return (
      <Alert>
        <ClockAlertIcon />
        <AlertTitle>正在进行分享率观察</AlertTitle>
        <AlertDescription>
          {formatDeadlineRemaining(assessment.deadline_at, status.observed_at)}
          ； 期间达到最低分享率即可自动完成考核。
        </AlertDescription>
      </Alert>
    )
  }
  if (status.vip_active && status.policy?.vip_exempt) {
    return (
      <Alert>
        <ShieldCheckIcon />
        <AlertTitle>VIP 状态有效，当前免于长期分享率考核</AlertTitle>
        <AlertDescription>
          H&amp;R、站点规则和其他账户限制仍独立生效。
        </AlertDescription>
      </Alert>
    )
  }
  if (!status.policy?.enabled) {
    return (
      <Alert>
        <InfoIcon />
        <AlertTitle>当前没有启用中的长期分享率规则</AlertTitle>
        <AlertDescription>这里仍会保留你的最终有效流量汇总。</AlertDescription>
      </Alert>
    )
  }
  if (!status.threshold_reached) {
    return (
      <Alert>
        <CircleCheckBigIcon />
        <AlertTitle>尚未达到考核下载量</AlertTitle>
        <AlertDescription>
          当前没有长期分享率考核；达到下载量门槛后才会按最低分享率判断。
        </AlertDescription>
      </Alert>
    )
  }
  if (status.minimum_ratio_reached) {
    return (
      <Alert>
        <CircleCheckBigIcon />
        <AlertTitle>当前分享率达标</AlertTitle>
        <AlertDescription>当前没有需要处理的长期分享率考核。</AlertDescription>
      </Alert>
    )
  }
  return (
    <Alert>
      <RefreshCwIcon />
      <AlertTitle>等待系统更新考核状态</AlertTitle>
      <AlertDescription>
        当前流量已经达到考核条件，后台任务会按周期刷新状态。
      </AlertDescription>
    </Alert>
  )
}

function PolicyCard({ status }: { status: MyRatioWatch }) {
  const policy = status.policy
  if (!policy) return null
  const progress = Math.min(
    100,
    policy.minimum_ratio_basis_points > 0
      ? (status.current_ratio_basis_points /
          policy.minimum_ratio_basis_points) *
          100
      : 100
  )
  return (
    <Card>
      <CardHeader>
        <CardTitle>当前规则</CardTitle>
        <CardDescription>
          {policy.bound_to_assessment
            ? "本次考核继续使用进入观察时绑定的规则。"
            : "这是当前全站生效的长期分享率规则。"}
        </CardDescription>
        <CardAction>
          <Badge variant="outline">第 {policy.rule_version} 版规则</Badge>
        </CardAction>
      </CardHeader>
      <CardContent className="flex flex-col gap-6">
        <Progress value={progress}>
          <ProgressLabel>
            恢复目标 {formatRatioBasisPoints(policy.minimum_ratio_basis_points)}
          </ProgressLabel>
          <ProgressValue>
            {() =>
              `当前 ${formatRatioBasisPoints(status.current_ratio_basis_points)}`
            }
          </ProgressValue>
        </Progress>
        <dl className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
          <RuleValue
            label="下载量门槛"
            value={formatBytes(policy.download_threshold_bytes)}
          />
          <RuleValue
            label="最低目标"
            value={formatRatioBasisPoints(policy.minimum_ratio_basis_points)}
          />
          <RuleValue
            label="下载限制线"
            value={formatRatioBasisPoints(
              policy.restriction_ratio_basis_points
            )}
          />
          <RuleValue
            label="观察期"
            value={formatWatchPeriod(policy.watch_period_seconds)}
          />
        </dl>
        <div className="flex flex-wrap items-center gap-2">
          <Badge variant={status.threshold_reached ? "secondary" : "outline"}>
            {status.threshold_reached ? "已达到下载量门槛" : "未达到下载量门槛"}
          </Badge>
          <Badge
            variant={status.minimum_ratio_reached ? "secondary" : "outline"}
          >
            {status.minimum_ratio_reached ? "分享率已达标" : "分享率未达标"}
          </Badge>
          {policy.vip_exempt && <Badge variant="outline">VIP 豁免</Badge>}
        </div>
      </CardContent>
    </Card>
  )
}

function AssessmentCard({
  status,
  canCreateAppeal,
  onAppeal,
}: {
  status: MyRatioWatch
  canCreateAppeal: boolean
  onAppeal: () => void
}) {
  const assessment = status.assessment
  if (!assessment) return null
  const statusLabel =
    assessment.status === "watching"
      ? "观察中"
      : assessment.status === "warning"
        ? "警告期"
        : "下载受限"
  return (
    <Card>
      <CardHeader>
        <CardTitle>本次考核</CardTitle>
        <CardDescription>
          达到最低分享率后系统会自动完成；仅在流量记录异常或特殊情况时提交申诉。
        </CardDescription>
        <CardAction>
          <Badge
            variant={
              assessment.status === "download_restricted"
                ? "destructive"
                : "secondary"
            }
          >
            {statusLabel}
          </Badge>
        </CardAction>
      </CardHeader>
      <CardContent className="flex flex-col gap-5">
        <dl className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
          <RuleValue
            label="开始时间"
            value={formatDateTime(assessment.started_at)}
          />
          <RuleValue
            label="观察截止"
            value={formatDateTime(assessment.deadline_at)}
          />
          <RuleValue
            label="剩余时间"
            value={formatDeadlineRemaining(
              assessment.deadline_at,
              status.observed_at
            )}
          />
          <RuleValue
            label="还需有效上传"
            value={formatBytes(status.recovery_uploaded_bytes)}
          />
        </dl>
        <Alert>
          <InfoIcon />
          <AlertTitle>恢复方式</AlertTitle>
          <AlertDescription>
            保持做种并增加有效上传即可恢复。考核只使用最终结算流量，优惠切换不会回算旧流量。
          </AlertDescription>
        </Alert>
        {status.can_appeal && canCreateAppeal ? (
          <div>
            <Button type="button" variant="outline" onClick={onAppeal}>
              <MessageSquareWarningIcon data-icon="inline-start" />
              提交申诉
            </Button>
          </div>
        ) : null}
      </CardContent>
    </Card>
  )
}

function AppealCard({
  appeal,
}: {
  appeal: NonNullable<MyRatioWatch["appeal"]>
}) {
  const title =
    appeal.status === "pending"
      ? "申诉已提交，等待处理"
      : appeal.status === "approved"
        ? "申诉已批准"
        : appeal.status === "rejected"
          ? "申诉未获批准"
          : "申诉随考核结束"
  const description =
    appeal.status === "pending"
      ? "管理员处理前仍可继续做种恢复分享率；本期不能重复提交。"
      : appeal.status === "approved"
        ? "管理员已人工解除本期分享率考核；独立账户下载限制不会随之解除。"
        : appeal.status === "rejected"
          ? "本期考核继续生效，请按处理意见和分享率目标继续恢复。"
          : "系统在管理员处理前已自动结束本期考核，因此申诉不再需要人工决定。"
  return (
    <Card>
      <CardHeader>
        <CardTitle>{title}</CardTitle>
        <CardDescription>{description}</CardDescription>
        <CardAction>
          <Badge
            variant={appeal.status === "rejected" ? "destructive" : "outline"}
          >
            {appealStatusLabel(appeal.status)}
          </Badge>
        </CardAction>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <div>
          <div className="text-xs text-muted-foreground">你的说明</div>
          <p className="mt-1 text-sm leading-6 whitespace-pre-wrap">
            {appeal.statement}
          </p>
        </div>
        {appeal.response ? (
          <div className="rounded-md border bg-muted/30 p-4">
            <div className="text-xs text-muted-foreground">处理意见</div>
            <p className="mt-1 text-sm leading-6 whitespace-pre-wrap">
              {appeal.response}
            </p>
          </div>
        ) : null}
        <div className="text-xs text-muted-foreground">
          提交于 {formatDateTime(appeal.submitted_at)}
          {appeal.resolved_at
            ? ` · 处理于 ${formatDateTime(appeal.resolved_at)}`
            : ""}
        </div>
      </CardContent>
    </Card>
  )
}

function appealStatusLabel(
  status: NonNullable<MyRatioWatch["appeal"]>["status"]
) {
  if (status === "pending") return "待处理"
  if (status === "approved") return "已批准"
  if (status === "rejected") return "已驳回"
  return "考核已结束"
}

function RuleValue({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex min-w-0 flex-col gap-1">
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className="truncate font-medium tabular-nums" title={value}>
        {value}
      </dd>
    </div>
  )
}

function ErrorAlert({
  title,
  description,
  retry,
}: {
  title: string
  description: string
  retry: () => void
}) {
  return (
    <Alert variant="destructive">
      <CircleAlertIcon />
      <AlertTitle>{title}</AlertTitle>
      <AlertDescription>{description}</AlertDescription>
      <AlertAction>
        <Button type="button" variant="outline" size="sm" onClick={retry}>
          <RefreshCwIcon data-icon="inline-start" />
          重试
        </Button>
      </AlertAction>
    </Alert>
  )
}

function AccessCard({
  icon,
  title,
  description,
  action,
}: {
  icon: ReactNode
  title: string
  description: string
  action: ReactNode
}) {
  return (
    <Card>
      <CardContent>
        <Empty>
          <EmptyHeader>
            <EmptyMedia variant="icon">{icon}</EmptyMedia>
            <EmptyTitle>{title}</EmptyTitle>
            <EmptyDescription>{description}</EmptyDescription>
          </EmptyHeader>
          <EmptyContent>{action}</EmptyContent>
        </Empty>
      </CardContent>
    </Card>
  )
}

function RatioWatchSkeleton() {
  return (
    <div
      className="flex flex-col gap-5"
      aria-label="正在加载分享率考核"
      aria-busy="true"
    >
      <Skeleton className="h-14 w-full" />
      <div className="grid gap-4 md:grid-cols-3">
        {Array.from({ length: 3 }, (_, index) => (
          <Card key={index} size="sm">
            <CardHeader>
              <Skeleton className="h-4 w-20" />
            </CardHeader>
            <CardContent className="flex flex-col gap-2">
              <Skeleton className="h-8 w-28" />
              <Skeleton className="h-3 w-36" />
            </CardContent>
          </Card>
        ))}
      </div>
      <Card>
        <CardHeader>
          <Skeleton className="h-5 w-24" />
          <Skeleton className="h-4 w-64 max-w-full" />
        </CardHeader>
        <CardContent className="flex flex-col gap-4">
          <Skeleton className="h-8 w-full" />
          <Skeleton className="h-20 w-full" />
        </CardContent>
      </Card>
    </div>
  )
}

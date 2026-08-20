import * as React from "react"
import { Link } from "react-router"
import {
  CircleAlertIcon,
  CircleCheckBigIcon,
  ClockAlertIcon,
  DownloadIcon,
  GaugeIcon,
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
import { Skeleton } from "~/components/ui/skeleton"
import { useWebSession } from "~/features/auth/api/session.mutations"
import { useCapabilities } from "~/features/authz/api/capabilities.queries"
import {
  type DownloadRestrictionStatus,
  useDownloadRestriction,
} from "~/features/identity/api/download-restriction.queries"
import { DownloadRestrictionAppealDialog } from "~/features/identity/components/download-restriction-appeal-dialog"
import { requestErrorDescription } from "~/shared/api/problem"
import { PageHeader, PageLayout } from "~/shared/components/page-layout"
import { formatDateTime } from "~/shared/formatters/date-time"

export function DownloadRestrictionPage() {
  const session = useWebSession()
  const capabilities = useCapabilities(session.data?.user.id)
  const canRead = Boolean(
    capabilities.data?.items.some(
      (capability) => capability.action === "user.downloadrestriction.read.self"
    )
  )
  const canAppeal = Boolean(
    capabilities.data?.items.some(
      (capability) =>
        capability.action === "user.downloadrestriction.appeal.create.self"
    )
  )
  const restriction = useDownloadRestriction(
    session.data?.user.id,
    Boolean(capabilities.data && canRead)
  )

  return (
    <PageLayout>
      <PageHeader
        title="下载限制"
        description="查看当前限制来自旧站或人工设置、长期分享率，还是 H&R。"
      />

      {(session.isPending || (session.data && capabilities.isPending)) && (
        <DownloadRestrictionSkeleton />
      )}

      {session.isError && (
        <RestrictionError
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
          title="登录后查看下载限制"
          description="限制来源和申诉内容只对本人可见。"
          action={
            <Link to="/login" className={buttonVariants()}>
              <LogInIcon data-icon="inline-start" />
              前往登录
            </Link>
          }
        />
      )}

      {session.data && capabilities.isError && (
        <RestrictionError
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
          title="当前账户不能查看下载限制"
          description="如有疑问，请联系站点管理人员。"
          action={
            <Link to="/" className={buttonVariants({ variant: "outline" })}>
              返回首页
            </Link>
          }
        />
      )}

      {session.data && canRead && restriction.isPending && (
        <DownloadRestrictionSkeleton />
      )}

      {session.data && canRead && restriction.isError && (
        <RestrictionError
          title="下载限制暂时无法读取"
          description={requestErrorDescription(
            restriction.error,
            "下载限制请求未能完成，请稍后再试。"
          )}
          retry={() => void restriction.refetch()}
        />
      )}

      {session.data && canRead && restriction.data && (
        <DownloadRestrictionContent
          status={restriction.data}
          userId={session.data.user.id}
          csrfToken={session.data.csrf_token}
          canCreateAppeal={canAppeal}
        />
      )}
    </PageLayout>
  )
}

function DownloadRestrictionContent({
  status,
  userId,
  csrfToken,
  canCreateAppeal,
}: {
  status: DownloadRestrictionStatus
  userId: string
  csrfToken: string
  canCreateAppeal: boolean
}) {
  const [appealOpen, setAppealOpen] = React.useState(false)
  return (
    <>
      <Alert variant={status.restricted ? "destructive" : "default"}>
        {status.restricted ? <ShieldXIcon /> : <CircleCheckBigIcon />}
        <AlertTitle>
          {status.restricted ? "当前新下载受限" : "当前可以正常下载"}
        </AlertTitle>
        <AlertDescription>
          {status.restricted
            ? "继续做种不受影响。请根据下方生效来源分别恢复或提交申诉。"
            : "目前没有旧站/人工、长期分享率或 H&R 来源限制新下载。"}
        </AlertDescription>
      </Alert>

      <section aria-labelledby="download-source-title">
        <h2 id="download-source-title" className="sr-only">
          下载限制来源
        </h2>
        <div className="grid gap-4 md:grid-cols-3">
          <SourceCard
            title="旧站 / 人工"
            description="从旧站迁入或由用户管理员设置"
            active={status.sources.manual_or_legacy}
            icon={<DownloadIcon />}
          />
          <SourceCard
            title="长期分享率"
            description="按全站有效上传与下载考核"
            active={status.sources.ratio_watch}
            icon={<GaugeIcon />}
            link="/account/ratio"
          />
          <SourceCard
            title="H&R"
            description="按单个种子的保种义务考核"
            active={status.sources.hit_and_run}
            icon={<ClockAlertIcon />}
            link="/account/hnr"
          />
        </div>
      </section>

      <ManualRestrictionCard
        status={status}
        canCreateAppeal={canCreateAppeal}
        onAppeal={() => setAppealOpen(true)}
      />

      <DownloadRestrictionAppealDialog
        open={appealOpen}
        userId={userId}
        csrfToken={csrfToken}
        onOpenChange={setAppealOpen}
      />
    </>
  )
}

function SourceCard({
  title,
  description,
  active,
  icon,
  link,
}: {
  title: string
  description: string
  active: boolean
  icon: React.ReactNode
  link?: string
}) {
  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2 text-base">
          {icon}
          {title}
        </CardTitle>
        <CardDescription>{description}</CardDescription>
        <CardAction>
          <Badge variant={active ? "destructive" : "outline"}>
            {active ? "正在限制" : "未限制"}
          </Badge>
        </CardAction>
      </CardHeader>
      {link ? (
        <CardContent>
          <Link to={link} className={buttonVariants({ variant: "outline" })}>
            查看详情
          </Link>
        </CardContent>
      ) : null}
    </Card>
  )
}

function ManualRestrictionCard({
  status,
  canCreateAppeal,
  onAppeal,
}: {
  status: DownloadRestrictionStatus
  canCreateAppeal: boolean
  onAppeal: () => void
}) {
  const restriction = status.restriction
  const appeal = status.appeal
  if (!restriction && !appeal) {
    return (
      <Card>
        <CardContent>
          <Empty>
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <ShieldCheckIcon />
              </EmptyMedia>
              <EmptyTitle>没有旧站或人工下载限制</EmptyTitle>
              <EmptyDescription>
                分享率与 H&amp;R 状态仍按各自规则独立判断。
              </EmptyDescription>
            </EmptyHeader>
          </Empty>
        </CardContent>
      </Card>
    )
  }
  return (
    <Card>
      <CardHeader>
        <CardTitle>旧站 / 人工限制</CardTitle>
        <CardDescription>
          这里只解除该来源，不会清除长期分享率或 H&amp;R 限制。
        </CardDescription>
        <CardAction>
          <Badge
            variant={
              status.sources.manual_or_legacy ? "destructive" : "outline"
            }
          >
            {status.sources.manual_or_legacy ? "生效中" : "已解除"}
          </Badge>
        </CardAction>
      </CardHeader>
      <CardContent className="flex flex-col gap-5">
        {restriction ? (
          <dl className="grid gap-4 sm:grid-cols-2">
            <div>
              <dt className="text-xs text-muted-foreground">限制说明</dt>
              <dd className="mt-1 text-sm leading-6">
                {restriction.reason_summary}
              </dd>
            </div>
            <div>
              <dt className="text-xs text-muted-foreground">记录时间</dt>
              <dd className="mt-1 text-sm">
                {formatDateTime(restriction.starts_at)}
              </dd>
            </div>
          </dl>
        ) : null}

        {appeal ? <AppealRecord appeal={appeal} /> : null}

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

function AppealRecord({
  appeal,
}: {
  appeal: NonNullable<DownloadRestrictionStatus["appeal"]>
}) {
  return (
    <div className="flex flex-col gap-3 rounded-md border bg-muted/20 p-4">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <span className="font-medium">申诉记录</span>
        <Badge
          variant={appeal.status === "rejected" ? "destructive" : "outline"}
        >
          {appealStatusLabel(appeal.status)}
        </Badge>
      </div>
      <div>
        <div className="text-xs text-muted-foreground">你的说明</div>
        <p className="mt-1 text-sm leading-6 whitespace-pre-wrap">
          {appeal.statement}
        </p>
      </div>
      {appeal.response ? (
        <div>
          <div className="text-xs text-muted-foreground">处理意见</div>
          <p className="mt-1 text-sm leading-6 whitespace-pre-wrap">
            {appeal.response}
          </p>
        </div>
      ) : null}
      <div className="text-xs text-muted-foreground">
        提交于 {formatDateTime(appeal.created_at)}
        {appeal.resolved_at
          ? ` · 处理于 ${formatDateTime(appeal.resolved_at)}`
          : ""}
      </div>
    </div>
  )
}

function appealStatusLabel(status: string) {
  if (status === "pending") return "待处理"
  if (status === "approved") return "已批准"
  if (status === "rejected") return "已驳回"
  return "来源已解除"
}

function RestrictionError({
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
        <Button variant="outline" size="sm" onClick={retry}>
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
  icon: React.ReactNode
  title: string
  description: string
  action: React.ReactNode
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

function DownloadRestrictionSkeleton() {
  return (
    <div className="flex flex-col gap-4" aria-label="正在加载下载限制">
      <Skeleton className="h-20 w-full" />
      <div className="grid gap-4 md:grid-cols-3">
        <Skeleton className="h-36 w-full" />
        <Skeleton className="h-36 w-full" />
        <Skeleton className="h-36 w-full" />
      </div>
      <Skeleton className="h-56 w-full" />
    </div>
  )
}

import * as React from "react"
import { Link } from "react-router"
import {
  CircleAlertIcon,
  CircleCheckBigIcon,
  ClockAlertIcon,
  LogInIcon,
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
import { ToggleGroup, ToggleGroupItem } from "~/components/ui/toggle-group"
import { useWebSession } from "~/features/auth/api/session.mutations"
import { useCapabilities } from "~/features/authz/api/capabilities.queries"
import {
  type HitAndRunFilter,
  type HitAndRunPageData,
  useHitAndRunPage,
} from "~/features/traffic/api/hnr.queries"
import { HitAndRunList } from "~/features/traffic/components/hit-and-run-list"
import { HNRAppealDialog } from "~/features/traffic/components/hnr-appeal-dialog"
import { formatHNRCount } from "~/features/traffic/model/hnr-format"
import { PageHeader, PageLayout } from "~/shared/components/page-layout"
import { requestErrorDescription } from "~/shared/api/problem"
import { formatDateTime } from "~/shared/formatters/date-time"
import { cn } from "~/lib/utils"

const filters: Array<{ value: HitAndRunFilter; label: string }> = [
  { value: "open", label: "待关注" },
  { value: "tracking", label: "考察中" },
  { value: "grace", label: "宽限期" },
  { value: "overdue", label: "待补做" },
  { value: "satisfied", label: "已达标" },
  { value: "exempt", label: "已豁免" },
  { value: "all", label: "全部" },
]

export function HitAndRunPage() {
  const [filter, setFilter] = React.useState<HitAndRunFilter>("open")
  const [cursors, setCursors] = React.useState<Array<string | undefined>>([
    undefined,
  ])
  const session = useWebSession()
  const capabilities = useCapabilities(session.data?.user.id)
  const [appealEntry, setAppealEntry] = React.useState<
    HitAndRunPageData["items"][number] | null
  >(null)
  const canRead = Boolean(
    capabilities.data?.items.some(
      (capability) => capability.action === "hnr.read.self"
    )
  )
  const canCreateAppeal = Boolean(
    capabilities.data?.items.some(
      (capability) => capability.action === "hnr.appeal.create.self"
    )
  )
  const cursor = cursors.at(-1)
  const records = useHitAndRunPage(
    session.data?.user.id,
    filter,
    cursor,
    canRead
  )
  const nextCursor = records.data?.next_cursor

  function changeFilter(next: HitAndRunFilter) {
    setFilter(next)
    setCursors([undefined])
  }

  return (
    <PageLayout>
      <PageHeader
        title="H&R"
        description="查看仍需做种的记录、考察进度和达标情况。"
      />

      {session.isPending && <HitAndRunPageSkeleton />}

      {session.isError && (
        <HNRAlert
          title="会话状态暂时无法读取"
          description={requestErrorDescription(
            session.error,
            "会话请求未能完成，请稍后刷新页面。"
          )}
          retry={() => void session.refetch()}
        />
      )}

      {!session.isPending && !session.isError && !session.data && (
        <HNRAccessCard
          icon={<LogInIcon />}
          title="登录后查看 H&R"
          description="记录只对本人可见，登录后可查看需要继续做种的项目。"
          action={
            <Link to="/login" className={buttonVariants()}>
              <LogInIcon data-icon="inline-start" />
              前往登录
            </Link>
          }
        />
      )}

      {session.data && capabilities.isPending && <HitAndRunPageSkeleton />}

      {session.data && capabilities.isError && (
        <HNRAlert
          title="暂时无法读取查看权限"
          description={requestErrorDescription(
            capabilities.error,
            "权限请求未能完成，请稍后再试。"
          )}
          retry={() => void capabilities.refetch()}
        />
      )}

      {session.data && capabilities.data && !canRead && (
        <HNRAccessCard
          icon={<ShieldXIcon />}
          title="当前账户不能查看 H&R"
          description="如有疑问，请联系站点管理人员。"
          action={
            <Link to="/" className={buttonVariants({ variant: "outline" })}>
              返回首页
            </Link>
          }
        />
      )}

      {session.data && canRead && records.isPending && (
        <HitAndRunPageSkeleton />
      )}

      {session.data && canRead && records.isError && (
        <HNRAlert
          title="H&R 记录暂时无法读取"
          description={requestErrorDescription(
            records.error,
            "H&R 请求未能完成，请稍后再试。"
          )}
          retry={() => void records.refetch()}
        />
      )}

      {session.data && canRead && records.data && (
        <HitAndRunContent
          page={records.data}
          filter={filter}
          pageNumber={cursors.length}
          onFilterChange={changeFilter}
          canCreateAppeal={canCreateAppeal}
          onAppeal={setAppealEntry}
          onPrevious={
            cursors.length > 1
              ? () => setCursors((current) => current.slice(0, -1))
              : undefined
          }
          onNext={
            nextCursor
              ? () => setCursors((current) => [...current, nextCursor])
              : undefined
          }
        />
      )}
      {session.data ? (
        <HNRAppealDialog
          entry={appealEntry}
          userId={session.data.user.id}
          csrfToken={session.data.csrf_token}
          onOpenChange={(open) => {
            if (!open) setAppealEntry(null)
          }}
        />
      ) : null}
    </PageLayout>
  )
}

function HitAndRunContent({
  page,
  filter,
  pageNumber,
  onFilterChange,
  canCreateAppeal,
  onAppeal,
  onPrevious,
  onNext,
}: {
  page: HitAndRunPageData
  filter: HitAndRunFilter
  pageNumber: number
  onFilterChange: (filter: HitAndRunFilter) => void
  canCreateAppeal: boolean
  onAppeal: (entry: HitAndRunPageData["items"][number]) => void
  onPrevious: (() => void) | undefined
  onNext: (() => void) | undefined
}) {
  return (
    <>
      <section aria-labelledby="hnr-summary-title">
        <Card className="gap-0 py-0">
          <CardHeader className="border-b py-3.5">
            <CardTitle id="hnr-summary-title">考察概览</CardTitle>
            <CardDescription>只统计当前账户的做种考察状态。</CardDescription>
            <CardAction className="flex flex-col items-end gap-1">
              <Badge variant="outline">
                共 {formatHNRCount(page.summary.total)} 条
              </Badge>
              <span className="hidden text-xs text-muted-foreground sm:inline">
                更新于 {formatDateTime(page.as_of)}
              </span>
            </CardAction>
          </CardHeader>
          <CardContent className="grid grid-cols-2 px-0 lg:grid-cols-4">
            <HNRMetric
              title="待关注"
              value={formatHNRCount(
                page.summary.tracking,
                page.summary.grace,
                page.summary.overdue
              )}
              description="考察中、宽限期与待补做"
              icon={<ClockAlertIcon />}
              tone="warning"
            />
            <HNRMetric
              title="待补做"
              value={formatHNRCount(page.summary.overdue)}
              description="继续做种仍可修复"
              icon={<CircleAlertIcon />}
              tone="primary"
            />
            <HNRMetric
              title="已达标"
              value={formatHNRCount(page.summary.satisfied)}
              description="已满足时长或实际分享率"
              icon={<CircleCheckBigIcon />}
              tone="positive"
            />
            <HNRMetric
              title="已豁免"
              value={formatHNRCount(page.summary.exempt)}
              description="规则豁免或申诉批准"
              icon={<ShieldCheckIcon />}
              tone="muted"
            />
          </CardContent>
        </Card>
      </section>

      {page.summary.overdue !== "0" ? (
        <Alert variant="destructive">
          <CircleAlertIcon />
          <AlertTitle>当前有 H&amp;R 待补做记录，新下载已受限</AlertTitle>
          <AlertDescription>
            继续做种不受影响。任一记录达到所需时长或实际分享率后会自动更新；全部待补做记录达标后，H&amp;R
            下载限制自动解除。
          </AlertDescription>
        </Alert>
      ) : null}

      <section aria-labelledby="hnr-list-title" className="flex flex-col gap-3">
        <h2 id="hnr-list-title" className="sr-only">
          H&amp;R 记录
        </h2>
        <div className="-mx-4 overflow-x-auto px-4 pb-1 sm:mx-0 sm:px-0">
          <ToggleGroup
            value={[filter]}
            onValueChange={(values) => {
              const selected = values[0] as HitAndRunFilter | undefined
              if (selected) onFilterChange(selected)
            }}
            size="default"
            spacing={1}
            aria-label="按 H&R 状态筛选"
            className="min-w-max bg-muted/60 p-1 pr-4 sm:pr-1"
          >
            {filters.map((item) => (
              <ToggleGroupItem
                key={item.value}
                value={item.value}
                className="min-w-20 data-[state=on]:bg-background data-[state=on]:shadow-sm"
              >
                {item.label}
              </ToggleGroupItem>
            ))}
          </ToggleGroup>
        </div>
        <HitAndRunList
          page={page}
          filter={filter}
          pageNumber={pageNumber}
          canCreateAppeal={canCreateAppeal}
          onAppeal={onAppeal}
          onPrevious={onPrevious}
          onNext={onNext}
        />
      </section>
    </>
  )
}

function HNRMetric({
  title,
  value,
  description,
  icon,
  tone,
}: {
  title: string
  value: string
  description: string
  icon: React.ReactNode
  tone: "warning" | "primary" | "positive" | "muted"
}) {
  return (
    <div
      className={cn(
        "flex min-h-32 flex-col gap-1 border-r border-b p-5 even:border-r-0 nth-last-[-n+2]:border-b-0 lg:border-b-0 lg:last:border-r-0 lg:even:border-r",
        tone === "warning" && "bg-warning/5",
        tone === "primary" && "bg-primary/5",
        tone === "positive" && "bg-success/5",
        tone === "muted" && "bg-muted/30"
      )}
    >
      <div className="flex items-center justify-between gap-3">
        <span className="text-sm text-muted-foreground">{title}</span>
        <span
          className={cn(
            "text-muted-foreground [&>svg]:size-4",
            tone === "warning" && "text-warning-foreground",
            tone === "primary" && "text-primary",
            tone === "positive" && "text-success-foreground"
          )}
          aria-hidden="true"
        >
          {icon}
        </span>
      </div>
      <strong className="font-heading text-3xl tabular-nums">{value}</strong>
      <span className="text-xs text-muted-foreground">{description}</span>
    </div>
  )
}

function HNRAlert({
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

function HNRAccessCard({
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

function HitAndRunPageSkeleton() {
  return (
    <div
      className="flex flex-col gap-6"
      aria-label="正在加载 H&R"
      aria-busy="true"
    >
      <Skeleton className="h-14 w-full" />
      <div className="grid grid-cols-2 gap-0 overflow-hidden rounded-lg border md:grid-cols-4">
        {Array.from({ length: 4 }, (_, index) => (
          <Card key={index} size="sm">
            <CardHeader>
              <CardTitle>
                <Skeleton className="h-4 w-20" />
              </CardTitle>
              <CardDescription>
                <Skeleton className="h-3 w-32" />
              </CardDescription>
            </CardHeader>
            <CardContent>
              <Skeleton className="h-7 w-16" />
            </CardContent>
          </Card>
        ))}
      </div>
      <Card>
        <CardHeader>
          <CardTitle>
            <Skeleton className="h-5 w-28" />
          </CardTitle>
          <CardDescription>
            <Skeleton className="h-4 w-72 max-w-full" />
          </CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-3">
          <Skeleton className="h-16 w-full" />
          <Skeleton className="h-16 w-full" />
          <Skeleton className="h-16 w-full" />
        </CardContent>
      </Card>
    </div>
  )
}

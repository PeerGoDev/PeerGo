import { Link } from "react-router"
import {
  ArrowDownToLineIcon,
  ArrowUpFromLineIcon,
  CircleAlertIcon,
  GaugeIcon,
  LogInIcon,
  RefreshCwIcon,
} from "lucide-react"

import {
  Alert,
  AlertAction,
  AlertDescription,
  AlertTitle,
} from "~/components/ui/alert"
import { Button, buttonVariants } from "~/components/ui/button"
import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from "~/components/ui/card"
import { Skeleton } from "~/components/ui/skeleton"
import { useWebSession } from "~/features/auth/api/session.mutations"
import {
  type TrafficOverview,
  useTrafficOverview,
} from "~/features/traffic/api/traffic.queries"
import { TrafficLedger } from "~/features/traffic/components/traffic-ledger"
import { formatShareRatio } from "~/features/traffic/model/format"
import { MetricCard } from "~/shared/components/metric-card"
import { PageHeader, PageLayout } from "~/shared/components/page-layout"
import { formatBytes } from "~/shared/formatters/bytes"
import { formatDateTime } from "~/shared/formatters/date-time"
import { requestErrorDescription } from "~/shared/api/problem"

export function TrafficPage() {
  const session = useWebSession()
  const traffic = useTrafficOverview(session.data?.user.id)

  return (
    <PageLayout>
      <PageHeader
        title="流量统计"
        description="查看有效上传、有效下载、分享率和最近流量记录。"
      />

      {session.isPending && <TrafficPageSkeleton />}

      {session.isError && (
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
      )}

      {!session.isPending && !session.isError && !session.data && (
        <Card>
          <CardHeader>
            <CardTitle>需要登录</CardTitle>
            <CardDescription>
              登录后可查看自己的流量汇总和明细。
            </CardDescription>
          </CardHeader>
          <CardContent>
            <Alert>
              <GaugeIcon />
              <AlertTitle>没有可用会话</AlertTitle>
              <AlertDescription>
                登录后即可查看仅属于当前账户的记录。
              </AlertDescription>
            </Alert>
          </CardContent>
          <CardFooter>
            <Link to="/login" className={buttonVariants()}>
              <LogInIcon data-icon="inline-start" />
              前往登录
            </Link>
          </CardFooter>
        </Card>
      )}

      {session.data && traffic.isPending && <TrafficPageSkeleton />}

      {session.data && traffic.isError && (
        <Alert variant="destructive">
          <CircleAlertIcon />
          <AlertTitle>流量记录暂时无法读取</AlertTitle>
          <AlertDescription>
            {requestErrorDescription(
              traffic.error,
              "流量记录请求未能完成，请稍后再试。"
            )}
          </AlertDescription>
          <AlertAction>
            <Button
              variant="outline"
              size="sm"
              onClick={() => void traffic.refetch()}
            >
              <RefreshCwIcon data-icon="inline-start" />
              重试
            </Button>
          </AlertAction>
        </Alert>
      )}

      {session.data && traffic.data && (
        <TrafficOverviewContent overview={traffic.data} />
      )}
    </PageLayout>
  )
}

function TrafficOverviewContent({ overview }: { overview: TrafficOverview }) {
  const totals = overview.totals
  const projectionTime = totals.projection_updated_at
  return (
    <>
      <section
        aria-labelledby="traffic-summary-title"
        className="flex flex-col gap-3"
      >
        <div className="flex justify-end">
          <h2 id="traffic-summary-title" className="sr-only">
            流量汇总
          </h2>
          <span className="text-xs text-muted-foreground">
            {projectionTime
              ? `更新于 ${formatDateTime(projectionTime)}`
              : "暂无流量记录"}
          </span>
        </div>
        <div className="grid gap-4 md:grid-cols-3">
          <MetricCard
            title="有效上传"
            value={formatBytes(totals.credited_uploaded_bytes)}
            description={`实际上传 ${formatBytes(totals.raw_uploaded_bytes)}`}
            icon={<ArrowUpFromLineIcon />}
            tone="positive"
          />
          <MetricCard
            title="有效下载"
            value={formatBytes(totals.charged_downloaded_bytes)}
            description={`实际下载 ${formatBytes(totals.raw_downloaded_bytes)}`}
            icon={<ArrowDownToLineIcon />}
            tone="primary"
          />
          <MetricCard
            title="分享率"
            value={formatShareRatio(
              totals.credited_uploaded_bytes,
              totals.charged_downloaded_bytes
            )}
            description="有效上传 / 有效下载"
            icon={<GaugeIcon />}
            tone="warning"
          />
        </div>
      </section>

      <section aria-label="最近流量记录">
        <TrafficLedger
          entries={overview.entries}
          totalEntries={totals.entry_count}
        />
      </section>
    </>
  )
}

function TrafficPageSkeleton() {
  return (
    <div
      className="flex flex-col gap-6"
      aria-label="正在加载流量记录"
      aria-busy="true"
    >
      <Skeleton className="h-14 w-full" />
      <div className="grid gap-4 md:grid-cols-3">
        {Array.from({ length: 3 }, (_, index) => (
          <Card key={index} size="sm">
            <CardHeader>
              <CardTitle>
                <Skeleton className="h-4 w-20" />
              </CardTitle>
              <CardDescription>
                <Skeleton className="h-3 w-28" />
              </CardDescription>
            </CardHeader>
            <CardContent>
              <Skeleton className="h-7 w-24" />
            </CardContent>
          </Card>
        ))}
      </div>
      <Card>
        <CardHeader>
          <CardTitle>
            <Skeleton className="h-5 w-24" />
          </CardTitle>
          <CardDescription>
            <Skeleton className="h-4 w-64 max-w-full" />
          </CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-3">
          <Skeleton className="h-12 w-full" />
          <Skeleton className="h-12 w-full" />
          <Skeleton className="h-12 w-full" />
        </CardContent>
      </Card>
    </div>
  )
}

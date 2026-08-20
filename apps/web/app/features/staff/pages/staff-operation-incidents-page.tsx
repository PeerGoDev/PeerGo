import { useQuery } from "@tanstack/react-query"
import {
  CircleAlertIcon,
  ClipboardCheckIcon,
  RefreshCwIcon,
  ShieldAlertIcon,
  ShieldCheckIcon,
} from "lucide-react"

import { Alert, AlertDescription, AlertTitle } from "~/components/ui/alert"
import { Badge } from "~/components/ui/badge"
import { Button } from "~/components/ui/button"
import {
  Card,
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
import { workerOperationsQueryOptions } from "~/features/staff/api/operations.queries"
import { StaffAccessGate } from "~/features/staff/components/staff-access-gate"
import { StaffPageFrame } from "~/features/staff/components/staff-page-frame"
import {
  summarizeOperationIncidents,
  workerQueueHasIncident,
  workerQueueState,
  type WorkerQueue,
} from "~/features/staff/model/operation-incidents"
import { MetricCard } from "~/shared/components/metric-card"
import { formatCompactDateTime } from "~/shared/formatters/date-time"
import { formatInteger } from "~/shared/formatters/integer"

export function StaffOperationIncidentsPage() {
  return (
    <StaffAccessGate
      requiredAction="operations.monitor.read"
      pageHeader={{
        title: "任务异常与审计",
        description: "查看需要人工关注的后台任务和审计事件投递状态。",
      }}
    >
      {() => <OperationIncidentsContent />}
    </StaffAccessGate>
  )
}

function OperationIncidentsContent() {
  const status = useQuery(workerOperationsQueryOptions())
  if (status.isPending) return <OperationIncidentsSkeleton />
  if (status.isError || !status.data) {
    return (
      <StaffPageFrame className="gap-4">
        <Alert variant="destructive">
          <CircleAlertIcon />
          <AlertTitle>任务异常状态暂时无法读取</AlertTitle>
          <AlertDescription>
            请检查 Core 数据库与后台会话后重试。
          </AlertDescription>
        </Alert>
        <Button
          variant="outline"
          className="w-fit"
          onClick={() => void status.refetch()}
        >
          <RefreshCwIcon data-icon="inline-start" />
          重试
        </Button>
      </StaffPageFrame>
    )
  }

  const data = status.data
  const summary = summarizeOperationIncidents(data.queues)
  const incidents = data.queues.filter(workerQueueHasIncident)

  return (
    <StaffPageFrame className="gap-4">
      <header className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h1 className="font-heading text-xl font-semibold">任务异常与审计</h1>
          <p className="text-sm text-muted-foreground">
            汇总持续重试、死信和审计投递积压；正常排队任务仍在 Worker
            状态页查看。
          </p>
        </div>
        <Button
          variant="outline"
          size="sm"
          disabled={status.isFetching}
          onClick={() => void status.refetch()}
        >
          <RefreshCwIcon
            data-icon="inline-start"
            className={status.isFetching ? "animate-spin" : undefined}
          />
          刷新状态
        </Button>
      </header>

      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
        <MetricCard
          title="异常队列"
          value={formatInteger(summary.incidentQueueCount)}
          description="存在重试、死信或错误证据"
          icon={<ShieldAlertIcon />}
          tone={summary.incidentQueueCount > 0 ? "warning" : "positive"}
        />
        <MetricCard
          title="失败待重试"
          value={formatInteger(summary.retrying)}
          description="由 Worker 按退避策略处理"
          icon={<RefreshCwIcon />}
          tone={summary.retrying > 0n ? "warning" : "positive"}
        />
        <MetricCard
          title="需人工处理"
          value={formatInteger(summary.dead)}
          description="自动重试已经停止"
          icon={<CircleAlertIcon />}
          tone={summary.dead > 0n ? "warning" : "positive"}
        />
        <MetricCard
          title="审计待投递"
          value={formatInteger(summary.auditOutstanding)}
          description="待处理、处理中与重试合计"
          icon={<ClipboardCheckIcon />}
          tone={summary.auditOutstanding > 0n ? "warning" : "positive"}
        />
      </div>

      <Alert>
        <ShieldCheckIcon />
        <AlertTitle>本页只读，不提供跳过或清空任务</AlertTitle>
        <AlertDescription>
          错误码和时间用于定位故障；重新执行、冲正或解除人工处理状态必须由受审计的专用命令完成。
        </AlertDescription>
      </Alert>

      <Card className="gap-0 py-0">
        <CardHeader className="p-6 pb-4">
          <CardTitle>需要关注的任务</CardTitle>
          <CardDescription>
            这里只列出持续重试、死信或保留最近错误证据的队列。
          </CardDescription>
        </CardHeader>
        <CardContent className="p-0">
          {incidents.length === 0 ? (
            <Empty>
              <EmptyHeader>
                <EmptyMedia variant="icon">
                  <ShieldCheckIcon />
                </EmptyMedia>
                <EmptyTitle>当前没有任务异常</EmptyTitle>
                <EmptyDescription>
                  普通待处理和处理中任务不属于异常，可前往 Worker
                  状态页查看完整队列。
                </EmptyDescription>
              </EmptyHeader>
            </Empty>
          ) : (
            <IncidentTable queues={incidents} />
          )}
        </CardContent>
      </Card>

      <Card className="gap-0 py-0">
        <CardHeader className="p-6 pb-4">
          <CardTitle>审计事件投递</CardTitle>
          <CardDescription>
            这里只检查审计事件是否可靠送达，不展示敏感业务审计正文。
          </CardDescription>
        </CardHeader>
        <CardContent className="p-0">
          {summary.audit ? (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>状态</TableHead>
                  <TableHead className="text-right">待处理</TableHead>
                  <TableHead className="text-right">处理中</TableHead>
                  <TableHead className="text-right">重试</TableHead>
                  <TableHead className="text-right">已投递</TableHead>
                  <TableHead>最近错误</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                <TableRow>
                  <TableCell>
                    <QueueStateBadge queue={summary.audit} />
                  </TableCell>
                  <CountCell value={summary.audit.pending} />
                  <CountCell value={summary.audit.processing} />
                  <CountCell value={summary.audit.retrying} />
                  <CountCell value={summary.audit.completed} />
                  <ErrorCell queue={summary.audit} />
                </TableRow>
              </TableBody>
            </Table>
          ) : (
            <Alert variant="destructive" className="m-6">
              <CircleAlertIcon />
              <AlertTitle>缺少审计投递队列</AlertTitle>
              <AlertDescription>
                Core 未返回预期的审计队列投影，请先核对数据库迁移和运行版本。
              </AlertDescription>
            </Alert>
          )}
        </CardContent>
      </Card>
    </StaffPageFrame>
  )
}

function IncidentTable({ queues }: { queues: WorkerQueue[] }) {
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>任务</TableHead>
          <TableHead>状态</TableHead>
          <TableHead className="text-right">待重试</TableHead>
          <TableHead className="text-right">人工处理</TableHead>
          <TableHead>最老积压</TableHead>
          <TableHead>最近错误</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {queues.map((queue) => (
          <TableRow key={queue.id}>
            <TableCell className="font-medium">{queue.label}</TableCell>
            <TableCell>
              <QueueStateBadge queue={queue} />
            </TableCell>
            <CountCell value={queue.retrying} />
            <CountCell value={queue.dead} destructive />
            <TableCell className="text-muted-foreground">
              {queue.oldest_pending_at
                ? formatCompactDateTime(queue.oldest_pending_at)
                : "—"}
            </TableCell>
            <ErrorCell queue={queue} />
          </TableRow>
        ))}
      </TableBody>
    </Table>
  )
}

function QueueStateBadge({ queue }: { queue: WorkerQueue }) {
  const state = workerQueueState(queue)
  if (state === "dead") return <Badge variant="destructive">需要人工处理</Badge>
  if (state === "retrying") return <Badge variant="secondary">等待重试</Badge>
  if (state === "backlogged") return <Badge variant="outline">存在积压</Badge>
  if (state === "processing") return <Badge variant="outline">处理中</Badge>
  return <Badge variant="outline">正常</Badge>
}

function CountCell({
  value,
  destructive = false,
}: {
  value: string
  destructive?: boolean
}) {
  return (
    <TableCell className="text-right tabular-nums">
      {destructive && value !== "0" ? (
        <Badge variant="destructive">{formatInteger(value)}</Badge>
      ) : (
        formatInteger(value)
      )}
    </TableCell>
  )
}

function ErrorCell({ queue }: { queue: WorkerQueue }) {
  return (
    <TableCell className="max-w-72">
      {queue.last_error_code ? (
        <div className="flex flex-col gap-1">
          <code className="text-xs break-all">{queue.last_error_code}</code>
          {queue.last_error_at ? (
            <span className="text-xs text-muted-foreground">
              {formatCompactDateTime(queue.last_error_at)}
            </span>
          ) : null}
        </div>
      ) : (
        "—"
      )}
    </TableCell>
  )
}

function OperationIncidentsSkeleton() {
  return (
    <StaffPageFrame aria-label="正在加载任务异常与审计状态">
      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
        {Array.from({ length: 4 }, (_, index) => (
          <Skeleton key={index} className="h-36 rounded-lg" />
        ))}
      </div>
      <Skeleton className="h-20 rounded-lg" />
      <Skeleton className="h-80 rounded-lg" />
      <Skeleton className="h-56 rounded-lg" />
    </StaffPageFrame>
  )
}

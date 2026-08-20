import { useQuery } from "@tanstack/react-query"
import {
  CircleAlertIcon,
  ClockAlertIcon,
  RefreshCwIcon,
  ServerCogIcon,
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
import { MetricCard } from "~/shared/components/metric-card"
import { formatCompactDateTime } from "~/shared/formatters/date-time"
import { formatInteger } from "~/shared/formatters/integer"

export function StaffWorkersPage() {
  return (
    <StaffAccessGate
      requiredAction="operations.monitor.read"
      pageHeader={{
        title: "Worker 状态",
        description: "统一查看 Core 后台任务的待处理、重试、死信和最老积压。",
      }}
    >
      {() => <WorkerContent />}
    </StaffAccessGate>
  )
}

function WorkerContent() {
  const status = useQuery(workerOperationsQueryOptions())
  if (status.isPending) return <WorkerSkeleton />
  if (status.isError || !status.data) {
    return (
      <StaffPageFrame className="gap-4">
        <Alert variant="destructive">
          <CircleAlertIcon />
          <AlertTitle>Worker 状态暂时无法读取</AlertTitle>
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
  const totals = data.queues.reduce(
    (value, queue) => ({
      pending: value.pending + BigInt(queue.pending),
      processing: value.processing + BigInt(queue.processing),
      retrying: value.retrying + BigInt(queue.retrying),
      dead: value.dead + BigInt(queue.dead),
    }),
    { pending: 0n, processing: 0n, retrying: 0n, dead: 0n }
  )

  return (
    <StaffPageFrame className="gap-4">
      <header className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h1 className="font-heading text-xl font-semibold">Worker 状态</h1>
          <p className="mt-1 text-sm text-muted-foreground">
            每 15 秒自动刷新，只显示可安全公开给管理员的队列元数据。
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
          title="待处理"
          value={formatInteger(totals.pending)}
          description="尚未开始"
          icon={<ClockAlertIcon />}
          tone={totals.pending > 0n ? "warning" : "positive"}
        />
        <MetricCard
          title="正在执行"
          value={formatInteger(totals.processing)}
          description="Worker 正在处理"
          icon={<ServerCogIcon />}
          tone="primary"
        />
        <MetricCard
          title="失败待重试"
          value={formatInteger(totals.retrying)}
          description="曾发生可重试错误"
          icon={<RefreshCwIcon />}
          tone={totals.retrying > 0n ? "warning" : "positive"}
        />
        <MetricCard
          title="需人工处理"
          value={formatInteger(totals.dead)}
          description="自动重试已经停止"
          icon={<CircleAlertIcon />}
          tone={totals.dead > 0n ? "warning" : "positive"}
        />
      </div>

      {totals.dead > 0n ? (
        <Alert variant="destructive">
          <CircleAlertIcon />
          <AlertTitle>存在需要人工处理的奖励任务</AlertTitle>
          <AlertDescription>
            这些任务不会自动继续入账。先核对奖励规则、用户权益和做种记录，再通过受审计的专用命令处理。
          </AlertDescription>
        </Alert>
      ) : (
        <Alert>
          <ShieldCheckIcon />
          <AlertTitle>当前没有需要人工处理的任务</AlertTitle>
          <AlertDescription>
            自动重试与处理中任务仍可能短暂出现；以最老积压时间判断是否持续异常。
          </AlertDescription>
        </Alert>
      )}

      <Card className="gap-0 py-0">
        <CardHeader className="p-6 pb-4">
          <CardTitle>统一任务队列</CardTitle>
          <CardDescription>
            奖励结算、优惠投递、Tracker 控制和审计投递
          </CardDescription>
        </CardHeader>
        <CardContent className="p-0">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Worker</TableHead>
                <TableHead className="text-right">待处理</TableHead>
                <TableHead className="text-right">正在执行</TableHead>
                <TableHead className="text-right">待重试</TableHead>
                <TableHead className="text-right">人工处理</TableHead>
                <TableHead>最老积压</TableHead>
                <TableHead>最近错误</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {data.queues.map((queue) => (
                <TableRow key={queue.id}>
                  <TableCell>
                    <div className="font-medium">{queue.label}</div>
                    <div className="text-xs text-muted-foreground">
                      已完成 {formatInteger(queue.completed)}
                    </div>
                  </TableCell>
                  <TableCell className="text-right tabular-nums">
                    {formatInteger(queue.pending)}
                  </TableCell>
                  <TableCell className="text-right tabular-nums">
                    {formatInteger(queue.processing)}
                  </TableCell>
                  <TableCell className="text-right tabular-nums">
                    {formatInteger(queue.retrying)}
                  </TableCell>
                  <TableCell className="text-right">
                    {queue.dead === "0" ? (
                      <Badge variant="outline">0</Badge>
                    ) : (
                      <Badge variant="destructive">
                        {formatInteger(queue.dead)}
                      </Badge>
                    )}
                  </TableCell>
                  <TableCell className="text-muted-foreground">
                    {queue.oldest_pending_at
                      ? formatCompactDateTime(queue.oldest_pending_at)
                      : "—"}
                  </TableCell>
                  <TableCell className="max-w-52">
                    {queue.last_error_code ? (
                      <div>
                        <code className="text-xs break-all">
                          {queue.last_error_code}
                        </code>
                        {queue.last_error_at ? (
                          <div className="mt-1 text-xs text-muted-foreground">
                            {formatCompactDateTime(queue.last_error_at)}
                          </div>
                        ) : null}
                      </div>
                    ) : (
                      "—"
                    )}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </CardContent>
      </Card>
    </StaffPageFrame>
  )
}

function WorkerSkeleton() {
  return (
    <StaffPageFrame aria-label="正在加载 Worker 状态">
      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
        {Array.from({ length: 4 }, (_, index) => (
          <Skeleton key={index} className="h-36 rounded-lg" />
        ))}
      </div>
      <Skeleton className="h-20 rounded-lg" />
      <Skeleton className="h-96 rounded-lg" />
    </StaffPageFrame>
  )
}

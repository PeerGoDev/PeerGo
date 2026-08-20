import { useQuery } from "@tanstack/react-query"
import type { ReactNode } from "react"
import {
  ActivityIcon,
  CircleAlertIcon,
  Clock3Icon,
  DatabaseZapIcon,
  HardDriveDownloadIcon,
  RefreshCwIcon,
  RouterIcon,
} from "lucide-react"

import { Alert, AlertDescription, AlertTitle } from "~/components/ui/alert"
import { Button } from "~/components/ui/button"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "~/components/ui/card"
import { Skeleton } from "~/components/ui/skeleton"
import { trackerOperationsQueryOptions } from "~/features/staff/api/operations.queries"
import { StaffAccessGate } from "~/features/staff/components/staff-access-gate"
import { StaffPageFrame } from "~/features/staff/components/staff-page-frame"
import { StaffSettingsValueTable } from "~/features/staff/components/staff-settings-value-table"
import { MetricCard } from "~/shared/components/metric-card"
import { formatCompactDateTime } from "~/shared/formatters/date-time"
import { formatInteger } from "~/shared/formatters/integer"

export function StaffTrackerSettingsPage() {
  return (
    <StaffAccessGate
      requiredAction="operations.monitor.read"
      pageHeader={{
        title: "Tracker 状态",
        description: "查看可用种子、在线群集数据和结算证据是否正常。",
      }}
    >
      {() => <TrackerSettingsContent />}
    </StaffAccessGate>
  )
}

function TrackerSettingsContent() {
  const status = useQuery(trackerOperationsQueryOptions())

  if (status.isPending) return <TrackerSettingsSkeleton />
  if (status.isError || !status.data) {
    return (
      <StaffPageFrame className="gap-4">
        <Alert variant="destructive">
          <CircleAlertIcon />
          <AlertTitle>Tracker 状态暂时无法读取</AlertTitle>
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
  const trackerBacklog = BigInt(data.control.pending_events)
  const evidenceMissing = BigInt(data.evidence.missing_windows)

  return (
    <StaffPageFrame className="gap-4">
      <header className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h1 className="font-heading text-xl font-semibold">Tracker 状态</h1>
          <p className="text-sm text-muted-foreground">
            用于排查数据同步和结算链路；Announce 等日常参数在“Tracker
            参数”中查看。
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
          title="控制事件积压"
          value={formatInteger(data.control.pending_events)}
          description={`重试 ${formatInteger(data.control.retrying_events)}`}
          icon={<RouterIcon />}
          tone={trackerBacklog > 0n ? "warning" : "positive"}
        />
        <MetricCard
          title="Tracker 可用种子"
          value={formatInteger(data.control.enabled_torrents)}
          description={`停用 ${formatInteger(data.control.disabled_torrents)}`}
          icon={<HardDriveDownloadIcon />}
          tone="primary"
        />
        <MetricCard
          title="群集快照序号"
          value={formatInteger(data.swarm.snapshot_sequence)}
          description={data.swarm.source_id || "尚无完整快照"}
          icon={<ActivityIcon />}
          tone="default"
        />
        <MetricCard
          title="本月证据缺口"
          value={formatInteger(data.evidence.missing_windows)}
          description={`${evidenceHealthLabel(data.evidence.health)} · 应有 ${formatInteger(data.evidence.expected_windows)}`}
          icon={<DatabaseZapIcon />}
          tone={evidenceMissing > 0n ? "warning" : "positive"}
        />
      </div>

      {data.evidence.health !== "healthy" ? (
        <Alert
          variant={
            data.evidence.health === "broken" ||
            data.evidence.health === "unavailable"
              ? "destructive"
              : "default"
          }
        >
          <CircleAlertIcon />
          <AlertTitle>
            做种证据{evidenceHealthLabel(data.evidence.health)}
          </AlertTitle>
          <AlertDescription>
            {evidenceHealthDescription(data.evidence.health)}
            证据恢复前，保种组成员对应周期只会显示“证据不完整”，不会被判为未达标，也不应发送贡献提醒。
          </AlertDescription>
        </Alert>
      ) : null}

      <Alert>
        <Clock3Icon />
        <AlertTitle>监控页不会修改运行状态</AlertTitle>
        <AlertDescription>
          Tracker
          allowlist、群集快照和小时证据均按各自水位推进；此页面没有清空队列、跳过事件或远程重启入口。
        </AlertDescription>
      </Alert>

      <div className="grid gap-4 xl:grid-cols-2">
        <StatusCard
          title="控制面与群集"
          description="Tracker 可用种子投影及最近完整群集快照"
        >
          <StaffSettingsValueTable
            rows={[
              {
                label: "控制水位",
                value: formatInteger(data.control.last_sequence),
              },
              {
                label: "控制面更新时间",
                value: formatTime(data.control.updated_at),
              },
              {
                label: "最早未处理事件",
                value: formatTime(data.control.oldest_pending_at),
              },
              {
                label: "路由纪元",
                value: formatInteger(data.swarm.routing_epoch),
              },
              {
                label: "群集观测时间",
                value: formatTime(data.swarm.observed_at),
              },
              {
                label: "群集应用时间",
                value: formatTime(data.swarm.applied_at),
              },
              {
                label: "未完成快照",
                value:
                  data.swarm.collecting_runs === "0"
                    ? "无"
                    : `${formatInteger(data.swarm.collecting_runs)}（${data.swarm.latest_run_progress || "等待分片"}）`,
              },
            ]}
          />
        </StatusCard>

        <StatusCard
          title="结算证据与用户投影"
          description="小时证据、最终流量和 H&R 消费进度"
        >
          <StaffSettingsValueTable
            rows={[
              {
                label: "最近证据窗口",
                value: formatTime(data.evidence.latest_window_start),
              },
              {
                label: "窗口状态",
                value: evidenceStatusLabel(data.evidence.latest_status),
              },
              {
                label: "本月覆盖状态",
                value: evidenceHealthLabel(data.evidence.health),
              },
              {
                label: "应覆盖至",
                value: formatTime(data.evidence.expected_through),
              },
              {
                label: "缺失窗口",
                value: formatInteger(data.evidence.missing_windows),
              },
              {
                label: "最早未完成窗口",
                value: formatTime(data.evidence.oldest_incomplete),
              },
              {
                label: "证据条目",
                value: formatInteger(data.evidence.latest_item_count),
              },
              {
                label: "分片进度",
                value:
                  data.evidence.latest_chunks > 0
                    ? `${data.evidence.latest_received}/${data.evidence.latest_chunks}`
                    : "—",
              },
              {
                label: "流量事件",
                value: formatInteger(data.consumers.traffic_entries),
              },
              {
                label: "最近流量应用",
                value: formatTime(data.consumers.traffic_applied_at),
              },
              {
                label: "H&R 事件",
                value: formatInteger(data.consumers.hnr_events),
              },
              {
                label: "最近 H&R 应用",
                value: formatTime(data.consumers.hnr_applied_at),
              },
            ]}
          />
        </StatusCard>
      </div>
    </StaffPageFrame>
  )
}

function StatusCard({
  title,
  description,
  children,
}: {
  title: string
  description: string
  children: ReactNode
}) {
  return (
    <Card className="gap-0 py-0">
      <CardHeader className="p-6 pb-4">
        <CardTitle>{title}</CardTitle>
        <CardDescription>{description}</CardDescription>
      </CardHeader>
      <CardContent className="p-0">{children}</CardContent>
    </Card>
  )
}

function evidenceStatusLabel(value: string) {
  if (!value) return "尚无窗口"
  return value === "complete" ? "已完成" : "组装中"
}

function evidenceHealthLabel(value: string) {
  switch (value) {
    case "healthy":
      return "正常"
    case "lagging":
      return "有延迟"
    case "broken":
      return "链路中断"
    default:
      return "尚无证据"
  }
}

function evidenceHealthDescription(value: string) {
  if (value === "lagging") return "最近小时的做种证据尚未到齐。"
  if (value === "broken") return "本月证据存在历史空洞或长期未完成窗口。"
  return "本月尚未收到可结算的做种证据。"
}

function formatTime(value: string | null) {
  return value ? formatCompactDateTime(value) : "—"
}

function TrackerSettingsSkeleton() {
  return (
    <StaffPageFrame aria-label="正在加载 Tracker 状态">
      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
        {Array.from({ length: 4 }, (_, index) => (
          <Skeleton key={index} className="h-36 rounded-lg" />
        ))}
      </div>
      <Skeleton className="h-20 rounded-lg" />
      <div className="grid gap-4 xl:grid-cols-2">
        <Skeleton className="h-96 rounded-lg" />
        <Skeleton className="h-96 rounded-lg" />
      </div>
    </StaffPageFrame>
  )
}

import { RefreshCwIcon } from "lucide-react"

import { Badge } from "~/components/ui/badge"
import { Button } from "~/components/ui/button"
import { Skeleton } from "~/components/ui/skeleton"
import { useTorrentSwarm } from "~/features/torrent/api/torrent.queries"
import { cn } from "~/lib/utils"
import { formatDateTime } from "~/shared/formatters/date-time"

export function TorrentSwarmOverview({
  torrentId,
  compact = false,
  showFreshness = true,
  className,
}: {
  torrentId: number
  compact?: boolean
  showFreshness?: boolean
  className?: string
}) {
  const swarm = useTorrentSwarm(torrentId)

  return (
    <section
      aria-labelledby="torrent-swarm-title"
      className={cn("w-full", className)}
    >
      <div className="flex flex-wrap items-center gap-x-4 gap-y-1">
        <h2
          id="torrent-swarm-title"
          className={compact ? "sr-only" : "text-sm font-semibold"}
        >
          做种状态
        </h2>

        {swarm.isPending ? <TorrentSwarmSkeleton /> : null}

        {swarm.isError ? (
          <div className="flex flex-1 flex-wrap items-center justify-between gap-2 text-xs text-muted-foreground">
            <p>做种统计暂时无法加载，种子详情和下载不受影响。</p>
            <Button
              type="button"
              variant="ghost"
              size="sm"
              onClick={() => void swarm.refetch()}
            >
              <RefreshCwIcon data-icon="inline-start" />
              重试
            </Button>
          </div>
        ) : null}

        {swarm.data ? (
          <>
            <dl className="flex flex-wrap items-center gap-x-5 gap-y-1">
              <SwarmFact
                label="做种"
                value={swarmValue(swarm.data.seeders, swarm.data.confidence)}
                tone="success"
              />
              <SwarmFact
                label="下载"
                value={swarmValue(swarm.data.leechers, swarm.data.confidence)}
                tone="primary"
              />
              <SwarmFact
                label="完成"
                value={swarmValue(swarm.data.completed, swarm.data.confidence)}
              />
            </dl>
            {showFreshness && swarm.data.confidence === "stale" ? (
              <Badge variant="outline">统计稍有延迟</Badge>
            ) : null}
            {showFreshness ? (
              <p className="ml-auto text-xs text-muted-foreground">
                {swarm.data.observed_at
                  ? `更新于 ${formatDateTime(swarm.data.observed_at)}`
                  : "正在等待首次统计"}
              </p>
            ) : null}
          </>
        ) : null}
      </div>
    </section>
  )
}

function SwarmFact({
  label,
  value,
  tone,
}: {
  label: string
  value: string
  tone?: "success" | "primary"
}) {
  return (
    <div className="flex min-w-0 items-baseline gap-1.5">
      <dt className="text-sm text-muted-foreground">{label}:</dt>
      <dd
        className={
          tone === "success"
            ? "font-semibold text-success-foreground tabular-nums"
            : tone === "primary"
              ? "font-semibold text-primary tabular-nums"
              : "font-semibold tabular-nums"
        }
      >
        {value}
      </dd>
    </div>
  )
}

function TorrentSwarmSkeleton() {
  return (
    <div
      className="flex items-center gap-4"
      aria-label="正在加载做种状态"
      aria-busy="true"
    >
      {Array.from({ length: 3 }, (_, index) => (
        <Skeleton key={index} className="h-5 w-12" />
      ))}
    </div>
  )
}

function swarmValue(value: number, confidence: string) {
  return confidence === "unavailable" ? "—" : value.toLocaleString("zh-CN")
}

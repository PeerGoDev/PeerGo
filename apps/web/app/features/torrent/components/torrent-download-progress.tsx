import { Progress } from "~/components/ui/progress"
import type { TrafficOverview } from "~/features/traffic/api/traffic.queries"
import { cn } from "~/lib/utils"

export type TorrentActivity = TrafficOverview["torrent_activity"][number]

export function TorrentDownloadProgress({
  activity,
  className,
  compact = false,
  overlay = false,
}: {
  activity: TorrentActivity | undefined
  className?: string
  compact?: boolean
  overlay?: boolean
}) {
  if (
    !activity ||
    (activity.raw_downloaded_bytes === "0" && !activity.completed)
  )
    return null
  const percent = Math.max(
    0,
    Math.min(100, activity.progress_basis_points / 100)
  )

  if (overlay) {
    return (
      <div
        className={cn(
          "pointer-events-none absolute inset-x-0 bottom-0 z-10 h-1 bg-black/20",
          className
        )}
        role="progressbar"
        aria-label={`下载进度 ${percent.toFixed(1)}%`}
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={percent}
        title={`已下载 ${percent.toFixed(1)}%`}
      >
        <div
          className={cn(
            "h-full transition-[width] duration-300",
            activity.completed ? "bg-success/80" : "bg-blue-500/80"
          )}
          style={{ width: `${activity.completed ? 100 : percent}%` }}
        />
      </div>
    )
  }

  return (
    <div
      className={cn("mt-1 min-w-0", className)}
      title={`已下载 ${percent.toFixed(1)}%`}
    >
      <div className="mb-0.5 flex items-center justify-between gap-2 text-[10px] leading-none text-muted-foreground">
        <span>{activity.completed ? "已完成" : "下载进度"}</span>
        <span className="tabular-nums">
          {percent.toFixed(compact ? 0 : 1)}%
        </span>
      </div>
      <Progress
        value={percent}
        aria-label={`下载进度 ${percent.toFixed(1)}%`}
      />
    </div>
  )
}

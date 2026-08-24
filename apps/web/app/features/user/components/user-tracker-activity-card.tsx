import { Link } from "react-router"
import { BoxIcon, MonitorIcon, NetworkIcon } from "lucide-react"

import { Badge } from "~/components/ui/badge"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "~/components/ui/card"
import { Progress } from "~/components/ui/progress"
import type { UserTrackerActivity } from "~/features/user/api/tracker-activity.queries"
import { formatBytes } from "~/shared/formatters/bytes"
import { formatCompactDateTime } from "~/shared/formatters/date-time"

export function UserTrackerActivityCard({
  activity,
  loading,
  error,
  visibility,
}: {
  activity: UserTrackerActivity | undefined
  loading: boolean
  error: boolean
  visibility: "self" | "admin"
}) {
  const seeding =
    activity?.items.filter((item) => item.seeding_connections > 0).length ?? 0
  const leeching =
    activity?.items.filter((item) => item.leeching_connections > 0).length ?? 0

  return (
    <Card className="gap-0 rounded-lg border py-0 shadow-sm ring-0">
      <CardHeader className="px-6 pt-6 pb-4">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div>
            <CardTitle>
              <h2>活跃任务与 BT 在线客户端</h2>
            </CardTitle>
            <CardDescription className="mt-1">
              {visibility === "admin" ? "管理员实时视图" : "仅自己可见"} ·
              不保存客户端活动历史，也不显示 IP
            </CardDescription>
          </div>
          <div className="flex flex-wrap gap-2 text-xs">
            <Badge variant="secondary">做种 {seeding}</Badge>
            <Badge variant="secondary">下载 {leeching}</Badge>
            <Badge variant="outline">
              连接 {activity?.total_connections ?? 0}
            </Badge>
          </div>
        </div>
      </CardHeader>
      <CardContent className="px-6 pb-6">
        {loading && !activity ? (
          <p className="py-6 text-sm text-muted-foreground">
            正在读取 Tracker 当前活动…
          </p>
        ) : error ? (
          <p className="py-6 text-sm text-destructive">
            Tracker 当前活动暂时不可用，请稍后重试。
          </p>
        ) : !activity?.items.length ? (
          <p className="py-6 text-sm text-muted-foreground">
            当前没有在线 BT 任务。
          </p>
        ) : (
          <div className="divide-y rounded-lg border">
            {activity.items.map((item) => {
              const progress = item.progress_basis_points / 100
              return (
                <div
                  key={`${item.torrent_id}-${item.info_hash_v1}`}
                  className="grid gap-3 p-4 lg:grid-cols-[minmax(0,1fr)_minmax(13rem,0.7fr)_11rem]"
                >
                  <div className="min-w-0">
                    <Link
                      to={`/torrents/${item.torrent_id}`}
                      className="font-medium underline-offset-4 hover:text-primary hover:underline"
                    >
                      种子 #{item.torrent_id}
                    </Link>
                    <p className="truncate font-mono text-[11px] text-muted-foreground">
                      {item.info_hash_v1}
                    </p>
                    <div className="mt-2 flex flex-wrap gap-1.5">
                      {item.address_families.map((family) => (
                        <Badge key={family} variant="outline">
                          {family}
                        </Badge>
                      ))}
                      {item.address_families.length === 2 ? (
                        <Badge variant="secondary">
                          <NetworkIcon data-icon="inline-start" />
                          双栈
                        </Badge>
                      ) : null}
                      {item.seedbox ? (
                        <Badge variant="secondary">
                          <BoxIcon data-icon="inline-start" />
                          盒子
                        </Badge>
                      ) : null}
                    </div>
                  </div>
                  <div className="min-w-0 text-sm">
                    <div className="flex items-center gap-1.5 text-muted-foreground">
                      <MonitorIcon className="size-3.5" />
                      {item.client_families.join(" / ")}
                    </div>
                    <p className="mt-1">
                      上传 {formatBytes(item.upload_speed)}/s · 下载{" "}
                      {formatBytes(item.download_speed)}/s
                    </p>
                    <p className="text-xs text-muted-foreground">
                      累计 ↑ {formatBytes(item.uploaded)} · ↓{" "}
                      {formatBytes(item.downloaded)}
                    </p>
                  </div>
                  <div className="min-w-0">
                    <div className="mb-1 flex justify-between text-xs">
                      <span>
                        {item.seeding_connections > 0 ? "做种" : "下载"}
                      </span>
                      <span>{progress.toFixed(1)}%</span>
                    </div>
                    <Progress
                      value={progress}
                      aria-label={`完成进度 ${progress.toFixed(1)}%`}
                    />
                    <p className="mt-2 text-right text-[11px] text-muted-foreground">
                      <time dateTime={item.last_announce}>
                        {formatCompactDateTime(item.last_announce)}
                      </time>
                    </p>
                  </div>
                </div>
              )
            })}
          </div>
        )}
        {activity?.truncated ? (
          <p className="mt-3 text-xs text-warning-foreground">
            活动较多，当前只显示最近的有界结果。
          </p>
        ) : null}
      </CardContent>
    </Card>
  )
}

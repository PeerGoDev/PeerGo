import * as React from "react"
import { UsersIcon } from "lucide-react"

import { Card, CardContent, CardHeader, CardTitle } from "~/components/ui/card"
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "~/components/ui/collapsible"
import { Skeleton } from "~/components/ui/skeleton"
import { useTorrentSwarm } from "~/features/torrent/api/torrent.queries"

export function TorrentPeerListCard({ torrentId }: { torrentId: number }) {
  const [open, setOpen] = React.useState(false)
  const swarm = useTorrentSwarm(torrentId)

  return (
    <Card className="gap-0 rounded-lg border py-0 shadow-sm ring-0">
      <Collapsible open={open} onOpenChange={setOpen}>
        <CardHeader className="p-6 pb-2">
          <CollapsibleTrigger className="w-full cursor-pointer transition-colors hover:text-primary">
            <CardTitle className="flex items-center justify-between gap-4 text-base font-semibold">
              <span className="flex items-center gap-2">
                <UsersIcon className="size-4" />
                用户列表
              </span>
              <span className="flex items-center gap-3 text-sm font-normal">
                {swarm.isPending ? (
                  <Skeleton className="h-4 w-28" />
                ) : (
                  <>
                    <span className="text-green-500">
                      {swarmCount(swarm.data?.seeders, swarm.data?.confidence)}{" "}
                      个做种
                    </span>
                    <span className="text-blue-500">
                      {swarmCount(swarm.data?.leechers, swarm.data?.confidence)}{" "}
                      个下载者
                    </span>
                  </>
                )}
                <span className="text-xs text-muted-foreground">
                  {open ? "▲ 收起" : "▼ 展开"}
                </span>
              </span>
            </CardTitle>
          </CollapsibleTrigger>
        </CardHeader>

        <CollapsibleContent>
          <CardContent className="space-y-4">
            {swarm.isError ? (
              <p className="py-4 text-center text-sm text-muted-foreground">
                活跃统计暂时不可用，请稍后重试。
              </p>
            ) : swarm.data ? (
              <>
                <div className="grid gap-3 sm:grid-cols-3">
                  <SwarmMetric
                    label="做种连接"
                    value={swarmCount(
                      swarm.data.seeders,
                      swarm.data.confidence
                    )}
                    tone="text-green-500"
                  />
                  <SwarmMetric
                    label="下载连接"
                    value={swarmCount(
                      swarm.data.leechers,
                      swarm.data.confidence
                    )}
                    tone="text-blue-500"
                  />
                  <SwarmMetric
                    label="累计完成"
                    value={swarmCount(
                      swarm.data.completed,
                      swarm.data.confidence
                    )}
                  />
                </div>
                <p className="text-center text-xs text-muted-foreground">
                  为保护成员隐私，这里只显示 Tracker
                  聚合统计，不公开用户、IP、客户端或单次汇报信息。
                </p>
              </>
            ) : null}
          </CardContent>
        </CollapsibleContent>
      </Collapsible>
    </Card>
  )
}

function SwarmMetric({
  label,
  value,
  tone,
}: {
  label: string
  value: string
  tone?: string
}) {
  return (
    <div className="rounded-lg bg-muted/30 px-4 py-3 text-center">
      <div className={`text-lg font-semibold tabular-nums ${tone ?? ""}`}>
        {value}
      </div>
      <div className="mt-1 text-xs text-muted-foreground">{label}</div>
    </div>
  )
}

function swarmCount(value: number | undefined, confidence: string | undefined) {
  if (value === undefined || confidence === "unavailable") return "—"
  return value.toLocaleString("zh-CN")
}

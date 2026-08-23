import * as React from "react"
import { Link } from "react-router"
import { DownloadIcon, UploadIcon, UsersIcon } from "lucide-react"

import { Card, CardContent, CardHeader, CardTitle } from "~/components/ui/card"
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "~/components/ui/collapsible"
import { Skeleton } from "~/components/ui/skeleton"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "~/components/ui/table"
import { useWebSession } from "~/features/auth/api/session.mutations"
import {
  type TorrentPeerList,
  useTorrentPeers,
  useTorrentSwarm,
} from "~/features/torrent/api/torrent.queries"
import { formatRelativeTime } from "~/features/torrent/model/format"
import { ApiProblemError } from "~/shared/api/problem"
import { formatBytes } from "~/shared/formatters/bytes"
import { formatCompactDateTime } from "~/shared/formatters/date-time"

type TorrentPeer = TorrentPeerList["items"][number]

export function TorrentPeerListCard({ torrentId }: { torrentId: number }) {
  const [open, setOpen] = React.useState(false)
  const session = useWebSession()
  const swarm = useTorrentSwarm(torrentId)
  const peers = useTorrentPeers(torrentId, Boolean(session.data))
  const grouped = React.useMemo(
    () => groupPeers(peers.data?.items ?? []),
    [peers.data?.items]
  )
  const seedingCount = peers.data
    ? grouped.seeders.length.toLocaleString("zh-CN")
    : swarmCount(swarm.data?.seeders, swarm.data?.confidence)
  const leechingCount = peers.data
    ? grouped.leechers.length.toLocaleString("zh-CN")
    : swarmCount(swarm.data?.leechers, swarm.data?.confidence)

  return (
    <Card className="gap-0 rounded-lg border py-0 shadow-sm ring-0">
      <Collapsible open={open} onOpenChange={setOpen}>
        <CardHeader className="p-6 pb-2">
          <CollapsibleTrigger className="w-full cursor-pointer transition-colors hover:text-primary">
            <CardTitle className="flex items-center justify-between gap-4 text-base font-semibold max-sm:items-start">
              <span className="flex items-center gap-2">
                <UsersIcon className="size-4" />
                User List
              </span>
              <span className="flex flex-wrap items-center justify-end gap-3 text-sm font-normal">
                {swarm.isPending && !peers.data ? (
                  <Skeleton className="h-4 w-28" />
                ) : (
                  <>
                    <span className="text-green-500">
                      {seedingCount} seeding
                    </span>
                    <span className="text-blue-500">
                      {leechingCount} 个下载者
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
          <CardContent className="space-y-6">
            <PeerListContent
              signedIn={Boolean(session.data)}
              peers={peers}
              grouped={grouped}
            />
          </CardContent>
        </CollapsibleContent>
      </Collapsible>
    </Card>
  )
}

function PeerListContent({
  signedIn,
  peers,
  grouped,
}: {
  signedIn: boolean
  peers: ReturnType<typeof useTorrentPeers>
  grouped: ReturnType<typeof groupPeers>
}) {
  if (!signedIn) {
    return (
      <p className="py-4 text-center text-sm text-muted-foreground">
        登录后可查看当前做种者和下载者。
      </p>
    )
  }
  if (peers.isPending) return <PeerTableSkeleton />
  if (peers.isError) {
    const sessionExpired =
      peers.error instanceof ApiProblemError && peers.error.status === 401
    return (
      <p className="py-4 text-center text-sm text-muted-foreground">
        {sessionExpired
          ? "登录状态已经失效，请重新登录后查看。"
          : "实时用户暂时不可用，请稍后重试。"}
      </p>
    )
  }
  if (!peers.data || peers.data.items.length === 0) {
    return (
      <p className="py-4 text-center text-sm text-muted-foreground">
        暂无活跃用户
      </p>
    )
  }

  return (
    <>
      <PeerSection kind="seeding" peers={grouped.seeders} />
      <PeerSection kind="leeching" peers={grouped.leechers} />
      {peers.data.truncated ? (
        <p className="text-center text-xs text-muted-foreground">
          活跃连接超过 200 个，当前显示 Tracker 最近返回的部分用户。
        </p>
      ) : null}
    </>
  )
}

function PeerSection({
  kind,
  peers,
}: {
  kind: "seeding" | "leeching"
  peers: TorrentPeer[]
}) {
  const seeding = kind === "seeding"
  return (
    <section>
      <h4
        className={`mb-2 flex items-center gap-2 text-sm font-medium ${seeding ? "text-green-500" : "text-blue-500"}`}
      >
        {seeding ? (
          <UploadIcon className="size-3.5" />
        ) : (
          <DownloadIcon className="size-3.5" />
        )}
        {seeding ? "做种者" : "下载者"} ({peers.length})
      </h4>
      {peers.length === 0 ? (
        <p className="rounded bg-muted/30 py-3 text-center text-sm text-muted-foreground">
          暂无{seeding ? "做种者" : "下载者"}
        </p>
      ) : (
        <div className="overflow-x-auto rounded-lg border">
          <Table className={seeding ? "min-w-[680px]" : "min-w-[760px]"}>
            <TableHeader>
              <TableRow className="bg-muted/30 text-xs text-muted-foreground">
                <TableHead className="px-3 py-2">用户</TableHead>
                {!seeding ? (
                  <TableHead className="px-3 py-2 text-center">进度</TableHead>
                ) : null}
                <TableHead className="px-3 py-2 text-center">分享率</TableHead>
                <TableHead className="px-3 py-2 text-right">上传量</TableHead>
                <TableHead className="px-3 py-2 text-right">下载量</TableHead>
                <TableHead className="px-3 py-2">客户端</TableHead>
                <TableHead className="px-3 py-2 text-right">汇报</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {peers.map((peer) => (
                <TableRow
                  key={`${kind}:${peer.username}`}
                  className="text-xs hover:bg-muted/50"
                >
                  <TableCell className="px-3 py-2">
                    <div className="flex items-center gap-1">
                      <Link
                        to={`/user/${encodeURIComponent(peer.username)}`}
                        className="text-primary hover:underline"
                      >
                        {peer.display_name}
                      </Link>
                      {peer.uploader ? (
                        <span className="text-green-500" title="发布者">
                          发布者
                        </span>
                      ) : null}
                    </div>
                    {peer.display_name !== peer.username ? (
                      <div className="text-[11px] text-muted-foreground">
                        @{peer.username}
                      </div>
                    ) : null}
                  </TableCell>
                  {!seeding ? (
                    <TableCell className="px-3 py-2 text-center">
                      <PeerProgress basisPoints={peer.progress_basis_points} />
                    </TableCell>
                  ) : null}
                  <TableCell className="px-3 py-2 text-center">
                    <span className={ratioTone(peer.uploaded, peer.downloaded)}>
                      {formatRatio(peer.uploaded, peer.downloaded)}
                    </span>
                  </TableCell>
                  <TableCell className="px-3 py-2 text-right text-green-500">
                    {formatBytes(peer.uploaded)}
                  </TableCell>
                  <TableCell className="px-3 py-2 text-right text-blue-500">
                    {formatBytes(peer.downloaded)}
                  </TableCell>
                  <TableCell className="px-3 py-2">
                    <span title={peer.client_families.join(" / ")}>
                      {peer.client_families.map(clientLabel).join(" / ") ||
                        "未知客户端"}
                    </span>
                    {connectionCount(peer, kind) > 1 ? (
                      <span className="ml-1 rounded bg-muted px-1 py-0.5 text-[10px] text-muted-foreground">
                        ×{connectionCount(peer, kind)}
                      </span>
                    ) : null}
                  </TableCell>
                  <TableCell className="px-3 py-2 text-right text-muted-foreground">
                    <time
                      dateTime={peer.last_announce}
                      title={formatCompactDateTime(peer.last_announce)}
                    >
                      {formatRelativeTime(peer.last_announce)}
                    </time>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}
    </section>
  )
}

function PeerProgress({ basisPoints }: { basisPoints: number }) {
  const percentage = Math.min(100, Math.max(0, basisPoints / 100))
  return (
    <div className="flex items-center justify-center gap-1">
      <div className="h-1.5 w-12 overflow-hidden rounded-full bg-muted">
        <div
          className="h-full bg-blue-500 transition-all"
          style={{ width: `${percentage}%` }}
        />
      </div>
      <span className="text-blue-500">{percentage.toFixed(0)}%</span>
    </div>
  )
}

function groupPeers(items: TorrentPeer[]) {
  return {
    seeders: items.filter((peer) => peer.seeding_connections > 0),
    leechers: items.filter((peer) => peer.leeching_connections > 0),
  }
}

function formatRatio(uploaded: string, downloaded: string) {
  const uploadedBytes = BigInt(uploaded)
  const downloadedBytes = BigInt(downloaded)
  if (downloadedBytes === 0n) return uploadedBytes > 0n ? "∞" : "0.00"
  const hundredths = (uploadedBytes * 100n) / downloadedBytes
  return (Number(hundredths) / 100).toFixed(2)
}

function ratioTone(uploaded: string, downloaded: string) {
  if (downloaded === "0") return uploaded === "0" ? "" : "text-green-500"
  const ratio = Number(uploaded) / Number(downloaded)
  if (ratio >= 1) return "text-green-500"
  if (ratio >= 0.5) return "text-amber-500"
  return "text-destructive"
}

function swarmCount(value: number | undefined, confidence: string | undefined) {
  if (value === undefined || confidence === "unavailable") return "—"
  return value.toLocaleString("zh-CN")
}

function clientLabel(client: string) {
  switch (client) {
    case "qbittorrent":
      return "qBittorrent"
    case "transmission":
      return "Transmission"
    case "deluge":
      return "Deluge"
    case "libtorrent":
      return "libtorrent"
    case "utorrent":
      return "µTorrent"
    default:
      return "未知客户端"
  }
}

function connectionCount(peer: TorrentPeer, kind: "seeding" | "leeching") {
  return kind === "seeding"
    ? peer.seeding_connections
    : peer.leeching_connections
}

function PeerTableSkeleton() {
  return (
    <div className="space-y-2 rounded-lg border p-4">
      <Skeleton className="h-5 w-48" />
      <Skeleton className="h-10 w-full" />
      <Skeleton className="h-10 w-full" />
    </div>
  )
}

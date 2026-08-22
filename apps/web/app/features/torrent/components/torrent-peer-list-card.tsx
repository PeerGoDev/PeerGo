import * as React from "react"
import { Link } from "react-router"
import { ShieldCheckIcon, UsersIcon } from "lucide-react"

import { Alert, AlertDescription, AlertTitle } from "~/components/ui/alert"
import { Badge } from "~/components/ui/badge"
import { Button } from "~/components/ui/button"
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
import { useCapabilities } from "~/features/authz/api/capabilities.queries"
import {
  useStaffCapabilities,
  useStaffSession,
} from "~/features/staff/api/staff-session.mutations"
import { hasCapability } from "~/features/staff/model/capability"
import {
  type ManagedTorrentPeerList,
  useManagedTorrentPeers,
  useTorrentSwarm,
} from "~/features/torrent/api/torrent.queries"
import { ApiProblemError } from "~/shared/api/problem"
import { formatBytes } from "~/shared/formatters/bytes"
import { formatCompactDateTime } from "~/shared/formatters/date-time"

export function TorrentPeerListCard({ torrentId }: { torrentId: number }) {
  const [open, setOpen] = React.useState(false)
  const swarm = useTorrentSwarm(torrentId)
  const webSession = useWebSession()
  const webCapabilities = useCapabilities(webSession.data?.user.id)
  const canStartStaffSession = hasCapability(
    webCapabilities.data,
    "staff.session.create.self"
  )
  const staffSession = useStaffSession(open && Boolean(webSession.data))
  const staffCapabilities = useStaffCapabilities(staffSession.data?.user.id)
  const canManageTorrent = hasCapability(
    staffCapabilities.data,
    "torrent.manage.read"
  )
  const peers = useManagedTorrentPeers(
    torrentId,
    open && Boolean(staffSession.data) && canManageTorrent
  )

  return (
    <Card className="gap-0 rounded-lg border py-0 shadow-sm ring-0">
      <Collapsible open={open} onOpenChange={setOpen}>
        <CardHeader className="p-6 pb-2">
          <CollapsibleTrigger className="w-full cursor-pointer transition-colors hover:text-primary">
            <CardTitle className="flex items-center justify-between gap-4 text-base font-semibold max-sm:items-start">
              <span className="flex items-center gap-2">
                <UsersIcon className="size-4" />
                用户列表
              </span>
              <span className="flex flex-wrap items-center justify-end gap-3 text-sm font-normal">
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
              <div className="grid gap-3 sm:grid-cols-3">
                <SwarmMetric
                  label="做种连接"
                  value={swarmCount(swarm.data.seeders, swarm.data.confidence)}
                  tone="text-green-500"
                />
                <SwarmMetric
                  label="下载连接"
                  value={swarmCount(swarm.data.leechers, swarm.data.confidence)}
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
            ) : null}

            <PeerManagementView
              canUseStaffView={
                canStartStaffSession || Boolean(staffSession.data)
              }
              staffLookupEnabled={open && Boolean(webSession.data)}
              staffSessionPending={staffSession.isPending}
              hasStaffSession={Boolean(staffSession.data)}
              staffCapabilitiesPending={staffCapabilities.isPending}
              canManageTorrent={canManageTorrent}
              peers={peers}
            />
          </CardContent>
        </CollapsibleContent>
      </Collapsible>
    </Card>
  )
}

function PeerManagementView({
  canUseStaffView,
  staffLookupEnabled,
  staffSessionPending,
  hasStaffSession,
  staffCapabilitiesPending,
  canManageTorrent,
  peers,
}: {
  canUseStaffView: boolean
  staffLookupEnabled: boolean
  staffSessionPending: boolean
  hasStaffSession: boolean
  staffCapabilitiesPending: boolean
  canManageTorrent: boolean
  peers: ReturnType<typeof useManagedTorrentPeers>
}) {
  if (staffLookupEnabled && staffSessionPending) {
    return <PeerTableSkeleton />
  }
  if (!canUseStaffView) {
    return (
      <p className="text-center text-xs text-muted-foreground">
        普通成员只显示 Tracker
        聚合统计；用户身份、客户端和连接状态仅对种子管理人员开放。
      </p>
    )
  }
  if (hasStaffSession && staffCapabilitiesPending) {
    return <PeerTableSkeleton />
  }
  if (!hasStaffSession) {
    return (
      <Alert>
        <ShieldCheckIcon />
        <AlertTitle>管理视图需要后台会话</AlertTitle>
        <AlertDescription className="flex flex-wrap items-center justify-between gap-3">
          <span>
            完成 WebAuthn 验证后，可查看当前活跃用户；不会显示 IP、端口或
            passkey。
          </span>
          <Button size="sm" variant="outline" render={<Link to="/staff" />}>
            开启后台会话
          </Button>
        </AlertDescription>
      </Alert>
    )
  }
  if (!canManageTorrent) {
    return (
      <p className="text-center text-xs text-muted-foreground">
        当前后台身份没有 torrent.manage.read 权限，因此仍只显示聚合统计。
      </p>
    )
  }
  if (peers.isPending) return <PeerTableSkeleton />
  if (peers.isError) {
    const needsElevation =
      peers.error instanceof ApiProblemError && peers.error.status === 401
    return (
      <Alert variant="destructive">
        <AlertTitle>
          {needsElevation ? "后台会话已经失效" : "实时用户暂时不可用"}
        </AlertTitle>
        <AlertDescription>
          {needsElevation
            ? "请重新进入管理后台完成验证后再试。"
            : "Tracker 当前无法提供内存中的活跃连接，请稍后重试。"}
        </AlertDescription>
      </Alert>
    )
  }
  if (!peers.data) return null
  return <ManagedPeerTable page={peers.data} />
}

function ManagedPeerTable({ page }: { page: ManagedTorrentPeerList }) {
  if (page.items.length === 0) {
    return (
      <div className="rounded-lg border border-dashed px-4 py-8 text-center text-sm text-muted-foreground">
        当前没有活跃用户。
      </div>
    )
  }
  return (
    <div className="space-y-3">
      <div className="rounded-lg border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>用户</TableHead>
              <TableHead>状态</TableHead>
              <TableHead>进度</TableHead>
              <TableHead>客户端</TableHead>
              <TableHead className="text-right">上传</TableHead>
              <TableHead className="text-right">下载</TableHead>
              <TableHead className="text-right">最近汇报</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {page.items.map((peer) => (
              <TableRow key={peer.user_id}>
                <TableCell>
                  <div className="flex items-center gap-2">
                    <Link
                      to={`/user/${encodeURIComponent(peer.username)}`}
                      className="font-medium text-primary hover:underline"
                    >
                      {peer.display_name}
                    </Link>
                    <span className="text-xs text-muted-foreground">
                      #{peer.user_numeric_id}
                    </span>
                    {peer.uploader ? (
                      <Badge variant="secondary">上传者</Badge>
                    ) : null}
                  </div>
                  <div className="text-xs text-muted-foreground">
                    @{peer.username}
                  </div>
                </TableCell>
                <TableCell>
                  <div className="flex flex-wrap gap-1">
                    {peer.seeding_connections > 0 ? (
                      <Badge
                        className="bg-green-500/10 text-green-600 dark:text-green-400"
                        variant="outline"
                      >
                        做种 {connectionSuffix(peer.seeding_connections)}
                      </Badge>
                    ) : null}
                    {peer.leeching_connections > 0 ? (
                      <Badge
                        className="bg-blue-500/10 text-blue-600 dark:text-blue-400"
                        variant="outline"
                      >
                        下载 {connectionSuffix(peer.leeching_connections)}
                      </Badge>
                    ) : null}
                  </div>
                </TableCell>
                <TableCell className="tabular-nums">
                  {(peer.progress_basis_points / 100).toFixed(1)}%
                </TableCell>
                <TableCell>
                  {peer.client_families.map(clientLabel).join(" / ")}
                </TableCell>
                <TableCell className="text-right tabular-nums">
                  {formatBytes(peer.uploaded)}
                </TableCell>
                <TableCell className="text-right tabular-nums">
                  {formatBytes(peer.downloaded)}
                </TableCell>
                <TableCell className="text-right text-muted-foreground">
                  <time dateTime={peer.last_announce}>
                    {formatCompactDateTime(peer.last_announce)}
                  </time>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>
      <p className="text-center text-xs text-muted-foreground">
        管理视图直接读取 Tracker 的 TTL 内存状态，本次返回{" "}
        {page.total_connections} 个连接并按用户合并； 不写入活动明细，也不展示
        IP、端口、peer ID 或 passkey。
        {page.truncated
          ? " 活跃连接超过 200 个，当前仅显示最近返回的一部分。"
          : ""}
      </p>
    </div>
  )
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

function connectionSuffix(count: number) {
  return count > 1 ? `×${count}` : ""
}

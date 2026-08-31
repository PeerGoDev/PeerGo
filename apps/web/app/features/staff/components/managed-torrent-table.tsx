import { Link } from "react-router"
import {
  ArrowUpRightIcon,
  BanIcon,
  ClipboardCheckIcon,
  CoinsIcon,
  HardDriveIcon,
  RotateCcwIcon,
} from "lucide-react"

import { Badge } from "~/components/ui/badge"
import { Button } from "~/components/ui/button"
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "~/components/ui/empty"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "~/components/ui/table"
import type { ManagedTorrent } from "~/features/staff/api/torrent-administration.queries"
import { managedTorrentStateLabel } from "~/features/staff/model/torrent-administration"
import { formatBytes } from "~/shared/formatters/bytes"
import { formatCompactDateTime } from "~/shared/formatters/date-time"

export function ManagedTorrentTable({
  torrents,
  hasFilters,
  canChangeAvailability,
  onChangeAvailability,
  canManagePurchase,
  onManagePurchase,
}: {
  torrents: ManagedTorrent[]
  hasFilters: boolean
  canChangeAvailability: boolean
  onChangeAvailability: (torrent: ManagedTorrent) => void
  canManagePurchase: boolean
  onManagePurchase: (torrent: ManagedTorrent) => void
}) {
  if (torrents.length === 0) {
    return (
      <Empty className="min-h-72 border">
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <HardDriveIcon />
          </EmptyMedia>
          <EmptyTitle>{hasFilters ? "没有匹配种子" : "暂无种子"}</EmptyTitle>
          <EmptyDescription>
            {hasFilters
              ? "请调整关键词、生命周期状态或分类筛选。"
              : "用户提交的种子会显示在这里。"}
          </EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  }

  return (
    <div className="overflow-hidden rounded-lg border">
      <p className="border-b bg-muted/30 px-3 py-2 text-xs text-muted-foreground md:hidden">
        表格可左右滑动，操作按钮位于末列
      </p>
      <Table className="min-w-[1160px] lg:min-w-[1280px]">
        <TableHeader className="bg-muted/50">
          <TableRow className="h-11">
            <TableHead className="w-[72px] px-3 text-right">ID</TableHead>
            <TableHead className="w-[430px]">种子名称</TableHead>
            <TableHead className="w-[150px]">上传者</TableHead>
            <TableHead className="w-[90px]">分类</TableHead>
            <TableHead className="w-[100px] text-right">大小</TableHead>
            <TableHead className="w-[100px] text-right">价格</TableHead>
            <TableHead className="w-[90px] text-center">状态</TableHead>
            <TableHead className="w-[85px] text-center">优惠</TableHead>
            <TableHead className="w-[110px] text-center">做种 / 下载</TableHead>
            <TableHead className="w-[130px]">更新时间</TableHead>
            <TableHead className="text-center">操作</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {torrents.map((torrent) => (
            <TableRow key={torrent.id} className="h-[58px] hover:bg-muted/30">
              <TableCell className="px-3 text-right font-mono text-xs text-muted-foreground tabular-nums">
                {torrent.id}
              </TableCell>
              <TableCell>
                <TorrentIdentity torrent={torrent} />
              </TableCell>
              <TableCell>
                <div className="flex flex-col">
                  <span className="text-sm font-medium">
                    {torrent.uploader_username}
                  </span>
                  <span className="text-xs text-muted-foreground">
                    用户 #{torrent.uploader_numeric_id}
                  </span>
                </div>
              </TableCell>
              <TableCell>
                <Badge variant="outline">{torrent.category_name}</Badge>
              </TableCell>
              <TableCell className="text-right tabular-nums">
                {formatBytes(torrent.total_size_bytes)}
              </TableCell>
              <TableCell className="text-right tabular-nums">
                {torrent.purchase_price === "0"
                  ? "免费"
                  : `${torrent.purchase_price} 魔力`}
              </TableCell>
              <TableCell className="text-center">
                <TorrentStateBadge state={torrent.state} />
              </TableCell>
              <TableCell className="text-center">
                <PromotionBadge promotion={torrent.promotion} />
              </TableCell>
              <TableCell className="text-center">
                <SwarmNumbers torrent={torrent} />
              </TableCell>
              <TableCell>
                <span className="text-xs text-muted-foreground">
                  {formatCompactDateTime(torrent.updated_at)}
                </span>
              </TableCell>
              <TableCell className="text-center">
                <TorrentActions
                  torrent={torrent}
                  canChangeAvailability={canChangeAvailability}
                  onChangeAvailability={onChangeAvailability}
                  canManagePurchase={canManagePurchase}
                  onManagePurchase={onManagePurchase}
                />
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  )
}

function TorrentIdentity({ torrent }: { torrent: ManagedTorrent }) {
  const content = (
    <div className="flex min-w-0 flex-col">
      <span className="truncate font-medium text-foreground">
        {torrent.title}
      </span>
      {torrent.subtitle ? (
        <span className="truncate text-xs text-muted-foreground">
          {torrent.subtitle}
        </span>
      ) : null}
    </div>
  )
  return torrent.state === "published" ? (
    <Link
      to={`/torrents/${torrent.id}`}
      className="block min-w-0 hover:text-primary"
    >
      {content}
    </Link>
  ) : (
    content
  )
}

function TorrentStateBadge({ state }: { state: ManagedTorrent["state"] }) {
  const variant =
    state === "published"
      ? "default"
      : state === "rejected" || state === "deleted"
        ? "destructive"
        : state === "pending_review"
          ? "secondary"
          : "outline"
  return <Badge variant={variant}>{managedTorrentStateLabel(state)}</Badge>
}

function PromotionBadge({
  promotion,
}: {
  promotion: ManagedTorrent["promotion"]
}) {
  const labels: Record<ManagedTorrent["promotion"], string> = {
    none: "普通",
    free: "免费",
    double_upload: "2X",
    double_upload_free: "2X免费",
    half_download: "50%",
    double_upload_half_download: "2X50%",
    thirty_percent_download: "30%",
  }
  return (
    <Badge variant={promotion === "none" ? "outline" : "secondary"}>
      {labels[promotion]}
    </Badge>
  )
}

function SwarmNumbers({ torrent }: { torrent: ManagedTorrent }) {
  return (
    <span className="inline-flex items-center justify-center gap-1 tabular-nums">
      <span className="text-success-foreground">{torrent.seeders}</span>
      <span className="text-muted-foreground">/</span>
      <span className="text-info">{torrent.leechers}</span>
      <span className="text-muted-foreground">/ {torrent.completed}</span>
    </span>
  )
}

function TorrentActions({
  torrent,
  canChangeAvailability,
  onChangeAvailability,
  canManagePurchase,
  onManagePurchase,
}: {
  torrent: ManagedTorrent
  canChangeAvailability: boolean
  onChangeAvailability: (torrent: ManagedTorrent) => void
  canManagePurchase: boolean
  onManagePurchase: (torrent: ManagedTorrent) => void
}) {
  const action = torrent.state === "published" || torrent.state === "disabled"
  return (
    <div className="flex items-center justify-center gap-1">
      {torrent.state === "published" ? (
        <Button
          variant="ghost"
          size="icon-xs"
          nativeButton={false}
          render={<Link to={`/torrents/${torrent.id}`} />}
          aria-label={`查看种子 ${torrent.id}`}
          title="查看种子"
        >
          <ArrowUpRightIcon />
        </Button>
      ) : torrent.state === "pending_review" ? (
        <Button
          variant="ghost"
          size="icon-xs"
          nativeButton={false}
          render={<Link to="/staff/content/torrent-reviews" />}
          aria-label={`审核种子 ${torrent.id}`}
          title="进入审核队列"
        >
          <ClipboardCheckIcon />
        </Button>
      ) : null}
      {action && canChangeAvailability ? (
        <Button
          variant="ghost"
          size="icon-xs"
          onClick={() => onChangeAvailability(torrent)}
          aria-label={`${torrent.state === "published" ? "下架" : "恢复"}种子 ${torrent.id}`}
          title={torrent.state === "published" ? "下架" : "恢复"}
        >
          {torrent.state === "published" ? <BanIcon /> : <RotateCcwIcon />}
        </Button>
      ) : null}
      {canManagePurchase && torrent.state !== "deleted" ? (
        <Button
          variant="ghost"
          size="icon-xs"
          onClick={() => onManagePurchase(torrent)}
          aria-label={`设置种子 ${torrent.id} 价格`}
          title="设置价格"
        >
          <CoinsIcon />
        </Button>
      ) : null}
    </div>
  )
}

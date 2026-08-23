import type { components } from "~/generated/api"
import {
  ArrowDownIcon,
  ArrowUpIcon,
  DownloadIcon,
  FileIcon,
  UploadIcon,
} from "lucide-react"

import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "~/components/ui/card"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "~/components/ui/table"
import {
  TorrentPromotion,
  TorrentSticky,
} from "~/features/torrent/components/torrent-promotion"
import { getTorrentSwarmFreshness } from "~/features/torrent/model/format"
import { TorrentSize } from "~/features/torrent/components/torrent-size"
import { TorrentBookmarkButton } from "~/features/torrent/components/torrent-bookmark-button"
import { TorrentDownloadButton } from "~/features/torrent/components/torrent-download-button"
import { TorrentCoverImage } from "~/features/torrent/components/torrent-cover-image"
import { TorrentTitleLink } from "~/features/torrent/components/torrent-title-link"
import type { TorrentBookmarkControls } from "~/features/torrent/hooks/use-torrent-bookmark-controls"
import type { TorrentSort } from "~/features/torrent/api/torrent.queries"
import { torrentCoverRequiresAdultConfirmation } from "~/features/torrent/model/adult-content"

type Torrent = components["schemas"]["TorrentSummary"]

export function TorrentTable({
  torrents,
  bookmarkControls,
  layout = "catalog",
  adultCoversVisible = false,
}: {
  torrents: Torrent[]
  bookmarkControls?: TorrentBookmarkControls
  timestampLabel?: string
  timestampByTorrentId?: ReadonlyMap<number, string>
  layout?: "catalog" | "bookmarks"
  sort?: TorrentSort
  onSortChange?: (sort: TorrentSort) => void
  adultCoversVisible?: boolean
}) {
  if (layout === "bookmarks") {
    return (
      <TorrentBookmarkTable
        torrents={torrents}
        bookmarkControls={bookmarkControls}
        adultCoversVisible={adultCoversVisible}
      />
    )
  }

  return (
    <Card className="gap-0 overflow-hidden rounded-lg border py-0 shadow-none ring-0">
      <CardHeader className="sr-only">
        <CardTitle>最新种子列表</CardTitle>
        <CardDescription>按上传时间从新到旧排列的种子资源。</CardDescription>
      </CardHeader>
      <CardContent className="px-0">
        <Table className="block min-w-0">
          <TableHeader className="sr-only">
            <TableRow>
              <TableHead>封面</TableHead>
              <TableHead>种子信息</TableHead>
              <TableHead>操作</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody className="block">
            {torrents.map((torrent) => {
              const swarmUnavailable =
                getTorrentSwarmFreshness(torrent) === "unavailable"

              return (
                <TableRow
                  key={torrent.id}
                  className="grid min-h-[108px] grid-cols-[64px_minmax(0,1fr)_64px] items-stretch gap-3 border-t! border-b-0! px-3 py-3 first:border-t-0! hover:bg-accent/45 sm:grid-cols-[64px_minmax(0,1fr)_72px] sm:px-4"
                >
                  <TableCell className="p-0">
                    <div className="relative flex h-[84px] w-16 items-center justify-center overflow-hidden rounded-[4px] bg-muted text-muted-foreground">
                      <TorrentCoverImage
                        torrentId={torrent.id}
                        title={torrent.name}
                        className="size-full object-cover"
                        fallbackClassName="bg-gradient-to-br from-neutral-100 via-neutral-200 to-neutral-300 text-neutral-500 dark:from-neutral-900 dark:via-neutral-800 dark:to-neutral-700 dark:text-neutral-400 [&_svg]:size-5"
                        obscured={
                          !adultCoversVisible &&
                          torrentCoverRequiresAdultConfirmation(
                            torrent.category
                          )
                        }
                        showObscuredLabel
                        obscuredLabel="18+"
                      />
                    </div>
                  </TableCell>
                  <TableCell className="min-w-0 p-0">
                    <div className="flex h-full min-w-0 flex-col py-0.5">
                      <div className="flex min-w-0 items-center gap-1.5">
                        <TorrentSticky
                          stickyUntil={torrent.sticky_until}
                          iconOnly
                          className="h-auto shrink-0 border-0 bg-transparent p-0 text-destructive shadow-none [&_svg]:size-3.5"
                        />
                        <TorrentTitleLink
                          torrentId={torrent.id}
                          title={torrent.name}
                          className="truncate text-sm leading-5 font-semibold hover:text-primary sm:text-[15px]"
                        />
                      </div>
                      {torrent.subtitle ? (
                        <p className="mt-0.5 truncate text-xs leading-5 text-muted-foreground">
                          {torrent.subtitle}
                        </p>
                      ) : null}
                      <div className="mt-0.5 flex min-h-5 flex-wrap items-center gap-1.5">
                        <TorrentPromotion
                          promotion={torrent.promotion}
                          className="h-[18px] rounded-[3px] px-1.5 text-[10px] font-medium"
                        />
                      </div>
                      <div className="mt-auto flex min-w-0 flex-wrap items-center gap-x-1.5 text-xs text-muted-foreground">
                        <span>{torrent.category.name}</span>
                        <span aria-hidden="true">·</span>
                        <span className="font-medium text-foreground">
                          <TorrentSize bytes={torrent.size_bytes} />
                        </span>
                        <span aria-hidden="true">·</span>
                        <span
                          className="inline-flex items-center gap-0.5 tabular-nums"
                          title={
                            swarmUnavailable ? "尚无 Tracker 统计" : undefined
                          }
                          aria-label={
                            swarmUnavailable
                              ? "尚无 Tracker 统计"
                              : `做种数 ${torrent.seeders.toLocaleString("zh-CN")}，下载数 ${torrent.leechers.toLocaleString("zh-CN")}`
                          }
                        >
                          <ArrowUpIcon className="size-3 text-success-foreground" />
                          <span className="text-success-foreground">
                            {swarmUnavailable
                              ? "—"
                              : torrent.seeders.toLocaleString("zh-CN")}
                          </span>
                          <ArrowDownIcon className="ml-0.5 size-3 text-destructive" />
                          <span className="text-destructive">
                            {swarmUnavailable
                              ? "—"
                              : torrent.leechers.toLocaleString("zh-CN")}
                          </span>
                        </span>
                      </div>
                    </div>
                  </TableCell>
                  <TableCell className="p-0">
                    <div className="flex h-full items-end justify-end gap-1 pb-0.5">
                      <TorrentDownloadButton
                        torrentId={torrent.id}
                        torrentName={torrent.name}
                        className="size-7 rounded border-0 p-1.5 text-muted-foreground hover:bg-primary/10 hover:text-primary [&_svg:not([class*='size-'])]:size-3.5"
                      />
                      <TorrentBookmarkButton
                        torrentId={torrent.id}
                        torrentName={torrent.name}
                        controls={bookmarkControls}
                        className="size-7 rounded border-0 p-1.5 text-muted-foreground hover:bg-warning/10 hover:text-warning-foreground [&_svg:not([class*='size-'])]:size-3.5"
                      />
                    </div>
                  </TableCell>
                </TableRow>
              )
            })}
          </TableBody>
        </Table>
      </CardContent>
    </Card>
  )
}

function TorrentBookmarkTable({
  torrents,
  bookmarkControls,
  adultCoversVisible,
}: {
  torrents: Torrent[]
  bookmarkControls?: TorrentBookmarkControls
  adultCoversVisible: boolean
}) {
  return (
    <Card className="hidden gap-0 rounded-lg border py-0 shadow-none ring-0 md:flex">
      <CardHeader className="sr-only">
        <CardTitle>我的收藏</CardTitle>
        <CardDescription>按最近收藏时间排列。</CardDescription>
      </CardHeader>
      <CardContent className="px-0">
        <Table className="block min-w-0">
          <TableHeader className="block bg-muted [&_tr]:border-b-0">
            <TableRow className="grid grid-cols-[48px_minmax(0,1fr)_100px_70px_70px_70px_110px_40px] items-center gap-3 px-4 py-3">
              <TableHead className="h-auto p-0">
                <span className="sr-only">封面</span>
              </TableHead>
              <TableHead className="h-auto p-0 text-muted-foreground">
                名称
              </TableHead>
              <TableHead className="h-auto p-0 text-muted-foreground">
                分类
              </TableHead>
              <TableHead className="h-auto p-0 text-right text-muted-foreground">
                做种
              </TableHead>
              <TableHead className="h-auto p-0 text-right text-muted-foreground">
                下载
              </TableHead>
              <TableHead className="h-auto p-0 text-right text-muted-foreground">
                完成
              </TableHead>
              <TableHead className="h-auto p-0 text-right text-muted-foreground">
                上传时间
              </TableHead>
              <TableHead className="h-auto p-0" />
            </TableRow>
          </TableHeader>
          <TableBody className="block">
            {torrents.map((torrent) => (
              <TableRow
                key={torrent.id}
                className="grid grid-cols-[48px_minmax(0,1fr)_100px_70px_70px_70px_110px_40px] items-center gap-3 border-t! border-b-0! px-4 py-2 hover:bg-accent/70"
              >
                <TableCell className="p-0">
                  <div className="flex h-8 w-12 items-center justify-center overflow-hidden rounded-sm bg-muted text-muted-foreground">
                    <TorrentCoverImage
                      torrentId={torrent.id}
                      title={torrent.name}
                      className="size-full object-cover"
                      fallbackClassName="bg-gradient-to-br from-neutral-100 via-neutral-200 to-neutral-300 text-neutral-500 dark:from-neutral-900 dark:via-neutral-800 dark:to-neutral-700 dark:text-neutral-400 [&_svg]:size-4"
                      obscured={
                        !adultCoversVisible &&
                        torrentCoverRequiresAdultConfirmation(torrent.category)
                      }
                    />
                  </div>
                </TableCell>
                <TableCell className="min-w-0 p-0">
                  <div className="min-w-0">
                    <div className="flex min-w-0 items-center gap-2">
                      <TorrentTitleLink
                        torrentId={torrent.id}
                        title={torrent.name}
                        className="truncate text-base font-normal"
                      />
                      <TorrentSticky stickyUntil={torrent.sticky_until} />
                      <TorrentPromotion promotion={torrent.promotion} />
                    </div>
                    <p className="truncate text-xs text-muted-foreground">
                      {torrent.subtitle}
                    </p>
                  </div>
                </TableCell>
                <TableCell className="p-0 text-sm text-muted-foreground">
                  {torrent.category.name}
                </TableCell>
                <TableCell className="flex items-center justify-end gap-1 p-0 text-right tabular-nums">
                  <UploadIcon className="size-3.5 text-success-foreground" />
                  <span>{torrent.seeders.toLocaleString("zh-CN")}</span>
                </TableCell>
                <TableCell className="flex items-center justify-end gap-1 p-0 text-right tabular-nums">
                  <DownloadIcon className="size-3.5 text-destructive" />
                  <span>{torrent.leechers.toLocaleString("zh-CN")}</span>
                </TableCell>
                <TableCell className="flex items-center justify-end gap-1 p-0 text-right tabular-nums">
                  <FileIcon className="size-3.5" />
                  <span>{torrent.completed.toLocaleString("zh-CN")}</span>
                </TableCell>
                <TableCell className="p-0 text-right text-xs text-muted-foreground">
                  <time dateTime={torrent.uploaded_at}>
                    {formatCompactDateTime(torrent.uploaded_at)}
                  </time>
                </TableCell>
                <TableCell className="p-0 text-center">
                  <TorrentBookmarkButton
                    torrentId={torrent.id}
                    torrentName={torrent.name}
                    controls={bookmarkControls}
                  />
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </CardContent>
    </Card>
  )
}

function formatCompactDateTime(value: string) {
  const timestamp = Date.parse(value)
  if (!Number.isFinite(timestamp)) return "时间未知"

  const parts = new Intl.DateTimeFormat("zh-CN", {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  }).formatToParts(timestamp)
  const part = (type: string) =>
    parts.find((item) => item.type === type)?.value ?? ""
  return `${part("year")}/${part("month")}/${part("day")} ${part("hour")}:${part("minute")}`
}

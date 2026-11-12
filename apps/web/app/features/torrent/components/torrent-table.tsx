import type { ReactNode } from "react"
import type { components } from "~/generated/api"
import {
  ArrowDownIcon,
  ArrowUpIcon,
  CircleCheckIcon,
  ChevronDownIcon,
  ChevronUpIcon,
} from "lucide-react"

import { Card } from "~/components/ui/card"
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
import { formatRelativeTime } from "~/features/torrent/model/format"
import { TorrentSize } from "~/features/torrent/components/torrent-size"
import { TorrentBookmarkButton } from "~/features/torrent/components/torrent-bookmark-button"
import { TorrentDownloadButton } from "~/features/torrent/components/torrent-download-button"
import {
  TorrentCoverImage,
  torrentCoverFallbackClass,
} from "~/features/torrent/components/torrent-cover-image"
import { TorrentCoverPreview } from "~/features/torrent/components/torrent-cover-preview"
import {
  TorrentDownloadProgress,
  type TorrentActivity,
} from "~/features/torrent/components/torrent-download-progress"
import { TorrentTitleLink } from "~/features/torrent/components/torrent-title-link"
import { TorrentSwarmNumbers } from "~/features/torrent/components/torrent-swarm-numbers"
import type { TorrentBookmarkControls } from "~/features/torrent/hooks/use-torrent-bookmark-controls"
import type { TorrentSort } from "~/features/torrent/api/torrent.queries"
import { formatDateTime } from "~/shared/formatters/date-time"
import { cn } from "~/lib/utils"

type Torrent = components["schemas"]["TorrentSummary"]

// Tablet and small-laptop widths drop the category column so the title keeps
// a readable share of the row; the category returns at `xl`.
const catalogGridClass =
  "grid grid-cols-[48px_minmax(0,1fr)_76px_120px_68px_52px] gap-x-2.5 xl:grid-cols-[56px_minmax(0,1fr)_72px_84px_148px_78px_52px]"
const bookmarkGridClass =
  "grid grid-cols-[48px_minmax(0,1fr)_64px_64px_64px_112px_40px] gap-x-3 xl:grid-cols-[56px_minmax(0,1fr)_88px_64px_64px_64px_120px_40px]"
const coverTriggerClass =
  "flex h-8 w-12 items-center justify-center overflow-hidden rounded-md border bg-muted text-faint"
const coverFallbackClass = cn(torrentCoverFallbackClass, "[&_svg]:size-4")
const rowActionClass =
  "size-6 rounded-md border-0 p-1 text-faint hover:bg-accent hover:text-foreground aria-pressed:text-warning-foreground [&_svg:not([class*='size-'])]:size-3.5"

export function TorrentTable({
  torrents,
  bookmarkControls,
  timestampLabel = "上传时间",
  timestampByTorrentId,
  layout = "catalog",
  sort,
  onSortChange,
  hiddenCoverCategoryIds,
  activityByTorrentId,
  footer,
}: {
  torrents: Torrent[]
  bookmarkControls?: TorrentBookmarkControls
  timestampLabel?: string
  timestampByTorrentId?: ReadonlyMap<number, string>
  layout?: "catalog" | "bookmarks"
  sort?: TorrentSort
  onSortChange?: (sort: TorrentSort) => void
  hiddenCoverCategoryIds?: ReadonlySet<string>
  activityByTorrentId?: ReadonlyMap<number, TorrentActivity>
  footer?: ReactNode
}) {
  if (layout === "bookmarks") {
    return (
      <TorrentBookmarkTable
        torrents={torrents}
        bookmarkControls={bookmarkControls}
        hiddenCoverCategoryIds={hiddenCoverCategoryIds}
        activityByTorrentId={activityByTorrentId}
        footer={footer}
      />
    )
  }

  return (
    <Card className="hidden gap-0 py-0 md:flex">
      <Table className="block min-w-0" aria-label="种子列表">
        <TableHeader className="block">
          <TableRow
            className={cn(
              catalogGridClass,
              "h-10 items-center rounded-none border-b! px-4 [&>th]:rounded-none!"
            )}
          >
            <TableHead className="h-auto p-0">
              <span className="sr-only">封面</span>
            </TableHead>
            <TableHead className="h-auto p-0">名称</TableHead>
            <TableHead className="hidden h-auto p-0 xl:block">分类</TableHead>
            <TableHead className="h-auto p-0 text-right">
              {onSortChange ? (
                <SortHeaderButton
                  active={sort === "size_desc" || sort === "size_asc"}
                  ascending={sort === "size_asc"}
                  onClick={() =>
                    onSortChange(
                      sort === "size_desc" ? "size_asc" : "size_desc"
                    )
                  }
                >
                  大小
                </SortHeaderButton>
              ) : (
                "大小"
              )}
            </TableHead>
            <TableHead className="h-auto p-0 text-center">
              <span
                className="flex items-center justify-center gap-1"
                aria-label="做种数 / 下载数 / 完成数"
              >
                <ArrowUpIcon className="size-3.5 text-success-foreground" />
                <span aria-hidden="true">/</span>
                <ArrowDownIcon className="size-3.5 text-destructive" />
                <span aria-hidden="true">/</span>
                {onSortChange ? (
                  <button
                    type="button"
                    onClick={() => onSortChange("completed_desc")}
                    className={cn(
                      "rounded-sm p-0.5 transition-colors hover:bg-accent hover:text-foreground",
                      sort === "completed_desc" && "bg-accent text-link"
                    )}
                    aria-label="按完成数排序"
                    aria-pressed={sort === "completed_desc"}
                  >
                    <CircleCheckIcon className="size-3.5" />
                  </button>
                ) : (
                  <CircleCheckIcon className="size-3.5" />
                )}
              </span>
            </TableHead>
            <TableHead className="h-auto p-0 text-right">
              {onSortChange ? (
                <SortHeaderButton
                  active={sort === "published_desc" || sort === "published_asc"}
                  ascending={sort === "published_asc"}
                  onClick={() =>
                    onSortChange(
                      sort === "published_desc"
                        ? "published_asc"
                        : "published_desc"
                    )
                  }
                >
                  {timestampLabel}
                </SortHeaderButton>
              ) : (
                timestampLabel
              )}
            </TableHead>
            <TableHead className="h-auto p-0 text-center">操作</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody className="block">
          {torrents.map((torrent) => {
            const timestamp =
              timestampByTorrentId?.get(torrent.id) ?? torrent.uploaded_at
            const obscured =
              hiddenCoverCategoryIds?.has(torrent.category.id) ?? false
            return (
              <TableRow
                key={torrent.id}
                className={cn(
                  catalogGridClass,
                  "relative h-14 items-center overflow-hidden px-4"
                )}
              >
                <TableCell className="p-0">
                  <TorrentCoverPreview
                    torrentId={torrent.id}
                    title={torrent.name}
                    triggerClassName={coverTriggerClass}
                    disabled={obscured}
                  >
                    <TorrentCoverImage
                      torrentId={torrent.id}
                      title={torrent.name}
                      className="size-full object-cover"
                      fallbackClassName={coverFallbackClass}
                      obscured={obscured}
                    />
                  </TorrentCoverPreview>
                </TableCell>
                <TableCell className="min-w-0 p-0">
                  <div className="flex min-w-0 items-center gap-1.5">
                    <TorrentTitleLink
                      torrentId={torrent.id}
                      title={torrent.name}
                      className="truncate"
                    />
                    <TorrentSticky stickyUntil={torrent.sticky_until} />
                    <TorrentPromotion promotion={torrent.promotion} />
                  </div>
                  {torrent.subtitle ? (
                    <p className="mt-0.5 truncate text-xs text-faint">
                      {torrent.subtitle}
                    </p>
                  ) : null}
                </TableCell>
                <TableCell className="hidden truncate p-0 text-muted-foreground xl:block">
                  {torrent.category.name}
                </TableCell>
                <TableCell className="p-0 text-right">
                  <TorrentSize bytes={torrent.size_bytes} />
                </TableCell>
                <TableCell className="p-0 text-center font-mono text-xs tabular-nums">
                  <TorrentSwarmNumbers torrent={torrent} />
                </TableCell>
                <TableCell className="p-0 text-right text-xs text-faint">
                  <time dateTime={timestamp} title={formatDateTime(timestamp)}>
                    {formatRelativeTime(timestamp)}
                  </time>
                </TableCell>
                <TableCell className="p-0">
                  <div className="flex items-center justify-center gap-1">
                    <TorrentDownloadButton
                      torrentId={torrent.id}
                      torrentName={torrent.name}
                      className={rowActionClass}
                    />
                    <TorrentBookmarkButton
                      torrentId={torrent.id}
                      torrentName={torrent.name}
                      controls={bookmarkControls}
                      className={rowActionClass}
                    />
                  </div>
                </TableCell>
                <RowDownloadProgress
                  activity={activityByTorrentId?.get(torrent.id)}
                />
              </TableRow>
            )
          })}
        </TableBody>
      </Table>
      {footer ? <div className="border-t px-4 py-2.5">{footer}</div> : null}
    </Card>
  )
}

// Absolutely positioned so the cell never claims a track in the row grid.
function RowDownloadProgress({
  activity,
}: {
  activity: TorrentActivity | undefined
}) {
  if (!activity) return null
  return (
    <td className="pointer-events-none absolute inset-x-0 bottom-0 h-1 p-0">
      <TorrentDownloadProgress activity={activity} overlay />
    </td>
  )
}

function SortHeaderButton({
  active,
  ascending,
  onClick,
  children,
}: {
  active: boolean
  ascending: boolean
  onClick: () => void
  children: ReactNode
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        "group ml-auto flex cursor-pointer items-center gap-0.5 rounded-sm transition-colors hover:text-foreground",
        active && "text-foreground"
      )}
    >
      {children}
      {active && ascending ? (
        <ChevronUpIcon className="size-3" />
      ) : (
        <ChevronDownIcon
          className={cn(
            "size-3",
            !active && "opacity-0 group-hover:opacity-60"
          )}
        />
      )}
    </button>
  )
}

function TorrentBookmarkTable({
  torrents,
  bookmarkControls,
  hiddenCoverCategoryIds,
  activityByTorrentId,
  footer,
}: {
  torrents: Torrent[]
  bookmarkControls?: TorrentBookmarkControls
  hiddenCoverCategoryIds?: ReadonlySet<string>
  activityByTorrentId?: ReadonlyMap<number, TorrentActivity>
  footer?: ReactNode
}) {
  return (
    <Card className="hidden gap-0 py-0 md:flex">
      <Table className="block min-w-0" aria-label="我的收藏">
        <TableHeader className="block">
          <TableRow
            className={cn(
              bookmarkGridClass,
              "h-10 items-center rounded-none border-b! px-4 [&>th]:rounded-none!"
            )}
          >
            <TableHead className="h-auto p-0">
              <span className="sr-only">封面</span>
            </TableHead>
            <TableHead className="h-auto p-0">名称</TableHead>
            <TableHead className="hidden h-auto p-0 xl:block">分类</TableHead>
            <TableHead className="h-auto p-0 text-right">做种</TableHead>
            <TableHead className="h-auto p-0 text-right">下载</TableHead>
            <TableHead className="h-auto p-0 text-right">完成</TableHead>
            <TableHead className="h-auto p-0 text-right">上传时间</TableHead>
            <TableHead className="h-auto p-0">
              <span className="sr-only">操作</span>
            </TableHead>
          </TableRow>
        </TableHeader>
        <TableBody className="block">
          {torrents.map((torrent) => {
            const obscured =
              hiddenCoverCategoryIds?.has(torrent.category.id) ?? false
            return (
              <TableRow
                key={torrent.id}
                className={cn(
                  bookmarkGridClass,
                  "relative h-14 items-center overflow-hidden px-4"
                )}
              >
                <TableCell className="p-0">
                  <TorrentCoverPreview
                    torrentId={torrent.id}
                    title={torrent.name}
                    triggerClassName={coverTriggerClass}
                    disabled={obscured}
                  >
                    <TorrentCoverImage
                      torrentId={torrent.id}
                      title={torrent.name}
                      className="size-full object-cover"
                      fallbackClassName={coverFallbackClass}
                      obscured={obscured}
                    />
                  </TorrentCoverPreview>
                </TableCell>
                <TableCell className="min-w-0 p-0">
                  <div className="flex min-w-0 items-center gap-1.5">
                    <TorrentTitleLink
                      torrentId={torrent.id}
                      title={torrent.name}
                      className="truncate"
                    />
                    <TorrentSticky stickyUntil={torrent.sticky_until} />
                    <TorrentPromotion promotion={torrent.promotion} />
                  </div>
                  {torrent.subtitle ? (
                    <p className="mt-0.5 truncate text-xs text-faint">
                      {torrent.subtitle}
                    </p>
                  ) : null}
                </TableCell>
                <TableCell className="hidden truncate p-0 text-muted-foreground xl:block">
                  {torrent.category.name}
                </TableCell>
                <TableCell className="p-0 text-right font-mono text-xs text-success-foreground tabular-nums">
                  {torrent.seeders.toLocaleString("zh-CN")}
                </TableCell>
                <TableCell className="p-0 text-right font-mono text-xs text-destructive tabular-nums">
                  {torrent.leechers.toLocaleString("zh-CN")}
                </TableCell>
                <TableCell className="p-0 text-right font-mono text-xs tabular-nums">
                  {torrent.completed.toLocaleString("zh-CN")}
                </TableCell>
                <TableCell className="p-0 text-right font-mono text-xs text-faint tabular-nums">
                  <time dateTime={torrent.uploaded_at}>
                    {formatCompactDateTime(torrent.uploaded_at)}
                  </time>
                </TableCell>
                <TableCell className="p-0 text-center">
                  <TorrentBookmarkButton
                    torrentId={torrent.id}
                    torrentName={torrent.name}
                    controls={bookmarkControls}
                    className={rowActionClass}
                  />
                </TableCell>
                <RowDownloadProgress
                  activity={activityByTorrentId?.get(torrent.id)}
                />
              </TableRow>
            )
          })}
        </TableBody>
      </Table>
      {footer ? <div className="border-t px-4 py-2.5">{footer}</div> : null}
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

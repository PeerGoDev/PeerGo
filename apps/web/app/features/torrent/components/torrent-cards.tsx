import * as React from "react"
import type { components } from "~/generated/api"
import { Link } from "react-router"
import { ArrowDownIcon, ArrowUpIcon } from "lucide-react"

import { Card } from "~/components/ui/card"
import {
  TorrentPromotion,
  TorrentSticky,
} from "~/features/torrent/components/torrent-promotion"
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
import { isTorrentId } from "~/features/torrent/api/torrent.download"
import type { TorrentBookmarkControls } from "~/features/torrent/hooks/use-torrent-bookmark-controls"
import {
  formatRelativeTime,
  getTorrentSwarmFreshness,
} from "~/features/torrent/model/format"
import { torrentCoverRequiresAdultConfirmation } from "~/features/torrent/model/adult-content"
import type { TorrentView } from "~/features/torrent/model/torrent-view"
import { cn } from "~/lib/utils"
import { formatDateTime } from "~/shared/formatters/date-time"
import { formatBytes } from "~/shared/formatters/bytes"

type Torrent = components["schemas"]["TorrentSummary"]

type TorrentCardsProps = {
  torrents: Torrent[]
  view: TorrentView
  bookmarkControls?: TorrentBookmarkControls
  timestampByTorrentId?: ReadonlyMap<number, string>
  adultCoversVisible?: boolean
  activityByTorrentId?: ReadonlyMap<number, TorrentActivity>
}

export function TorrentCards({
  torrents,
  view,
  bookmarkControls,
  timestampByTorrentId,
  adultCoversVisible = false,
  activityByTorrentId,
}: TorrentCardsProps) {
  if (view === "poster") {
    return (
      <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 sm:gap-4 md:grid-cols-4 xl:grid-cols-5">
        {torrents.map((torrent) => (
          <TorrentPosterCard
            key={torrent.id}
            torrent={torrent}
            fixedAspect
            timestamp={
              timestampByTorrentId?.get(torrent.id) ?? torrent.uploaded_at
            }
            adultCoversVisible={adultCoversVisible}
            activity={activityByTorrentId?.get(torrent.id)}
          />
        ))}
      </div>
    )
  }

  if (view === "waterfall") {
    return (
      <TorrentWaterfall
        torrents={torrents}
        timestampByTorrentId={timestampByTorrentId}
        adultCoversVisible={adultCoversVisible}
        activityByTorrentId={activityByTorrentId}
      />
    )
  }

  return (
    <Card className="gap-0 py-0 md:hidden">
      {torrents.map((torrent) => {
        const timestamp =
          timestampByTorrentId?.get(torrent.id) ?? torrent.uploaded_at
        const swarmUnavailable =
          getTorrentSwarmFreshness(torrent) === "unavailable"
        const obscured =
          !adultCoversVisible &&
          torrentCoverRequiresAdultConfirmation(torrent.category)

        return (
          <article
            key={torrent.id}
            className="relative flex min-w-0 gap-3 border-t p-3 first:border-t-0"
          >
            <TorrentCoverPreview
              torrentId={torrent.id}
              title={torrent.name}
              triggerClassName="relative flex h-[4.5rem] w-12 shrink-0 items-center justify-center overflow-hidden rounded-md border bg-muted text-faint"
              disabled={obscured}
            >
              <TorrentCoverImage
                torrentId={torrent.id}
                title={torrent.name}
                className="size-full object-cover"
                fallbackClassName={cn(
                  torrentCoverFallbackClass,
                  "[&_svg]:size-5"
                )}
                obscured={obscured}
              />
            </TorrentCoverPreview>
            <div className="flex min-w-0 flex-1 flex-col gap-1">
              <h3 className="min-w-0 text-sm leading-snug">
                <TorrentTitleLink
                  torrentId={torrent.id}
                  title={torrent.name}
                  className="line-clamp-2"
                />
              </h3>
              {torrent.subtitle ? (
                <p className="line-clamp-1 text-xs text-faint">
                  {torrent.subtitle}
                </p>
              ) : null}
              <div className="flex min-w-0 flex-wrap items-center gap-1.5">
                <TorrentSticky stickyUntil={torrent.sticky_until} />
                <TorrentPromotion promotion={torrent.promotion} />
                <time
                  dateTime={timestamp}
                  title={formatDateTime(timestamp)}
                  className="ml-auto text-xs text-faint"
                >
                  {formatRelativeTime(timestamp)}
                </time>
              </div>
              <div className="mt-auto flex min-w-0 items-end justify-between gap-2 text-xs">
                <div className="flex min-w-0 flex-wrap items-center gap-x-1.5 gap-y-1 text-faint">
                  <span className="text-muted-foreground">
                    {torrent.category.name}
                  </span>
                  <span aria-hidden="true">·</span>
                  <span className="font-mono text-foreground tabular-nums">
                    {formatBytes(torrent.size_bytes)}
                  </span>
                  <span aria-hidden="true">·</span>
                  <SwarmCompact
                    torrent={torrent}
                    unavailable={swarmUnavailable}
                  />
                </div>
                <span className="-mr-1 -mb-1 flex shrink-0 items-center">
                  <TorrentDownloadButton
                    torrentId={torrent.id}
                    torrentName={torrent.name}
                    className="size-8 text-muted-foreground"
                  />
                  <TorrentBookmarkButton
                    torrentId={torrent.id}
                    torrentName={torrent.name}
                    controls={bookmarkControls}
                    className="size-8 text-muted-foreground aria-pressed:text-warning-foreground"
                  />
                </span>
              </div>
            </div>
            <TorrentDownloadProgress
              activity={activityByTorrentId?.get(torrent.id)}
              overlay
            />
          </article>
        )
      })}
    </Card>
  )
}

function SwarmCompact({
  torrent,
  unavailable,
}: {
  torrent: Torrent
  unavailable: boolean
}) {
  return (
    <span
      className="flex items-center gap-1 font-mono tabular-nums"
      title={unavailable ? "尚无 Tracker 统计" : undefined}
      aria-label={
        unavailable
          ? "尚无 Tracker 统计"
          : `做种数 ${torrent.seeders.toLocaleString("zh-CN")}，下载数 ${torrent.leechers.toLocaleString("zh-CN")}`
      }
    >
      <ArrowUpIcon className="size-3 text-success-foreground" />
      <span className="text-success-foreground">
        {unavailable ? "—" : torrent.seeders.toLocaleString("zh-CN")}
      </span>
      <ArrowDownIcon className="ml-0.5 size-3 text-destructive" />
      <span className="text-destructive">
        {unavailable ? "—" : torrent.leechers.toLocaleString("zh-CN")}
      </span>
    </span>
  )
}

function TorrentPosterCard({
  torrent,
  timestamp,
  fixedAspect,
  adultCoversVisible,
  activity,
}: {
  torrent: Torrent
  timestamp: string
  fixedAspect: boolean
  adultCoversVisible: boolean
  activity: TorrentActivity | undefined
}) {
  const obscured =
    !adultCoversVisible &&
    torrentCoverRequiresAdultConfirmation(torrent.category)
  const swarmUnavailable = getTorrentSwarmFreshness(torrent) === "unavailable"
  const content = (
    <>
      <div
        className={cn(
          "relative overflow-hidden border-b bg-muted",
          fixedAspect && "flex aspect-[2/3] items-center justify-center"
        )}
      >
        <TorrentCoverImage
          torrentId={torrent.id}
          title={torrent.name}
          className={cn(
            "transition-transform duration-300 group-hover:scale-[1.03]",
            fixedAspect
              ? "absolute inset-0 size-full object-contain"
              : "block h-auto w-full"
          )}
          fallbackClassName={cn(
            torrentCoverFallbackClass,
            "[&_svg]:size-7",
            !fixedAspect && "aspect-[3/4]"
          )}
          blurredBackground={fixedAspect}
          obscured={obscured}
          showObscuredLabel
        />
        {torrent.sticky_until || torrent.promotion !== "none" ? (
          <div className="pointer-events-none absolute top-2 left-2 flex flex-wrap gap-1">
            <TorrentSticky
              stickyUntil={torrent.sticky_until}
              className="bg-card/90 shadow-xs backdrop-blur-sm"
            />
            <TorrentPromotion
              promotion={torrent.promotion}
              className="bg-card/90 shadow-xs backdrop-blur-sm"
            />
          </div>
        ) : null}
        <TorrentDownloadProgress activity={activity} overlay />
      </div>
      <div className="flex flex-1 flex-col gap-1.5 px-3 pt-2.5 pb-3">
        <h3
          className="line-clamp-2 text-sm leading-snug font-medium text-title transition-colors group-hover:text-title-hover"
          title={torrent.name}
        >
          {torrent.name}
        </h3>
        <div className="mt-auto flex items-center justify-between gap-2 text-2xs text-faint">
          <span className="min-w-0 truncate">
            {torrent.category.name} ·{" "}
            <span className="font-mono tabular-nums">
              {formatBytes(torrent.size_bytes)}
            </span>
          </span>
          <span
            className="flex shrink-0 items-center gap-1.5 font-mono tabular-nums"
            title={swarmUnavailable ? "尚无 Tracker 统计" : undefined}
          >
            <span className="text-success-foreground">
              ↑{swarmUnavailable ? "—" : torrent.seeders}
            </span>
            <span className="text-destructive">
              ↓{swarmUnavailable ? "—" : torrent.leechers}
            </span>
          </span>
        </div>
        <time
          dateTime={timestamp}
          title={formatDateTime(timestamp)}
          className="sr-only"
        >
          {formatRelativeTime(timestamp)}
        </time>
      </div>
    </>
  )

  const cardClassName =
    "group flex h-full flex-col overflow-hidden rounded-xl border bg-card text-card-foreground shadow-card transition-[border-color,box-shadow] hover:border-input"

  return (
    <article className="min-w-0">
      {isTorrentId(torrent.id) ? (
        <Link
          to={`/torrents/${torrent.id}`}
          target="_blank"
          rel="noreferrer"
          className={cn(
            cardClassName,
            "outline-none focus-visible:ring-3 focus-visible:ring-ring/30"
          )}
        >
          {content}
        </Link>
      ) : (
        <div className={cardClassName}>{content}</div>
      )}
    </article>
  )
}

function TorrentWaterfall({
  torrents,
  timestampByTorrentId,
  adultCoversVisible,
  activityByTorrentId,
}: Omit<TorrentCardsProps, "view" | "bookmarkControls">) {
  const containerRef = React.useRef<HTMLDivElement>(null)
  const columnCount = useWaterfallColumnCount(containerRef)
  // Round-robin keeps the newest torrents across the first visual row instead
  // of filling the first column top to bottom like CSS multi-column layout.
  const columns = Array.from({ length: columnCount }, (_, column) =>
    torrents.filter((_, index) => index % columnCount === column)
  )

  return (
    <div
      ref={containerRef}
      className="grid items-start gap-3 sm:gap-4"
      style={{ gridTemplateColumns: `repeat(${columnCount}, minmax(0, 1fr))` }}
    >
      {columns.map((items, column) => (
        <div key={column} className="flex min-w-0 flex-col gap-3 sm:gap-4">
          {items.map((torrent) => (
            <TorrentPosterCard
              key={torrent.id}
              torrent={torrent}
              fixedAspect={false}
              timestamp={
                timestampByTorrentId?.get(torrent.id) ?? torrent.uploaded_at
              }
              adultCoversVisible={adultCoversVisible ?? false}
              activity={activityByTorrentId?.get(torrent.id)}
            />
          ))}
        </div>
      ))}
    </div>
  )
}

function waterfallColumnsForWidth(width: number) {
  if (width >= 880) return 4
  if (width >= 600) return 3
  return 2
}

function useWaterfallColumnCount(
  containerRef: React.RefObject<HTMLDivElement | null>
) {
  const [columnCount, setColumnCount] = React.useState(() =>
    waterfallColumnsForWidth(
      typeof window === "undefined" ? 1024 : window.innerWidth - 64
    )
  )

  React.useEffect(() => {
    const element = containerRef.current
    if (!element || typeof ResizeObserver === "undefined") return
    const observer = new ResizeObserver(([entry]) => {
      if (!entry) return
      setColumnCount(waterfallColumnsForWidth(entry.contentRect.width))
    })
    observer.observe(element)
    return () => observer.disconnect()
  }, [containerRef])

  return columnCount
}

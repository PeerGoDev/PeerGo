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
import { TorrentCoverImage } from "~/features/torrent/components/torrent-cover-image"
import { TorrentCoverPreview } from "~/features/torrent/components/torrent-cover-preview"
import {
  TorrentDownloadProgress,
  type TorrentActivity,
} from "~/features/torrent/components/torrent-download-progress"
import { TorrentSize } from "~/features/torrent/components/torrent-size"
import { TorrentTitleLink } from "~/features/torrent/components/torrent-title-link"
import { isTorrentId } from "~/features/torrent/api/torrent.download"
import type { TorrentBookmarkControls } from "~/features/torrent/hooks/use-torrent-bookmark-controls"
import {
  formatRelativeTime,
  getTorrentSwarmFreshness,
} from "~/features/torrent/model/format"
import { torrentCoverRequiresAdultConfirmation } from "~/features/torrent/model/adult-content"
import { formatDateTime } from "~/shared/formatters/date-time"
import { formatBytes } from "~/shared/formatters/bytes"

type Torrent = components["schemas"]["TorrentSummary"]

export function TorrentCards({
  torrents,
  poster,
  bookmarkControls,
  timestampByTorrentId,
  adultCoversVisible = false,
  activityByTorrentId,
}: {
  torrents: Torrent[]
  poster: boolean
  bookmarkControls?: TorrentBookmarkControls
  timestampByTorrentId?: ReadonlyMap<number, string>
  adultCoversVisible?: boolean
  activityByTorrentId?: ReadonlyMap<number, TorrentActivity>
}) {
  if (poster) {
    return (
      <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 sm:gap-4 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6">
        {torrents.map((torrent) => {
          const timestamp =
            timestampByTorrentId?.get(torrent.id) ?? torrent.uploaded_at
          const swarmUnavailable =
            getTorrentSwarmFreshness(torrent) === "unavailable"
          const posterContent = (
            <>
              <TorrentCoverPreview
                torrentId={torrent.id}
                title={torrent.name}
                triggerClassName="relative flex aspect-[2/3] items-center justify-center overflow-hidden rounded-lg bg-muted shadow-sm transition-shadow group-hover:shadow-lg"
                disabled={
                  !adultCoversVisible &&
                  torrentCoverRequiresAdultConfirmation(torrent.category)
                }
              >
                <TorrentCoverImage
                  torrentId={torrent.id}
                  title={torrent.name}
                  className="absolute inset-0 size-full object-contain transition-transform duration-300 group-hover:scale-105"
                  fallbackClassName="bg-gradient-to-br from-neutral-200 via-neutral-300 to-neutral-500 text-white/75 dark:from-neutral-800 dark:via-neutral-700 dark:to-neutral-600 dark:text-white/65 [&_svg]:drop-shadow-sm"
                  blurredBackground
                  obscured={
                    !adultCoversVisible &&
                    torrentCoverRequiresAdultConfirmation(torrent.category)
                  }
                  showObscuredLabel
                />

                <div className="pointer-events-none absolute top-1.5 right-1.5 flex flex-col items-end gap-1">
                  <TorrentSticky
                    stickyUntil={torrent.sticky_until}
                    className="h-auto rounded px-1.5 py-0.5 text-[10px] font-bold shadow-md"
                  />
                  <TorrentPromotion
                    promotion={torrent.promotion}
                    className="h-auto rounded px-1.5 py-0.5 text-[10px] font-bold shadow-md"
                  />
                </div>

                <div className="absolute inset-x-0 bottom-0 bg-gradient-to-t from-black/85 via-black/50 to-transparent px-2 pt-6 pb-2 text-white">
                  <div className="flex items-center justify-between text-[11px]">
                    <TorrentSize bytes={torrent.size_bytes} />
                    <span
                      className="flex shrink-0 items-center gap-1 tabular-nums"
                      title={swarmUnavailable ? "尚无 Tracker 统计" : undefined}
                    >
                      <ArrowUpIcon className="size-2.5 text-success" />
                      <span className="font-medium text-success">
                        {swarmUnavailable ? "—" : torrent.seeders}
                      </span>
                      <ArrowDownIcon className="ml-1 size-2.5 text-destructive" />
                      <span className="font-medium text-destructive">
                        {swarmUnavailable ? "—" : torrent.leechers}
                      </span>
                    </span>
                  </div>
                </div>
                <TorrentDownloadProgress
                  activity={activityByTorrentId?.get(torrent.id)}
                  overlay
                />
              </TorrentCoverPreview>

              <div className="mt-2 px-0.5">
                <h3
                  className="line-clamp-2 text-sm leading-snug font-medium transition-colors group-hover:text-primary"
                  title={torrent.name}
                >
                  {torrent.name}
                </h3>
                <div className="mt-1 flex items-center gap-2 text-xs">
                  <time
                    dateTime={timestamp}
                    title={formatDateTime(timestamp)}
                    className="ml-auto text-muted-foreground"
                  >
                    {formatRelativeTime(timestamp)}
                  </time>
                </div>
              </div>
            </>
          )

          return (
            <article key={torrent.id} className="min-w-0">
              {isTorrentId(torrent.id) ? (
                <Link
                  to={`/torrents/${torrent.id}`}
                  target="_blank"
                  rel="noreferrer"
                  className="group block rounded-lg outline-none focus-visible:ring-2 focus-visible:ring-ring"
                >
                  {posterContent}
                </Link>
              ) : (
                <div className="block rounded-lg">{posterContent}</div>
              )}
            </article>
          )
        })}
      </div>
    )
  }

  return (
    <Card className="grid grid-cols-1 gap-0 py-0 shadow-none md:hidden">
      {torrents.map((torrent) => {
        const timestamp =
          timestampByTorrentId?.get(torrent.id) ?? torrent.uploaded_at
        const swarmUnavailable =
          getTorrentSwarmFreshness(torrent) === "unavailable"

        return (
          <article
            key={torrent.id}
            className="relative flex min-w-0 gap-3 overflow-hidden border-t p-3"
          >
            <TorrentCoverPreview
              torrentId={torrent.id}
              title={torrent.name}
              triggerClassName="relative flex h-24 w-16 shrink-0 items-center justify-center overflow-hidden rounded-sm bg-muted text-muted-foreground"
              disabled={
                !adultCoversVisible &&
                torrentCoverRequiresAdultConfirmation(torrent.category)
              }
            >
              <TorrentCoverImage
                torrentId={torrent.id}
                title={torrent.name}
                className="size-full object-cover"
                fallbackClassName="bg-gradient-to-br from-neutral-100 via-neutral-200 to-neutral-300 text-neutral-500 dark:from-neutral-900 dark:via-neutral-800 dark:to-neutral-700 dark:text-neutral-400 [&_svg]:size-5"
                obscured={
                  !adultCoversVisible &&
                  torrentCoverRequiresAdultConfirmation(torrent.category)
                }
                showObscuredLabel
              />
            </TorrentCoverPreview>
            <div className="flex min-w-0 flex-1 flex-col justify-between">
              <div className="flex min-w-0 items-start gap-1.5">
                <h3 className="min-w-0 flex-1 text-sm leading-snug font-medium">
                  <TorrentTitleLink
                    torrentId={torrent.id}
                    title={torrent.name}
                    className="line-clamp-2"
                  />
                </h3>
              </div>
              <p className="mt-0.5 line-clamp-1 text-xs text-muted-foreground">
                {torrent.subtitle}
              </p>
              <div className="mt-1 flex flex-wrap items-center gap-2">
                <TorrentSticky
                  stickyUntil={torrent.sticky_until}
                  className="h-auto rounded px-1.5 py-0.5 text-[10px]"
                />
                <TorrentPromotion
                  promotion={torrent.promotion}
                  className="h-auto rounded px-1.5 py-0.5 text-[10px]"
                />
              </div>
              <div className="mt-auto flex min-w-0 items-end justify-between gap-2 text-xs">
                <div className="flex min-w-0 flex-wrap items-center gap-x-1.5 gap-y-1 text-muted-foreground">
                  <span>{torrent.category.name}</span>
                  <span aria-hidden="true">·</span>
                  <span className="text-success-foreground tabular-nums">
                    {formatBytes(torrent.size_bytes)}
                  </span>
                  <span aria-hidden="true">·</span>
                  <span
                    className="flex items-center gap-1 tabular-nums"
                    title={swarmUnavailable ? "尚无 Tracker 统计" : undefined}
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
                <span className="flex shrink-0 items-center gap-1">
                  <TorrentDownloadButton
                    torrentId={torrent.id}
                    torrentName={torrent.name}
                  />
                  <TorrentBookmarkButton
                    torrentId={torrent.id}
                    torrentName={torrent.name}
                    controls={bookmarkControls}
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

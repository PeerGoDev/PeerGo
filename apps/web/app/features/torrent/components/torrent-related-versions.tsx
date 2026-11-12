import * as React from "react"
import { Link } from "react-router"
import {
  DownloadIcon,
  FileTextIcon,
  LayersIcon,
  UploadIcon,
} from "lucide-react"

import { Badge } from "~/components/ui/badge"
import { Button, buttonVariants } from "~/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "~/components/ui/card"
import { Skeleton } from "~/components/ui/skeleton"
import { useTorrentRelatedVersions } from "~/features/torrent/api/torrent.queries"
import { TorrentPromotion } from "~/features/torrent/components/torrent-promotion"
import {
  formatTorrentSize,
  getTorrentSwarmFreshness,
} from "~/features/torrent/model/format"
import { formatCompactDateTime } from "~/shared/formatters/date-time"

export function TorrentRelatedVersions({ torrentId }: { torrentId: number }) {
  const [expanded, setExpanded] = React.useState(false)
  const related = useTorrentRelatedVersions(torrentId)

  if (related.isPending) {
    return (
      <Card size="sm" aria-label="正在加载其它版本" aria-busy="true">
        <CardHeader>
          <CardTitle>
            <Skeleton className="h-5 w-28" />
          </CardTitle>
        </CardHeader>
        <CardContent>
          <Skeleton className="h-16 w-full" />
        </CardContent>
      </Card>
    )
  }
  if (related.isError || !related.data?.items.length) {
    return null
  }

  const visibleVersions = expanded
    ? related.data.items
    : related.data.items.slice(0, 3)

  return (
    <Card className="gap-0 py-0">
      <CardHeader className="p-(--card-spacing) pb-2">
        <CardTitle className="flex items-center gap-2">
          <LayersIcon className="size-4" />
          其它版本 ({related.data.items.length.toLocaleString("zh-CN")})
        </CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-2.5 px-(--card-spacing) pb-(--card-spacing)">
        {visibleVersions.map((torrent) => (
          <div
            key={torrent.id}
            className="flex items-center justify-between gap-4 rounded-lg border px-3.5 py-3 transition-colors hover:border-input hover:bg-accent/50"
          >
            <div className="min-w-0 flex-1">
              <Link
                to={`/torrents/${torrent.id}`}
                title={torrent.name}
                className="block truncate font-medium text-title transition-colors hover:text-title-hover"
              >
                {torrent.name}
              </Link>
              {torrent.subtitle ? (
                <p className="mt-0.5 truncate text-xs text-faint">
                  {torrent.subtitle}
                </p>
              ) : null}
              <div className="mt-2 flex flex-wrap items-center gap-x-3.5 gap-y-1.5 text-xs text-faint">
                <span className="flex items-center gap-1 font-mono text-foreground tabular-nums">
                  <FileTextIcon className="size-3.5 text-faint" />
                  {formatTorrentSize(torrent.size_bytes)}
                </span>
                <Badge variant="secondary">{torrent.category.name}</Badge>
                <TorrentPromotion promotion={torrent.promotion} />
                <span className="flex items-center gap-1 font-mono text-success-foreground tabular-nums">
                  <UploadIcon className="size-3.5" />
                  {swarmValue(torrent, torrent.seeders)}
                </span>
                <span className="flex items-center gap-1 font-mono text-destructive tabular-nums">
                  <DownloadIcon className="size-3.5" />
                  {swarmValue(torrent, torrent.leechers)}
                </span>
                <span className="font-mono tabular-nums">
                  {formatCompactDateTime(torrent.uploaded_at)}
                </span>
              </div>
            </div>
            <Link
              to={`/torrents/${torrent.id}`}
              className={buttonVariants({
                variant: "outline",
                size: "xs",
                className: "h-7 shrink-0 px-2.5",
              })}
            >
              查看
            </Link>
          </div>
        ))}
        {related.data.items.length > 3 ? (
          <Button
            type="button"
            variant="ghost"
            size="sm"
            className="w-full text-xs font-normal text-faint hover:text-foreground"
            onClick={() => setExpanded((current) => !current)}
          >
            {expanded
              ? "收起其它版本 ▲"
              : `展开更多 (${related.data.items.length - 3}) ▼`}
          </Button>
        ) : null}
      </CardContent>
    </Card>
  )
}

function swarmValue(
  torrent: Parameters<typeof getTorrentSwarmFreshness>[0],
  value: number
) {
  return getTorrentSwarmFreshness(torrent) === "unavailable" ? "—" : value
}

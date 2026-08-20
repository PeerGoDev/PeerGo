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
    <Card className="gap-0 rounded-lg py-0 shadow-sm">
      <CardHeader className="p-6 pb-2">
        <CardTitle className="flex items-center gap-2 text-2xl leading-none font-semibold">
          <LayersIcon className="size-5" />
          其它版本 ({related.data.items.length.toLocaleString("zh-CN")})
        </CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-3 px-6 pb-6">
        {visibleVersions.map((torrent) => (
          <div
            key={torrent.id}
            className="flex items-start justify-between gap-4 rounded-lg border p-4 transition-colors hover:bg-muted/50"
          >
            <div className="min-w-0 flex-1">
              <Link
                to={`/torrents/${torrent.id}`}
                title={torrent.name}
                className="block truncate font-medium text-primary hover:underline"
              >
                {torrent.name}
              </Link>
              {torrent.subtitle ? (
                <p className="mt-2 truncate text-sm text-muted-foreground">
                  {torrent.subtitle}
                </p>
              ) : null}
              <div className="mt-2 flex flex-wrap items-center gap-x-4 gap-y-1 text-sm text-muted-foreground">
                <span className="flex items-center gap-1">
                  <FileTextIcon className="size-3.5" />
                  {formatTorrentSize(torrent.size_bytes)}
                </span>
                <Badge variant="secondary">{torrent.category.name}</Badge>
                {torrent.promotion === "free" ? (
                  <Badge variant="destructive">免费</Badge>
                ) : null}
                <span className="flex items-center gap-1 text-success-foreground">
                  <UploadIcon className="size-3.5" />
                  {swarmValue(torrent, torrent.seeders)}
                </span>
                <span className="flex items-center gap-1 text-primary">
                  <DownloadIcon className="size-3.5" />
                  {swarmValue(torrent, torrent.leechers)}
                </span>
                <span className="text-xs">
                  {formatCompactDateTime(torrent.uploaded_at)}
                </span>
              </div>
            </div>
            <Link
              to={`/torrents/${torrent.id}`}
              className={buttonVariants({ variant: "outline", size: "sm" })}
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
            className="w-full text-muted-foreground"
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

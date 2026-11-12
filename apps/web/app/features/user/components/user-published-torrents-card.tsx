import { Link } from "react-router"
import { FolderUpIcon } from "lucide-react"

import { Badge } from "~/components/ui/badge"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "~/components/ui/card"
import type { components } from "~/generated/api"
import { formatBytes } from "~/shared/formatters/bytes"
import { formatCompactDateTime } from "~/shared/formatters/date-time"

type PublishedTorrent = components["schemas"]["PublicUserPublishedTorrent"]

export function UserPublishedTorrentsCard({
  items,
  total,
}: {
  items: PublishedTorrent[]
  total: number
}) {
  return (
    <Card className="gap-0 py-0">
      <CardHeader className="px-(--card-spacing) pt-(--card-spacing) pb-3">
        <CardTitle>
          <h2 className="flex items-center gap-2">
            <FolderUpIcon className="size-4 text-primary" />
            已发布种子
          </h2>
        </CardTitle>
        <CardDescription>
          公开、非匿名发布共 {total.toLocaleString("zh-CN")}{" "}
          个；以下为最近发布。
        </CardDescription>
      </CardHeader>
      <CardContent className="px-(--card-spacing) pb-(--card-spacing)">
        {items.length === 0 ? (
          <p className="py-5 text-sm text-muted-foreground">
            暂无公开发布记录。
          </p>
        ) : (
          <div className="-mx-3 divide-y">
            {items.map((item) => (
              <Link
                key={item.id}
                to={`/torrents/${item.id}`}
                className="group grid gap-1.5 rounded-md px-3 py-2.5 transition-colors hover:bg-accent sm:grid-cols-[minmax(0,1fr)_auto] sm:items-center sm:gap-3"
              >
                <div className="min-w-0">
                  <p className="truncate font-medium text-title transition-colors group-hover:text-title-hover">
                    {item.title}
                  </p>
                  <p className="truncate text-xs text-faint">
                    {item.subtitle || `种子 #${item.id}`}
                  </p>
                </div>
                <div className="flex flex-wrap items-center gap-2 text-xs text-faint sm:justify-end">
                  <Badge variant="secondary">{item.category.name}</Badge>
                  <span className="font-mono text-foreground tabular-nums">
                    {formatBytes(item.total_size_bytes)}
                  </span>
                  <time
                    dateTime={item.published_at}
                    className="font-mono tabular-nums"
                  >
                    {formatCompactDateTime(item.published_at)}
                  </time>
                </div>
              </Link>
            ))}
          </div>
        )}
      </CardContent>
    </Card>
  )
}

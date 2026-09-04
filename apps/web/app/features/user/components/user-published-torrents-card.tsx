import { Link } from "react-router"

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
      <CardHeader className="px-6 pt-6 pb-4">
        <CardTitle>
          <h2>已发布种子</h2>
        </CardTitle>
        <CardDescription>
          公开、非匿名发布共 {total.toLocaleString("zh-CN")}{" "}
          个；以下为最近发布。
        </CardDescription>
      </CardHeader>
      <CardContent className="px-6 pb-6">
        {items.length === 0 ? (
          <p className="py-5 text-sm text-muted-foreground">
            暂无公开发布记录。
          </p>
        ) : (
          <div className="divide-y rounded-lg">
            {items.map((item) => (
              <Link
                key={item.id}
                to={`/torrents/${item.id}`}
                className="grid gap-2 p-3 transition-colors hover:bg-muted/50 sm:grid-cols-[minmax(0,1fr)_auto]"
              >
                <div className="min-w-0">
                  <p className="truncate font-bold text-title">{item.title}</p>
                  <p className="truncate text-xs text-muted-foreground">
                    {item.subtitle || `种子 #${item.id}`}
                  </p>
                </div>
                <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground sm:justify-end">
                  <Badge variant="outline">{item.category.name}</Badge>
                  <span>{formatBytes(item.total_size_bytes)}</span>
                  <time dateTime={item.published_at}>
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

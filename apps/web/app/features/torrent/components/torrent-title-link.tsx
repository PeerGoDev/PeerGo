import { Link } from "react-router"

import { isTorrentId } from "~/features/torrent/api/torrent.download"
import { cn } from "~/lib/utils"

export function TorrentTitleLink({
  torrentId,
  title,
  className,
}: {
  torrentId: number
  title: string
  className?: string
}) {
  if (!isTorrentId(torrentId)) {
    return <span className={className}>{title}</span>
  }

  return (
    <Link
      to={`/torrents/${torrentId}`}
      className={cn(
        "rounded-sm font-medium text-title transition-colors hover:text-title-hover focus-visible:ring-2 focus-visible:ring-ring/40 focus-visible:outline-none",
        className
      )}
    >
      {title}
    </Link>
  )
}

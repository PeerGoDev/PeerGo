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
        "rounded-sm underline-offset-4 hover:text-primary hover:underline focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none",
        className
      )}
    >
      {title}
    </Link>
  )
}

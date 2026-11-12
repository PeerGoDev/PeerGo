import { formatTorrentSizeParts } from "~/features/torrent/model/format"
import { cn } from "~/lib/utils"

const unitToneClass = {
  muted: "text-muted-foreground",
  blue: "text-info",
  green: "text-success-foreground",
  purple: "text-chart-5",
  red: "text-destructive",
} as const

export function TorrentSize({
  bytes,
  className,
}: {
  bytes: number | bigint | string
  className?: string
}) {
  const size = formatTorrentSizeParts(bytes)
  if (!size) {
    return <span className={cn("text-faint", className)}>—</span>
  }

  return (
    <span className={cn("font-mono tabular-nums", className)}>
      <span className="font-medium">{size.value}</span>
      <span className={cn("ml-0.5", unitToneClass[size.tone])}>
        {size.unit}
      </span>
    </span>
  )
}

import { Badge } from "~/components/ui/badge"
import type { TorrentPromotion as TorrentPromotionValue } from "~/features/torrent/api/torrent.queries"
import { cn } from "~/lib/utils"
import { PinIcon } from "lucide-react"

const promotionLabels: Record<
  Exclude<TorrentPromotionValue, "none">,
  string
> = {
  free: "免费",
  double_upload: "2X",
  double_upload_free: "2X免费",
  half_download: "50%",
  double_upload_half_download: "2X50%",
  thirty_percent_download: "30%",
}

export function torrentPromotionLabel(promotion: TorrentPromotionValue) {
  return promotion === "none" ? null : promotionLabels[promotion]
}

export function TorrentPromotion({
  promotion,
  className,
}: {
  promotion: TorrentPromotionValue
  className?: string
}) {
  const label = torrentPromotionLabel(promotion)
  if (!label) {
    return null
  }

  return (
    <Badge variant="destructive" className={cn("h-5", className)}>
      {label}
    </Badge>
  )
}

export function TorrentSticky({
  stickyUntil,
  className,
}: {
  stickyUntil: string | null
  className?: string
}) {
  if (!stickyUntil) {
    return null
  }

  return (
    <Badge
      variant="outline"
      className={cn(
        "h-5 border-warning/40 bg-warning/10 text-warning-foreground",
        className
      )}
      title={`置顶至 ${new Date(stickyUntil).toLocaleString("zh-CN")}`}
    >
      <PinIcon data-icon="inline-start" />
      置顶
    </Badge>
  )
}

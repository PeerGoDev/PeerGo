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

// Semantic promotion tones: green = free download, blue = discounted
// download, brand blue = pure upload bonus.
const promotionTones: Record<
  Exclude<TorrentPromotionValue, "none">,
  "success" | "primary" | "info"
> = {
  free: "success",
  double_upload: "primary",
  double_upload_free: "success",
  half_download: "info",
  double_upload_half_download: "info",
  thirty_percent_download: "info",
}

export function TorrentPromotion({
  promotion,
  className,
}: {
  promotion: TorrentPromotionValue
  className?: string
}) {
  const label = torrentPromotionLabel(promotion)
  if (!label || promotion === "none") {
    return null
  }

  return (
    <Badge variant={promotionTones[promotion]} className={className}>
      {label}
    </Badge>
  )
}

export function TorrentSticky({
  stickyUntil,
  className,
  iconOnly = false,
}: {
  stickyUntil: string | null
  className?: string
  iconOnly?: boolean
}) {
  if (!stickyUntil) {
    return null
  }

  return (
    <Badge
      variant="warning"
      className={cn(iconOnly && "px-1", className)}
      title={`置顶至 ${new Date(stickyUntil).toLocaleString("zh-CN")}`}
    >
      <PinIcon data-icon="inline-start" />
      {iconOnly ? <span className="sr-only">置顶</span> : "置顶"}
    </Badge>
  )
}

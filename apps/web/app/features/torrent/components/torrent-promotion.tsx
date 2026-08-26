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
// download, primary orange = pure upload bonus.
const promotionToneClasses: Record<
  Exclude<TorrentPromotionValue, "none">,
  string
> = {
  free: "bg-success/10 text-success-foreground",
  double_upload: "bg-primary/10 text-sidebar-accent-foreground",
  double_upload_free: "bg-success/10 text-success-foreground",
  half_download: "bg-info/10 text-info",
  double_upload_half_download: "bg-info/10 text-info",
  thirty_percent_download: "bg-info/10 text-info",
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
    <Badge
      variant="outline"
      className={cn(
        "h-5 border-transparent",
        promotionToneClasses[promotion],
        className
      )}
    >
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
      variant="outline"
      className={cn(
        "h-5 border-transparent bg-destructive/10 text-title",
        className
      )}
      title={`置顶至 ${new Date(stickyUntil).toLocaleString("zh-CN")}`}
    >
      <PinIcon data-icon="inline-start" />
      {iconOnly ? <span className="sr-only">置顶</span> : "置顶"}
    </Badge>
  )
}

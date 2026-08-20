import * as React from "react"
import { ImageIcon } from "lucide-react"

import { isTorrentId } from "~/features/torrent/api/torrent.download"
import { resolveApiUrl } from "~/shared/api/client"
import { cn } from "~/lib/utils"

export function TorrentCoverImage({
  torrentId,
  title,
  className,
  fallbackClassName,
  fallbackLabel,
  blurredBackground = false,
  obscured = false,
  showObscuredLabel = false,
  available = true,
}: {
  torrentId: number
  title: string
  className?: string
  fallbackClassName?: string
  fallbackLabel?: string
  blurredBackground?: boolean
  obscured?: boolean
  showObscuredLabel?: boolean
  available?: boolean
}) {
  const [failed, setFailed] = React.useState(false)
  const canReadCover = available && isTorrentId(torrentId) && !failed
  const source = resolveApiUrl(
    `/api/v1/torrents/${encodeURIComponent(torrentId)}/cover`
  )

  React.useEffect(() => {
    setFailed(false)
  }, [torrentId])

  if (!canReadCover) {
    return (
      <div
        className={cn(
          "flex size-full flex-col items-center justify-center gap-2 text-muted-foreground",
          fallbackClassName
        )}
        role="img"
        aria-label={`${title}暂无封面`}
      >
        <ImageIcon className="size-8" aria-hidden="true" />
        {fallbackLabel ? <span>{fallbackLabel}</span> : null}
      </div>
    )
  }

  return (
    <>
      {blurredBackground ? (
        <img
          src={source}
          alt=""
          aria-hidden="true"
          className="absolute inset-0 size-full scale-110 object-cover opacity-60 blur-xl"
          onError={() => setFailed(true)}
        />
      ) : null}
      <img
        src={source}
        alt={obscured ? `${title}封面已隐藏` : `${title}封面`}
        className={cn(
          className,
          obscured && "scale-110 blur-xl group-hover:scale-110"
        )}
        onError={() => setFailed(true)}
      />
      {obscured && showObscuredLabel ? (
        <span className="pointer-events-none absolute top-1/2 left-1/2 -translate-x-1/2 -translate-y-1/2 rounded bg-black/70 px-2 py-1 text-[11px] font-semibold whitespace-nowrap text-white shadow-sm">
          NSFW · 18+
        </span>
      ) : null}
    </>
  )
}

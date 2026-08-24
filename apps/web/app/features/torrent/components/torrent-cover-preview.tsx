import type { ReactNode } from "react"

import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "~/components/ui/tooltip"
import { TorrentCoverImage } from "~/features/torrent/components/torrent-cover-image"

export function TorrentCoverPreview({
  torrentId,
  title,
  children,
  triggerClassName,
  disabled = false,
}: {
  torrentId: number
  title: string
  children: ReactNode
  triggerClassName: string
  disabled?: boolean
}) {
  const trigger = <div className={triggerClassName}>{children}</div>
  if (disabled) return trigger

  return (
    <Tooltip>
      <TooltipTrigger render={trigger} />
      <TooltipContent
        side="inline-end"
        sideOffset={10}
        className="block max-w-none border bg-popover p-2 text-popover-foreground shadow-xl"
      >
        <div className="relative flex max-h-[70vh] w-72 items-center justify-center overflow-hidden rounded bg-muted sm:w-80">
          <TorrentCoverImage
            torrentId={torrentId}
            title={title}
            source="cover"
            className="max-h-[70vh] w-full object-contain"
            fallbackClassName="aspect-[2/3]"
          />
        </div>
      </TooltipContent>
    </Tooltip>
  )
}

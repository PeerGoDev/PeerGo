import * as React from "react"
import { ImageIcon } from "lucide-react"

import { Badge } from "~/components/ui/badge"
import { Card, CardContent, CardHeader, CardTitle } from "~/components/ui/card"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogTitle,
} from "~/components/ui/dialog"
import { resolveApiUrl } from "~/shared/api/client"

const maximumScreenshotCount = 6

export function TorrentScreenshotsCard({
  torrentId,
  screenshotCount,
}: {
  torrentId: number
  screenshotCount: number
}) {
  const [selectedPosition, setSelectedPosition] = React.useState<number | null>(
    null
  )
  const [failedPositions, setFailedPositions] = React.useState<Set<number>>(
    () => new Set()
  )
  const positions = React.useMemo(
    () =>
      Array.from(
        {
          length: Math.min(
            maximumScreenshotCount,
            Math.max(0, screenshotCount)
          ),
        },
        (_, position) => position
      ),
    [screenshotCount]
  )

  if (positions.length === 0) return null

  const recordFailure = (position: number) => {
    setFailedPositions((current) => {
      if (current.has(position)) return current
      const next = new Set(current)
      next.add(position)
      return next
    })
  }

  return (
    <>
      <Card className="gap-0 rounded-lg border py-0 shadow-sm ring-0">
        <CardHeader className="p-6 pb-2">
          <CardTitle className="flex items-center gap-2 text-2xl leading-none font-semibold">
            <ImageIcon className="size-5" />
            截图 ({positions.length})
          </CardTitle>
        </CardHeader>
        <CardContent className="px-6 pb-6">
          <div className="-mx-6 flex snap-x gap-2 overflow-x-auto px-6 sm:mx-0 sm:grid sm:grid-cols-3 sm:gap-3 sm:overflow-visible sm:px-0 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6">
            {positions.map((position) => (
              <ScreenshotThumbnail
                key={position}
                torrentId={torrentId}
                position={position}
                failed={failedPositions.has(position)}
                onFailure={() => recordFailure(position)}
                onSelect={() => setSelectedPosition(position)}
              />
            ))}
          </div>
        </CardContent>
      </Card>

      <Dialog
        open={selectedPosition !== null}
        onOpenChange={(open) => {
          if (!open) setSelectedPosition(null)
        }}
      >
        <DialogContent className="w-auto max-w-[calc(100%-2rem)] gap-2 p-2 sm:max-w-[calc(100%-3rem)]">
          <DialogTitle className="sr-only">
            {selectedPosition === null
              ? "种子截图"
              : `种子截图 ${selectedPosition + 1}`}
          </DialogTitle>
          <DialogDescription className="sr-only">
            查看上传者随种子提交的截图。
          </DialogDescription>
          {selectedPosition !== null ? (
            <img
              src={torrentScreenshotUrl(torrentId, selectedPosition)}
              alt={`截图 ${selectedPosition + 1} 大图`}
              className="max-h-[85vh] max-w-[90vw] rounded-lg object-contain"
              onError={() => {
                recordFailure(selectedPosition)
                setSelectedPosition(null)
              }}
            />
          ) : null}
        </DialogContent>
      </Dialog>
    </>
  )
}

function ScreenshotThumbnail({
  torrentId,
  position,
  failed,
  onFailure,
  onSelect,
}: {
  torrentId: number
  position: number
  failed: boolean
  onFailure: () => void
  onSelect: () => void
}) {
  return (
    <button
      type="button"
      aria-label={`查看截图 ${position + 1}`}
      className="relative aspect-[2/3] w-32 shrink-0 snap-start overflow-hidden rounded-lg border bg-muted text-muted-foreground transition-opacity hover:opacity-90 focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:outline-none sm:w-auto"
      onClick={onSelect}
      disabled={failed}
    >
      {position === 0 ? (
        <Badge className="absolute top-1 left-1 z-10 rounded-sm px-1.5 py-0.5 text-xs font-normal">
          封面
        </Badge>
      ) : null}
      {failed ? (
        <span className="flex size-full flex-col items-center justify-center gap-1 text-xs">
          <ImageIcon className="size-5" />
          暂无截图
        </span>
      ) : (
        <img
          src={torrentScreenshotUrl(torrentId, position)}
          alt={`截图 ${position + 1}`}
          className="size-full object-cover"
          loading="lazy"
          onError={onFailure}
        />
      )}
    </button>
  )
}

function torrentScreenshotUrl(torrentId: number, position: number) {
  return resolveApiUrl(
    `/api/v1/torrents/${encodeURIComponent(torrentId)}/screenshots/${position}`
  )
}

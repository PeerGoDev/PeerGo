import type { ComponentProps } from "react"
import { BookmarkCheckIcon, BookmarkIcon } from "lucide-react"

import { Button } from "~/components/ui/button"
import { Spinner } from "~/components/ui/spinner"
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "~/components/ui/tooltip"
import type { TorrentBookmarkControls } from "~/features/torrent/hooks/use-torrent-bookmark-controls"

export function TorrentBookmarkButton({
  torrentId,
  torrentName,
  controls,
  showLabel = false,
  iconVariant = "ghost",
  iconSize = "icon-xs",
  className,
}: {
  torrentId: number
  torrentName: string
  controls: TorrentBookmarkControls | undefined
  showLabel?: boolean
  iconVariant?: ComponentProps<typeof Button>["variant"]
  iconSize?: ComponentProps<typeof Button>["size"]
  className?: string
}) {
  if (!controls?.visible) {
    return null
  }

  const bookmarked = controls.bookmarkedIds.has(torrentId)
  const pending = controls.pendingTorrentId === torrentId
  const failed = controls.failedTorrentId === torrentId
  const description = failed
    ? "操作失败，请稍后重试"
    : pending
      ? bookmarked
        ? "正在加入收藏…"
        : "正在取消收藏…"
      : !controls.ready
        ? "收藏状态暂时不可用"
        : !controls.writable
          ? "当前账户不能修改收藏"
          : bookmarked
            ? "取消收藏"
            : "添加收藏"

  return (
    <>
      <Tooltip>
        <TooltipTrigger
          render={
            <Button
              type="button"
              variant={showLabel ? "outline" : iconVariant}
              size={showLabel ? "default" : iconSize}
              className={className}
              disabled={!controls.ready || !controls.writable || controls.busy}
              focusableWhenDisabled
              aria-label={`${bookmarked ? "取消收藏" : "收藏"}“${torrentName}”`}
              aria-pressed={bookmarked}
              onClick={() => controls.toggle(torrentId)}
            >
              {pending ? (
                <Spinner />
              ) : bookmarked ? (
                <BookmarkCheckIcon data-icon="inline-start" />
              ) : (
                <BookmarkIcon data-icon="inline-start" />
              )}
              {showLabel && (bookmarked ? "已收藏" : "收藏")}
            </Button>
          }
        />
        <TooltipContent>{description}</TooltipContent>
      </Tooltip>
      <span
        className="sr-only"
        role={failed ? "alert" : "status"}
        aria-live="polite"
      >
        {pending || failed ? description : ""}
      </span>
    </>
  )
}

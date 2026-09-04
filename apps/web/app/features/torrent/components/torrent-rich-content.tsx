import * as React from "react"
import {
  ChevronDownIcon,
  CircleAlertIcon,
  FileTextIcon,
  InfoIcon,
  RefreshCwIcon,
} from "lucide-react"
import Markdown from "react-markdown"

import {
  Alert,
  AlertAction,
  AlertDescription,
  AlertTitle,
} from "~/components/ui/alert"
import { Button } from "~/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "~/components/ui/card"
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "~/components/ui/collapsible"
import { Skeleton } from "~/components/ui/skeleton"
import { useTorrentContent } from "~/features/torrent/api/torrent.queries"
import {
  hasMediaInfoSummary,
  summarizeMediaInfo,
  type MediaInfoSummary,
} from "~/features/torrent/model/media-info"
import { TorrentScreenshotsCard } from "~/features/torrent/components/torrent-screenshots-card"
import { TorrentShareToSocial } from "~/features/torrent/components/torrent-share-to-social"
import { cn } from "~/lib/utils"

export function TorrentRichContent({
  torrentId,
  torrentTitle,
  torrentSubtitle,
  torrentSizeBytes,
  screenshotCount,
}: {
  torrentId: number
  torrentTitle: string
  torrentSubtitle: string
  torrentSizeBytes: number
  screenshotCount: number
}) {
  const content = useTorrentContent(torrentId)

  if (content.isPending) {
    return (
      <>
        <TorrentRichContentSkeleton />
        <TorrentScreenshotsCard
          torrentId={torrentId}
          screenshotCount={screenshotCount}
        />
      </>
    )
  }
  if (content.isError) {
    return (
      <>
        <TorrentScreenshotsCard
          torrentId={torrentId}
          screenshotCount={screenshotCount}
        />
        <Alert variant="destructive">
          <CircleAlertIcon />
          <AlertTitle>资源说明暂时不可用</AlertTitle>
          <AlertDescription>
            种子详情与下载不受影响，可以稍后单独重试长文内容。
          </AlertDescription>
          <AlertAction>
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={() => void content.refetch()}
            >
              <RefreshCwIcon data-icon="inline-start" />
              重试
            </Button>
          </AlertAction>
        </Alert>
      </>
    )
  }
  if (!content.data) {
    return (
      <TorrentScreenshotsCard
        torrentId={torrentId}
        screenshotCount={screenshotCount}
      />
    )
  }

  return (
    <TorrentRichContentView
      torrentId={torrentId}
      screenshotCount={screenshotCount}
      mediaInfo={content.data.media_info}
      description={content.data.description}
      descriptionFormat={content.data.description_format}
      shareAction={
        <TorrentShareToSocial
          torrentId={torrentId}
          title={torrentTitle}
          subtitle={torrentSubtitle}
          sizeBytes={torrentSizeBytes}
          screenshotCount={screenshotCount}
        />
      }
    />
  )
}

// Published and pending-review torrents use different authorization endpoints,
// but their submitted rich content is the same evidence. Keeping the renderer
// pure lets both surfaces share ordering, MediaInfo/BDInfo parsing, screenshots,
// and description behavior without making private review data public.
export function TorrentRichContentView({
  torrentId,
  screenshotCount,
  mediaInfo,
  description,
  descriptionFormat,
  screenshotUrl,
  shareAction,
}: {
  torrentId: number
  screenshotCount: number
  mediaInfo: string
  description: string
  descriptionFormat: "markdown" | "plain_text"
  screenshotUrl?: (torrentId: number, position: number) => string
  shareAction?: React.ReactNode
}) {
  return (
    <>
      {mediaInfo ? <TorrentMediaInfoCard mediaInfo={mediaInfo} /> : null}
      <TorrentScreenshotsCard
        torrentId={torrentId}
        screenshotCount={screenshotCount}
        screenshotUrl={screenshotUrl}
      />
      <TorrentDescriptionCard
        description={description}
        format={descriptionFormat}
        shareAction={shareAction}
      />
    </>
  )
}

export function TorrentMediaInfoCard({ mediaInfo }: { mediaInfo: string }) {
  const [open, setOpen] = React.useState(false)
  const summary = React.useMemo(
    () => summarizeMediaInfo(mediaInfo),
    [mediaInfo]
  )

  return (
    <Card className="gap-0 py-0">
      <Collapsible open={open} onOpenChange={setOpen}>
        <CardHeader className="p-6 pb-2">
          <CardTitle className="flex items-center gap-2">
            <InfoIcon className="size-4" />
            MediaInfo/BDInfo
          </CardTitle>
        </CardHeader>
        <CardContent className="px-6 pb-6">
          {hasMediaInfoSummary(summary) ? (
            <MediaInfoSummaryList summary={summary} />
          ) : (
            <p className="text-sm text-muted-foreground">
              暂无可提取的摘要，可展开查看完整技术信息。
            </p>
          )}
          <div className="mt-4 flex justify-end">
            <CollapsibleTrigger className="flex items-center gap-1 text-sm text-muted-foreground transition-colors hover:text-foreground">
              {open ? "隐藏原始信息" : "查看原始信息"}
              <ChevronDownIcon
                data-icon="inline-end"
                className={cn(
                  "size-3.5 transition-transform",
                  open && "rotate-180"
                )}
              />
            </CollapsibleTrigger>
          </div>
          <CollapsibleContent>
            <pre className="mt-4 overflow-x-auto rounded bg-muted p-4 font-mono text-xs leading-5 break-words whitespace-pre-wrap">
              {mediaInfo}
            </pre>
          </CollapsibleContent>
        </CardContent>
      </Collapsible>
    </Card>
  )
}

function MediaInfoSummaryList({ summary }: { summary: MediaInfoSummary }) {
  const videoFacts: [string, string | undefined][] = [
    ["时长", summary.duration],
    ["分辨率", summary.resolution],
    ["比特率", summary.videoBitRate || summary.overallBitRate],
    ["HDR", mediaInfoHdrLabel(summary)],
    ["帧率", summary.frameRate],
    ["档次", summary.profile],
    ["格式", summary.videoFormat],
  ]
  if (!summary.hdr && summary.bitDepth) {
    videoFacts.splice(3, 0, ["位深", summary.bitDepth])
  }
  const visibleVideoFacts = videoFacts.filter(
    (fact): fact is [string, string] => Boolean(fact[1])
  )

  return (
    <div className="grid gap-x-8 gap-y-3 text-sm md:grid-cols-3 md:gap-y-1">
      <dl className="flex flex-col gap-1">
        {visibleVideoFacts.map(([label, value]) => (
          <MediaInfoFact key={label} label={label} value={value} />
        ))}
      </dl>
      <MediaInfoTrackList kind="音轨" tracks={summary.audioTracks} />
      <MediaInfoTrackList kind="字幕" tracks={summary.subtitleTracks} />
    </div>
  )
}

function MediaInfoTrackList({
  kind,
  tracks,
}: {
  kind: "音轨" | "字幕"
  tracks: string[]
}) {
  const [expanded, setExpanded] = React.useState(false)
  const visibleTrackLimit = 5
  const visibleTracks = expanded ? tracks : tracks.slice(0, visibleTrackLimit)
  const hiddenTrackCount = tracks.length - visibleTrackLimit

  return (
    <div className="flex min-w-0 flex-col gap-1">
      <dl className="flex flex-col gap-1">
        {visibleTracks.map((value, index) => (
          <MediaInfoFact
            key={`${kind}-${index}`}
            label={`${kind} #${index + 1}`}
            value={value}
            track
          />
        ))}
      </dl>
      {hiddenTrackCount > 0 ? (
        <button
          type="button"
          aria-expanded={expanded}
          className="inline-flex w-fit items-center gap-1 text-sm text-primary hover:underline"
          onClick={() => setExpanded((current) => !current)}
        >
          <ChevronDownIcon
            className={cn(
              "size-3.5 transition-transform",
              expanded && "rotate-180"
            )}
          />
          {expanded ? "收起" : `展开更多${kind} (${hiddenTrackCount})`}
        </button>
      ) : null}
    </div>
  )
}

function mediaInfoHdrLabel(summary: MediaInfoSummary) {
  if (!summary.hdr) return undefined
  return [summary.hdr, summary.bitDepth].filter(Boolean).join(" / ")
}

function MediaInfoFact({
  label,
  value,
  track = false,
}: {
  label: string
  value: string
  track?: boolean
}) {
  return (
    <div className="flex min-w-0">
      <dt
        className={cn("w-14 shrink-0 text-muted-foreground", track && "w-16")}
      >
        {label}:
      </dt>
      <dd className="min-w-0 break-words">{value}</dd>
    </div>
  )
}

export function TorrentDescriptionCard({
  description,
  format,
  shareAction,
}: {
  description: string
  format: "markdown" | "plain_text"
  shareAction?: React.ReactNode
}) {
  return (
    <Card className="gap-0 py-0">
      <CardHeader className="p-6 pb-2">
        <CardTitle className="flex items-center gap-2">
          <FileTextIcon className="size-4" />
          描述
        </CardTitle>
      </CardHeader>
      <CardContent className="px-6 pb-6">
        <div>
          {!description ? (
            <p className="text-muted-foreground">暂无描述</p>
          ) : format === "plain_text" ? (
            <p className="text-sm leading-7 break-words whitespace-pre-wrap">
              {description}
            </p>
          ) : (
            <Markdown
              skipHtml
              components={{
                // Legacy upload images were intentionally excluded from the
                // migration. Suppressing image nodes avoids broken PtYes URLs
                // while preserving every migrated textual statement.
                img: () => null,
                a: ({ children, ...props }) => (
                  <a
                    {...props}
                    target="_blank"
                    rel="noreferrer"
                    className="text-primary underline underline-offset-4"
                  >
                    {children}
                  </a>
                ),
                p: ({ children }) => (
                  <p className="my-3 text-sm leading-7 break-words whitespace-pre-wrap first:mt-0 last:mb-0">
                    {children}
                  </p>
                ),
                blockquote: ({ children }) => (
                  <blockquote className="my-4 border-l-2 border-primary/70 bg-primary/5 px-4 py-1 text-muted-foreground">
                    {children}
                  </blockquote>
                ),
                pre: ({ children }) => (
                  <pre className="my-4 overflow-auto rounded-md border bg-muted/35 p-4 font-mono text-xs leading-5 break-words whitespace-pre-wrap">
                    {children}
                  </pre>
                ),
                code: ({ children }) => (
                  <code className="font-mono text-xs">{children}</code>
                ),
              }}
            >
              {description}
            </Markdown>
          )}
        </div>
        {shareAction ? (
          <div className="mt-3 flex items-center justify-end gap-2 border-t pt-3">
            <span className="mr-2 text-sm text-muted-foreground">
              觉得这个资源不错？
            </span>
            {shareAction}
          </div>
        ) : null}
      </CardContent>
    </Card>
  )
}

function TorrentRichContentSkeleton() {
  return (
    <Card size="sm" aria-label="正在加载资源说明" aria-busy="true">
      <CardHeader>
        <CardTitle>
          <Skeleton className="h-5 w-36" />
        </CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        <Skeleton className="h-4 w-full" />
        <Skeleton className="h-4 w-4/5" />
      </CardContent>
    </Card>
  )
}

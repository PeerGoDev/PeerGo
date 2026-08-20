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
    <>
      {content.data.media_info ? (
        <TorrentMediaInfoCard mediaInfo={content.data.media_info} />
      ) : null}
      <TorrentScreenshotsCard
        torrentId={torrentId}
        screenshotCount={screenshotCount}
      />
      <TorrentDescriptionCard
        description={content.data.description}
        format={content.data.description_format}
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
    <Card className="gap-0 rounded-lg border py-0 shadow-sm ring-0">
      <Collapsible open={open} onOpenChange={setOpen}>
        <CardHeader className="p-6 pb-2">
          <CardTitle className="flex items-center gap-2 text-2xl leading-none font-semibold">
            <InfoIcon className="size-5" />
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
  const videoFacts = [
    ["时长", summary.duration],
    ["分辨率", summary.resolution],
    ["比特率", summary.overallBitRate],
    ["HDR", summary.bitDepth],
    ["帧率", summary.frameRate],
    ["档次", summary.profile],
    ["格式", summary.videoFormat],
  ].filter((fact): fact is [string, string] => Boolean(fact[1]))

  return (
    <div className="grid gap-x-8 gap-y-1 text-sm sm:grid-cols-3">
      <dl className="flex flex-col gap-1">
        {videoFacts.map(([label, value]) => (
          <MediaInfoFact key={label} label={label} value={value} />
        ))}
      </dl>
      <dl className="flex flex-col gap-1">
        {summary.audioTracks.map((value, index) => (
          <MediaInfoFact
            key={`audio-${index}`}
            label={`音轨 #${index + 1}`}
            value={value}
            track
          />
        ))}
      </dl>
      <dl className="flex flex-col gap-1">
        {summary.subtitleTracks.map((value, index) => (
          <MediaInfoFact
            key={`subtitle-${index}`}
            label={`字幕 #${index + 1}`}
            value={value}
            track
          />
        ))}
      </dl>
    </div>
  )
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
    <Card className="gap-0 rounded-lg border py-0 shadow-sm ring-0">
      <CardHeader className="p-6 pb-2">
        <CardTitle className="flex items-center gap-2 text-2xl leading-none font-semibold">
          <FileTextIcon className="size-5" />
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

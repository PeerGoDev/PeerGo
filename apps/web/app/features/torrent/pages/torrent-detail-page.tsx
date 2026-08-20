import * as React from "react"
import { Link, useParams } from "react-router"
import {
  CircleAlertIcon,
  CircleCheckIcon,
  CopyIcon,
  EyeOffIcon,
  FileArchiveIcon,
  FolderOpenIcon,
  HashIcon,
  RefreshCwIcon,
  ZapIcon,
} from "lucide-react"

import {
  Alert,
  AlertAction,
  AlertDescription,
  AlertTitle,
} from "~/components/ui/alert"
import { Badge } from "~/components/ui/badge"
import { Button } from "~/components/ui/button"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "~/components/ui/card"
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "~/components/ui/empty"
import { Skeleton } from "~/components/ui/skeleton"
import { useWebSession } from "~/features/auth/api/session.mutations"
import { ContentTipDialog } from "~/features/economy/components/content-tip-dialog"
import { parseTorrentId } from "~/features/torrent/api/torrent.download"
import {
  type TorrentPublicDetail,
  useTorrentDetail,
  useTorrentFiles,
} from "~/features/torrent/api/torrent.queries"
import { TorrentDownloadButton } from "~/features/torrent/components/torrent-download-button"
import { TorrentCoverImage } from "~/features/torrent/components/torrent-cover-image"
import { torrentPromotionLabel } from "~/features/torrent/components/torrent-promotion"
import { TorrentBookmarkButton } from "~/features/torrent/components/torrent-bookmark-button"
import { TorrentPromotionProductDialog } from "~/features/torrent/components/torrent-promotion-product-dialog"
import { TorrentSticky } from "~/features/torrent/components/torrent-promotion"
import { OffsetPagination } from "~/shared/components/offset-pagination"
import { TorrentCommentsCard } from "~/features/torrent/components/torrent-comments-card"
import { TorrentPeerListCard } from "~/features/torrent/components/torrent-peer-list-card"
import { TorrentSwarmOverview } from "~/features/torrent/components/torrent-swarm-overview"
import { TorrentRichContent } from "~/features/torrent/components/torrent-rich-content"
import { TorrentRelatedVersions } from "~/features/torrent/components/torrent-related-versions"
import { TorrentReportDialog } from "~/features/torrent/components/torrent-report-dialog"
import {
  type TorrentBookmarkControls,
  useTorrentBookmarkControls,
} from "~/features/torrent/hooks/use-torrent-bookmark-controls"
import { formatTorrentSize } from "~/features/torrent/model/format"
import { ApiProblemError } from "~/shared/api/problem"
import { PageLayout } from "~/shared/components/page-layout"
import {
  formatCompactDate,
  formatCompactDateTime,
  formatDateTime,
} from "~/shared/formatters/date-time"
import { cn } from "~/lib/utils"

const filePageSize = 50

export function TorrentDetailPage() {
  const { torrentId: routeTorrentId } = useParams()
  const torrentId = parseTorrentId(routeTorrentId)
  const validTorrentId = torrentId !== undefined
  const detail = useTorrentDetail(torrentId ?? 0, validTorrentId)
  const session = useWebSession()
  const bookmarkTorrentIds = React.useMemo(
    () => (torrentId === undefined ? [] : [torrentId]),
    [torrentId, validTorrentId]
  )
  const bookmarkControls = useTorrentBookmarkControls(bookmarkTorrentIds)

  if (!validTorrentId) {
    return (
      <PageLayout>
        <TorrentDetailUnavailable invalidId />
      </PageLayout>
    )
  }
  if (detail.isPending) {
    return (
      <PageLayout>
        <TorrentDetailSkeleton />
      </PageLayout>
    )
  }
  if (detail.isError) {
    return (
      <PageLayout>
        <TorrentDetailUnavailable
          notFound={
            detail.error instanceof ApiProblemError &&
            detail.error.status === 404
          }
          retry={() => void detail.refetch()}
        />
      </PageLayout>
    )
  }
  if (!detail.data) {
    return null
  }

  return (
    <PageLayout className="gap-6">
      <TorrentDetailContent
        detail={detail.data}
        bookmarkControls={bookmarkControls}
        userId={session.data?.user.id}
        csrfToken={session.data?.csrf_token}
      />
      <TorrentRichContent
        torrentId={detail.data.id}
        torrentTitle={detail.data.title}
        torrentSubtitle={detail.data.subtitle}
        torrentSizeBytes={detail.data.total_size_bytes}
        screenshotCount={detail.data.screenshot_count}
      />
      <TorrentFilesCard
        key={detail.data.id}
        torrentId={detail.data.id}
        infoHash={detail.data.info_hash_v1}
      />
      <TorrentRelatedVersions torrentId={detail.data.id} />
      <TorrentPeerListCard torrentId={detail.data.id} />
      <TorrentCommentsCard torrentId={detail.data.id} />
    </PageLayout>
  )
}

function TorrentDetailContent({
  detail,
  bookmarkControls,
  userId,
  csrfToken,
}: {
  detail: TorrentPublicDetail
  bookmarkControls: TorrentBookmarkControls
  userId?: string
  csrfToken?: string
}) {
  const groupedFacets = groupTorrentFacets(detail)
  const inlineFacets = groupedFacets.filter(
    (facet) => facet.values.length === 1
  )
  const tagFacets = groupedFacets.filter((facet) => facet.values.length > 1)
  const promotionLabel = torrentPromotionLabel(detail.promotion)

  return (
    <Card className="gap-0 rounded-lg border py-0 shadow-sm ring-0">
      <CardContent className="p-6">
        <div className="mb-2 flex items-start justify-between gap-4 max-sm:flex-col max-sm:gap-2">
          <div className="min-w-0 flex-1">
            <h1 className="text-xl font-bold break-all max-sm:text-base">
              {detail.title}
            </h1>
          </div>
          <div className="flex shrink-0 items-center justify-end gap-1.5 max-sm:justify-start md:min-w-[178px]">
            <TorrentBookmarkButton
              torrentId={detail.id}
              torrentName={detail.title}
              controls={bookmarkControls}
              iconVariant="outline"
              iconSize="icon"
            />
          </div>
        </div>

        <div className="flex min-w-0 gap-4 max-md:flex-wrap max-md:gap-3">
          <div
            className={cn(
              "relative w-32 shrink-0 self-start overflow-hidden rounded-lg border bg-muted max-sm:w-24 md:w-40",
              // Migrated images are intentionally absent. Preserve the same
              // desktop cover slot as PtYes so the statistics rail and the
              // following MediaInfo card do not jump upward for legacy rows.
              detail.screenshot_count === 0 && "h-36 md:h-56"
            )}
          >
            <Badge className="absolute top-1 left-1 z-10 rounded-sm px-1.5 py-0.5 text-xs font-normal">
              封面
            </Badge>
            <TorrentCoverImage
              torrentId={detail.id}
              title={detail.title}
              available={detail.screenshot_count > 0}
              className="h-auto max-h-56 w-full object-contain max-sm:max-h-40"
              fallbackClassName="bg-gradient-to-br from-neutral-100 via-neutral-200 to-neutral-300 px-2 text-center text-xs text-neutral-500 dark:from-neutral-900 dark:via-neutral-800 dark:to-neutral-700 dark:text-neutral-400 [&_svg]:size-7"
              fallbackLabel="暂无封面"
            />
          </div>

          <div className="flex min-w-0 flex-1 flex-col gap-2 max-md:basis-[55%]">
            {detail.subtitle ? (
              <p className="text-sm text-muted-foreground">{detail.subtitle}</p>
            ) : null}

            <dl className="flex flex-wrap items-center gap-x-4 gap-y-1 text-sm">
              <div className="flex min-w-0 items-baseline gap-1">
                <dt className="text-muted-foreground">分类:</dt>
                <dd>
                  <Link
                    to={`/torrents?category=${encodeURIComponent(detail.category.id)}`}
                    className="font-medium text-primary hover:underline"
                  >
                    {detail.category.name}
                  </Link>
                </dd>
              </div>
              {inlineFacets.map((facet) => (
                <InlineDetailFact
                  key={facet.id}
                  label={facet.name}
                  value={facet.values[0] ?? "-"}
                />
              ))}
              <InlineDetailFact
                label="大小"
                value={formatTorrentSize(detail.total_size_bytes)}
              />
            </dl>

            {detail.external_identifiers.length > 0 ? (
              <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-sm">
                {sortExternalIdentifiers(detail.external_identifiers).map(
                  (identifier) => {
                    const href = externalIdentifierURL(
                      identifier.provider,
                      identifier.external_id
                    )
                    return href ? (
                      <a
                        key={`${identifier.provider}:${identifier.external_id}`}
                        href={href}
                        target="_blank"
                        rel="noreferrer"
                        className="inline-flex items-center gap-1 hover:opacity-80"
                      >
                        <Badge
                          className={cn(
                            "rounded-sm border-0 px-1 py-0 text-[10px] leading-4 font-bold",
                            externalIdentifierBadgeClass(identifier.provider)
                          )}
                        >
                          {externalIdentifierLabel(identifier.provider)}
                        </Badge>
                        <span className="text-muted-foreground">暂无</span>
                      </a>
                    ) : null
                  }
                )}
              </div>
            ) : null}

            {tagFacets.length > 0 ? (
              <dl className="flex flex-col gap-1 text-sm">
                {tagFacets.map((facet) => (
                  <div key={facet.id} className="flex items-center gap-1.5">
                    <dt className="text-xs text-muted-foreground">
                      {facet.name}:
                    </dt>
                    <dd className="flex flex-wrap gap-1">
                      {facet.values.map((value) => (
                        <Badge
                          key={value}
                          variant="secondary"
                          className="rounded px-2 py-0.5 text-xs font-normal"
                        >
                          {value}
                        </Badge>
                      ))}
                    </dd>
                  </div>
                ))}
              </dl>
            ) : null}
          </div>

          <div className="flex w-[156px] shrink-0 flex-col gap-2 self-start max-md:w-full">
            <TorrentDownloadButton
              torrentId={detail.id}
              torrentName={detail.title}
              purchaseAware
              showLabel
              showCopyAction
              className="h-10 w-full"
            />
            <TorrentPromotionProductDialog
              torrentId={detail.id}
              torrentTitle={detail.title}
            />
            <ContentTipDialog
              target={{
                kind: "torrent",
                torrentId: detail.id,
                title: detail.title,
              }}
              userId={userId}
              csrfToken={csrfToken}
              buttonVariant="outline"
              buttonSize="default"
              className="h-9 w-full"
            />
            <TorrentReportDialog
              torrentId={detail.id}
              torrentTitle={detail.title}
            />
          </div>
        </div>

        <div className="mt-4 flex items-center justify-between gap-x-4 gap-y-2 max-md:flex-wrap">
          <div className="flex items-center gap-2">
            <TorrentSticky stickyUntil={detail.sticky_until} />
            {promotionLabel ? (
              <div className="inline-flex h-[30px] items-center gap-1.5 rounded-sm border border-destructive/20 bg-destructive/10 px-3 text-sm font-semibold text-destructive">
                <ZapIcon className="size-3.5" />
                <span>{promotionLabel}</span>
                {detail.promotion_ends_at ? (
                  <span className="ml-1 text-xs font-semibold opacity-70">
                    至{" "}
                    <time dateTime={detail.promotion_ends_at}>
                      {formatCompactDate(detail.promotion_ends_at)}
                    </time>
                  </span>
                ) : null}
              </div>
            ) : null}
          </div>
          <div className="flex min-w-0 flex-1 flex-wrap items-center justify-between gap-x-4 gap-y-1 max-md:w-full max-md:flex-col max-md:items-start md:min-w-[678px]">
            <TorrentSwarmOverview
              torrentId={detail.id}
              compact
              showFreshness={false}
              className="w-auto"
            />
            <dl className="flex flex-wrap items-center justify-start gap-x-4 gap-y-1 text-sm">
              <InlineDetailFact
                label="上传者"
                value={
                  detail.anonymous ? (
                    <span className="inline-flex items-center gap-1 text-muted-foreground">
                      <EyeOffIcon className="size-3.5" />
                      匿名
                    </span>
                  ) : (
                    detail.uploader_display_name
                  )
                }
              />
              <InlineDetailFact
                label="上传时间"
                value={`${formatCompactDateTime(detail.published_at)} (${formatTimeAgo(detail.published_at)})`}
              />
            </dl>
          </div>
        </div>
      </CardContent>
    </Card>
  )
}

function InlineDetailFact({
  label,
  value,
}: {
  label: string
  value: React.ReactNode
}) {
  return (
    <div className="flex min-w-0 items-baseline gap-1">
      <dt className="text-muted-foreground">{label}:</dt>
      <dd className="font-medium break-words">{value}</dd>
    </div>
  )
}

function formatTimeAgo(value: string) {
  const timestamp = Date.parse(value)
  if (!Number.isFinite(timestamp)) return "时间未知"

  const seconds = Math.max(0, Math.floor((Date.now() - timestamp) / 1_000))
  if (seconds < 60) return "刚刚"
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return `${minutes}分钟前`
  const hours = Math.floor(minutes / 60)
  if (hours < 24) return `${hours}小时前`
  const days = Math.floor(hours / 24)
  if (days < 30) return `${days}天前`
  const months = Math.floor(days / 30)
  if (months < 12) return `${months}个月前`
  return `${Math.floor(months / 12)}年前`
}

function groupTorrentFacets(detail: TorrentPublicDetail) {
  const grouped = new Map<
    string,
    { id: string; name: string; values: string[] }
  >()
  for (const facet of detail.facets) {
    const current = grouped.get(facet.facet_id)
    if (current) {
      current.values.push(facet.option_label)
      continue
    }
    grouped.set(facet.facet_id, {
      id: facet.facet_id,
      name: facet.facet_name,
      values: [facet.option_label],
    })
  }
  return [...grouped.values()]
}

function externalIdentifierLabel(provider: string) {
  switch (provider) {
    case "imdb":
      return "IMDb"
    case "douban":
      return "豆瓣"
    case "tmdb":
      return "TMDB"
    case "bangumi":
      return "Bangumi"
    case "steam":
      return "Steam"
    default:
      return provider
  }
}

function sortExternalIdentifiers(
  identifiers: TorrentPublicDetail["external_identifiers"]
) {
  const providerOrder = ["imdb", "douban", "tmdb", "bangumi", "steam"]
  const priority = (provider: string) => {
    const index = providerOrder.indexOf(provider)
    return index === -1 ? providerOrder.length : index
  }
  return [...identifiers].sort(
    (left, right) => priority(left.provider) - priority(right.provider)
  )
}

function externalIdentifierBadgeClass(provider: string) {
  switch (provider) {
    case "imdb":
      return "bg-warning text-warning-foreground"
    case "douban":
      return "bg-success text-success-foreground"
    case "tmdb":
      return "bg-chart-3 text-primary-foreground"
    case "bangumi":
      return "bg-chart-5 text-primary-foreground"
    case "steam":
      return "bg-secondary text-secondary-foreground"
    default:
      return "bg-secondary text-secondary-foreground"
  }
}

function externalIdentifierURL(provider: string, externalId: string) {
  switch (provider) {
    case "imdb":
      return `https://www.imdb.com/title/${externalId}/`
    case "douban":
      return `https://movie.douban.com/subject/${externalId}/`
    case "bangumi":
      return `https://bgm.tv/subject/${externalId}`
    case "steam":
      return `https://store.steampowered.com/app/${externalId}/`
    default:
      // TMDB IDs are media-type dependent, so the API should not invent a
      // movie or TV route without an explicit type in the persisted metadata.
      return null
  }
}

function TorrentFilesCard({
  torrentId,
  infoHash,
}: {
  torrentId: number
  infoHash: string
}) {
  const [offset, setOffset] = React.useState(0)
  const [expanded, setExpanded] = React.useState(false)
  const [copied, setCopied] = React.useState(false)
  const files = useTorrentFiles(torrentId, filePageSize, offset)
  const visibleFiles = files.data
    ? expanded
      ? files.data.items
      : files.data.items.slice(0, 5)
    : []

  function changeOffset(nextOffset: number) {
    setExpanded(false)
    setOffset(nextOffset)
  }

  async function copyInfoHash() {
    await navigator.clipboard.writeText(infoHash)
    setCopied(true)
    globalThis.setTimeout(() => setCopied(false), 2_000)
  }

  return (
    <Card
      className="gap-0 rounded-lg py-0 shadow-sm"
      aria-labelledby="torrent-files-title"
    >
      <CardHeader className="p-6 pb-2">
        <CardTitle
          id="torrent-files-title"
          className="flex flex-wrap items-center justify-between gap-2 text-base font-semibold"
        >
          <span className="flex items-center gap-2">
            <FileArchiveIcon className="size-4" />
            文件列表
            {files.data
              ? ` (${files.data.total.toLocaleString("zh-CN")} 个文件)`
              : ""}
          </span>
          <span className="flex min-w-0 items-center gap-2 text-sm font-normal">
            <HashIcon className="size-3.5 shrink-0 text-muted-foreground" />
            <code className="max-w-[140px] truncate font-mono text-xs text-muted-foreground sm:max-w-[200px] md:max-w-none">
              {infoHash}
            </code>
            <Button
              type="button"
              variant="ghost"
              size="icon-xs"
              aria-label="复制 Info Hash"
              onClick={() => void copyInfoHash()}
            >
              {copied ? (
                <CircleCheckIcon className="text-success" />
              ) : (
                <CopyIcon />
              )}
            </Button>
          </span>
        </CardTitle>
      </CardHeader>
      <CardContent className="px-6 pb-6">
        {files.isPending && <TorrentFilesSkeleton />}
        {files.isError && (
          <Alert variant="destructive">
            <CircleAlertIcon />
            <AlertTitle>文件清单暂时不可用</AlertTitle>
            <AlertDescription>
              种子可能已停止公开，也可能是服务暂时繁忙，请稍后重试。
            </AlertDescription>
            <AlertAction>
              <Button
                variant="outline"
                size="sm"
                onClick={() => void files.refetch()}
              >
                <RefreshCwIcon data-icon="inline-start" />
                重试
              </Button>
            </AlertAction>
          </Alert>
        )}
        {files.data && files.data.items.length === 0 && (
          <Empty>
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <FolderOpenIcon />
              </EmptyMedia>
              <EmptyTitle>这一页没有文件</EmptyTitle>
              <EmptyDescription>返回上一页查看，或稍后重试。</EmptyDescription>
            </EmptyHeader>
          </Empty>
        )}
        {files.data && files.data.items.length > 0 && (
          <>
            <div className="flex flex-col gap-1">
              {visibleFiles.map((file) => (
                <div
                  key={file.file_index}
                  className="flex items-center justify-between gap-4 border-b py-1.5 text-sm last:border-0"
                >
                  <div className="flex min-w-0 flex-1 items-center gap-2">
                    <FileArchiveIcon className="size-3.5 shrink-0 text-muted-foreground" />
                    <span className="truncate" title={file.display_path}>
                      {file.display_path}
                    </span>
                    {file.is_padding ? (
                      <Badge variant="outline">填充</Badge>
                    ) : null}
                  </div>
                  <span className="shrink-0 text-muted-foreground tabular-nums">
                    {formatTorrentSize(file.size_bytes)}
                  </span>
                </div>
              ))}
            </div>

            {files.data.items.length > 5 ? (
              <Button
                type="button"
                variant="ghost"
                size="sm"
                className="mt-2 w-full text-muted-foreground"
                onClick={() => setExpanded((current) => !current)}
              >
                {expanded
                  ? "收起文件列表 ▲"
                  : `展开全部 ${files.data.items.length.toLocaleString("zh-CN")} 个文件 ▼`}
              </Button>
            ) : null}

            {files.data.total > files.data.limit ? (
              <OffsetPagination
                total={files.data.total}
                limit={files.data.limit}
                offset={files.data.offset}
                onOffsetChange={changeOffset}
                ariaLabel="种子文件分页"
                summaryLabel="文件"
                buttonVariant="ghost"
                className="mt-3 border-t pt-3"
              />
            ) : null}
          </>
        )}
      </CardContent>
    </Card>
  )
}

function TorrentDetailUnavailable({
  invalidId = false,
  notFound = false,
  retry,
}: {
  invalidId?: boolean
  notFound?: boolean
  retry?: () => void
}) {
  const unavailable = invalidId || notFound
  if (unavailable) {
    return (
      <div className="flex min-h-[calc(100vh-200px)] items-center justify-center">
        <div className="text-center">
          <CircleAlertIcon className="mx-auto mb-4 size-16 text-destructive" />
          <p className="mb-4 text-xl font-medium text-destructive">
            种子不存在
          </p>
          <Button nativeButton={false} render={<Link to="/" />}>
            返回首页
          </Button>
        </div>
      </div>
    )
  }

  return (
    <div className="flex min-h-[calc(100vh-200px)] items-center justify-center">
      <div className="text-center">
        <CircleAlertIcon className="mx-auto mb-4 size-16 text-destructive" />
        <p className="mb-4 text-xl font-medium text-destructive">
          种子暂时无法读取
        </p>
        <div className="flex justify-center gap-2">
          <Button
            variant="outline"
            nativeButton={false}
            render={<Link to="/" />}
          >
            返回首页
          </Button>
          {retry ? (
            <Button onClick={retry}>
              <RefreshCwIcon data-icon="inline-start" />
              重试
            </Button>
          ) : null}
        </div>
      </div>
    </div>
  )
}

function TorrentDetailSkeleton() {
  return (
    <div
      className="flex flex-col gap-5"
      aria-label="正在加载种子详情"
      aria-busy="true"
    >
      <Skeleton className="h-7 w-28" />
      <Card size="sm">
        <CardHeader>
          <CardTitle>
            <Skeleton className="h-7 w-4/5" />
          </CardTitle>
          <CardDescription>
            <Skeleton className="h-4 w-2/5" />
          </CardDescription>
        </CardHeader>
        <CardContent className="grid gap-4 sm:grid-cols-[7rem_minmax(0,1fr)]">
          <Skeleton className="aspect-[2/3] w-24 justify-self-center sm:w-full" />
          <div className="grid content-start gap-4 sm:grid-cols-2 lg:grid-cols-4">
            {Array.from({ length: 4 }, (_, index) => (
              <Skeleton key={index} className="h-10 w-full" />
            ))}
          </div>
        </CardContent>
      </Card>
      <TorrentFilesSkeleton />
    </div>
  )
}

function TorrentFilesSkeleton() {
  return (
    <div
      className="flex flex-col gap-3"
      aria-label="正在加载文件清单"
      aria-busy="true"
    >
      {Array.from({ length: 5 }, (_, index) => (
        <Skeleton key={index} className="h-10 w-full" />
      ))}
    </div>
  )
}

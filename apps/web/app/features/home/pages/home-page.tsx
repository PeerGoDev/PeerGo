import * as React from "react"
import { Link } from "react-router"
import { NewspaperIcon, PinIcon, TriangleAlertIcon } from "lucide-react"

import { Alert, AlertDescription, AlertTitle } from "~/components/ui/alert"
import { Button } from "~/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "~/components/ui/card"
import { Input } from "~/components/ui/input"
import { Skeleton } from "~/components/ui/skeleton"
import { useWebSession } from "~/features/auth/api/session.mutations"
import {
  useLatestAnnouncement,
  useSiteInfo,
} from "~/features/site/api/site.queries"
import { TorrentCards } from "~/features/torrent/components/torrent-cards"
import {
  TorrentListEmpty,
  TorrentListError,
  TorrentListSkeleton,
} from "~/features/torrent/components/torrent-list-state"
import { TorrentTable } from "~/features/torrent/components/torrent-table"
import { TorrentViewControls } from "~/features/torrent/components/torrent-view-controls"
import { useTorrentList } from "~/features/torrent/api/torrent.queries"
import { useTorrentBookmarkControls } from "~/features/torrent/hooks/use-torrent-bookmark-controls"
import { useAdultCoverVisibility } from "~/features/torrent/hooks/use-adult-cover-visibility"
import { useTorrentView } from "~/features/torrent/hooks/use-torrent-view"
import { PageHeader, PageLayout } from "~/shared/components/page-layout"
import { formatLongDate } from "~/shared/formatters/date-time"

export function HomePage() {
  const [draftQuery, setDraftQuery] = React.useState("")
  const [query, setQuery] = React.useState("")
  const siteInfo = useSiteInfo()
  const session = useWebSession()
  const isAuthenticated = Boolean(session.data)
  const announcement = useLatestAnnouncement(isAuthenticated)
  const torrents = useTorrentList({ query }, isAuthenticated)
  const torrentIds = React.useMemo(
    () => torrents.data?.items.map((torrent) => torrent.id) ?? [],
    [torrents.data?.items]
  )
  const bookmarkControls = useTorrentBookmarkControls(torrentIds)
  const [view, setView] = useTorrentView(siteInfo.data?.default_torrent_view)
  const [adultCoversVisible, setAdultCoversVisible] = useAdultCoverVisibility()
  const showLatestAnnouncement = siteInfo.data?.show_latest_announcement ?? true

  if (!isAuthenticated) {
    return (
      <CommunityWelcome
        name={siteInfo.data?.name ?? "PeerGo"}
        description={siteInfo.data?.description ?? "私有分享社区"}
        registrationAvailable={siteInfo.data?.registration_mode !== "closed"}
      />
    )
  }

  function handleSearch(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setQuery(draftQuery.trim())
  }

  return (
    <PageLayout className="gap-6">
      <PageHeader title="首页" />

      {showLatestAnnouncement ? (
        <section
          id="latest-announcement"
          aria-labelledby="announcement-title"
          className="w-full"
        >
          <AnnouncementState
            data={announcement.data}
            loading={announcement.isPending}
            error={announcement.isError}
            publisherLabel={`${siteInfo.data?.name ?? "PeerGo"} 站务`}
          />
        </section>
      ) : null}

      <section aria-labelledby="catalog-search-title" className="flex flex-col">
        <h2 id="catalog-search-title" className="sr-only">
          搜索种子
        </h2>
        <form onSubmit={handleSearch} role="search" className="flex gap-3">
          <Input
            value={draftQuery}
            onChange={(event) => setDraftQuery(event.target.value)}
            placeholder="搜索种子..."
            aria-label="搜索种子"
            maxLength={100}
            className="h-10 flex-1 rounded-[6px] bg-background"
          />
          <Button
            type="submit"
            className="h-10 w-[60px] shrink-0 rounded-[6px] px-0"
          >
            搜索
          </Button>
        </form>
      </section>

      <section
        id="latest-torrents"
        aria-labelledby="latest-torrents-title"
        className="flex w-full scroll-mt-20 flex-col gap-4"
      >
        <div className="flex w-full flex-wrap items-center justify-between gap-3">
          <div className="flex flex-wrap items-center gap-2">
            <h2
              id="latest-torrents-title"
              className="font-heading text-xl font-semibold"
            >
              {query ? "搜索结果" : "最新种子"}
            </h2>
            {query && (
              <span
                className="text-xs text-muted-foreground"
                aria-live="polite"
              >
                {torrents.data
                  ? `共 ${torrents.data.total.toLocaleString("zh-CN")} 条`
                  : "搜索中"}
              </span>
            )}
          </div>

          <TorrentViewControls
            value={view}
            onValueChange={setView}
            adultCoversVisible={adultCoversVisible}
            onAdultCoversVisibleChange={setAdultCoversVisible}
          />
        </div>

        {torrents.isPending && <TorrentListSkeleton />}
        {torrents.isError && (
          <TorrentListError
            error={torrents.error}
            retry={() => void torrents.refetch()}
          />
        )}
        {torrents.data && torrents.data.items.length === 0 && (
          <TorrentListEmpty query={query} />
        )}
        {torrents.data && torrents.data.items.length > 0 && (
          <>
            {view === "list" && (
              <>
                <TorrentTable
                  torrents={torrents.data.items}
                  bookmarkControls={bookmarkControls}
                  adultCoversVisible={adultCoversVisible}
                />
                <TorrentCards
                  torrents={torrents.data.items}
                  poster={false}
                  bookmarkControls={bookmarkControls}
                  adultCoversVisible={adultCoversVisible}
                />
              </>
            )}
            {view === "poster" && (
              <TorrentCards
                torrents={torrents.data.items}
                poster
                bookmarkControls={bookmarkControls}
                adultCoversVisible={adultCoversVisible}
              />
            )}
          </>
        )}
      </section>
    </PageLayout>
  )
}

function CommunityWelcome({
  name,
  description,
  registrationAvailable,
}: {
  name: string
  description: string
  registrationAvailable: boolean
}) {
  return (
    <PageLayout
      className="min-h-[calc(100svh-var(--shell-header-height)-var(--shell-gap))] items-center justify-center"
      aria-labelledby="community-welcome-title"
    >
      <div className="flex max-w-xl flex-col items-center gap-6 text-center">
        <div className="flex flex-col gap-3">
          <h1
            id="community-welcome-title"
            className="font-heading text-4xl font-bold tracking-tight sm:text-5xl"
          >
            {name}
          </h1>
          <p className="text-base text-muted-foreground">{description}</p>
        </div>
        <div className="flex items-center gap-3">
          <Button nativeButton={false} render={<Link to="/login" />} size="lg">
            登录
          </Button>
          {registrationAvailable ? (
            <Button
              nativeButton={false}
              render={<Link to="/register" />}
              variant="outline"
              size="lg"
            >
              注册
            </Button>
          ) : null}
        </div>
      </div>
    </PageLayout>
  )
}

function AnnouncementState({
  data,
  loading,
  error,
  publisherLabel,
}: {
  data:
    | {
        id: string
        title: string
        summary: string
        published_at: string
      }
    | null
    | undefined
  loading: boolean
  error: boolean
  publisherLabel: string
}) {
  if (loading) {
    return (
      <Card
        className="min-h-[122px] gap-0 py-0"
        aria-label="正在加载最新公告"
        aria-busy="true"
      >
        <CardHeader className="px-4 pt-4 pb-3">
          <CardTitle className="flex items-center gap-2 leading-5 [&_svg]:size-4">
            <Skeleton className="h-4 w-24" />
          </CardTitle>
        </CardHeader>
        <CardContent className="px-4 pt-0 pb-4">
          <div className="flex flex-col gap-2 border-l-2 border-primary py-2 pl-3">
            <Skeleton className="h-4 w-48" />
            <Skeleton className="h-3 w-72 max-w-full" />
          </div>
        </CardContent>
      </Card>
    )
  }

  if (error) {
    return (
      <Alert variant="destructive">
        <TriangleAlertIcon />
        <AlertTitle>公告暂时无法读取</AlertTitle>
        <AlertDescription>
          首页其余内容仍可使用，公告恢复后会自动重新获取。
        </AlertDescription>
      </Alert>
    )
  }

  if (!data) {
    return (
      <Card className="min-h-[122px] gap-0 py-0">
        <CardHeader className="px-4 pt-4 pb-3">
          <CardTitle className="flex items-center gap-2 text-sm leading-5 font-semibold text-muted-foreground [&_svg]:size-4">
            <NewspaperIcon className="text-primary" />
            <span>最新公告</span>
          </CardTitle>
        </CardHeader>
        <CardContent className="px-4 pt-0 pb-4">
          <div className="border-l-2 border-primary py-2 pl-3 text-sm text-muted-foreground">
            暂无公告
          </div>
        </CardContent>
      </Card>
    )
  }

  return (
    <Card className="min-h-[122px] gap-0 py-0">
      <CardHeader className="px-4 pt-4 pb-3">
        <CardTitle className="flex items-center gap-2 text-sm leading-5 font-semibold text-muted-foreground [&_svg]:size-4">
          <NewspaperIcon className="text-primary" />
          <span>最新公告</span>
        </CardTitle>
      </CardHeader>
      <CardContent className="px-4 pt-0 pb-4">
        <div className="-ml-1 border-l-2 border-primary py-2 pl-3">
          <div className="flex min-w-0 items-start gap-2">
            <PinIcon
              className="mt-0.5 size-3.5 shrink-0 text-primary"
              aria-hidden="true"
            />
            <div className="min-w-0 flex-1">
              <h2
                id="announcement-title"
                className="truncate text-sm font-medium"
              >
                <Link
                  to={`/announcements/${encodeURIComponent(data.id)}`}
                  className="hover:text-primary hover:underline hover:underline-offset-4"
                >
                  {data.title}
                </Link>
              </h2>
              <div className="mt-1 flex min-w-0 items-center gap-2 text-xs text-muted-foreground">
                <span className="min-w-0 truncate">{publisherLabel}</span>
                <span className="shrink-0" aria-hidden="true">
                  •
                </span>
                <time className="shrink-0" dateTime={data.published_at}>
                  {formatLongDate(data.published_at)}
                </time>
              </div>
            </div>
          </div>
        </div>
      </CardContent>
    </Card>
  )
}

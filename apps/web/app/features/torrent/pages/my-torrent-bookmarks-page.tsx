import * as React from "react"
import { Link } from "react-router"
import {
  BookmarkIcon,
  CircleAlertIcon,
  LogInIcon,
  RefreshCwIcon,
  ShieldXIcon,
} from "lucide-react"

import {
  Alert,
  AlertAction,
  AlertDescription,
  AlertTitle,
} from "~/components/ui/alert"
import { Button, buttonVariants } from "~/components/ui/button"
import { Card, CardContent } from "~/components/ui/card"
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "~/components/ui/empty"
import { useWebSession } from "~/features/auth/api/session.mutations"
import { useCapabilities } from "~/features/authz/api/capabilities.queries"
import { useTrafficOverview } from "~/features/traffic/api/traffic.queries"
import { useMyTorrentBookmarks } from "~/features/torrent/api/torrent-bookmarks.queries"
import { TorrentCards } from "~/features/torrent/components/torrent-cards"
import { TorrentListSkeleton } from "~/features/torrent/components/torrent-list-state"
import { OffsetPagination } from "~/shared/components/offset-pagination"
import { TorrentTable } from "~/features/torrent/components/torrent-table"
import { useTorrentBookmarkControls } from "~/features/torrent/hooks/use-torrent-bookmark-controls"
import { useAdultCoverVisibility } from "~/features/torrent/hooks/use-adult-cover-visibility"
import { PageLayout } from "~/shared/components/page-layout"
import { requestErrorDescription } from "~/shared/api/problem"

const bookmarkPageSize = 20

export function MyTorrentBookmarksPage() {
  const [offset, setOffset] = React.useState(0)
  const session = useWebSession()
  const capabilities = useCapabilities(session.data?.user.id)
  const traffic = useTrafficOverview(session.data?.user.id)
  const canRead = Boolean(
    capabilities.data?.items.some(
      (capability) => capability.action === "torrent.bookmark.read.self"
    )
  )
  const bookmarks = useMyTorrentBookmarks(
    session.data?.user.id,
    bookmarkPageSize,
    offset,
    canRead
  )
  const torrents = React.useMemo(
    () => bookmarks.data?.items.map((item) => item.torrent) ?? [],
    [bookmarks.data?.items]
  )
  const bookmarkTimes = React.useMemo(
    () =>
      new Map(
        bookmarks.data?.items.map((item) => [
          item.torrent.id,
          item.bookmarked_at,
        ]) ?? []
      ),
    [bookmarks.data?.items]
  )
  const torrentIds = React.useMemo(
    () => torrents.map((torrent) => torrent.id),
    [torrents]
  )
  const bookmarkControls = useTorrentBookmarkControls(torrentIds)
  const activityByTorrentId = React.useMemo(
    () =>
      new Map(
        (traffic.data?.torrent_activity ?? []).map((activity) => [
          activity.torrent.id,
          activity,
        ])
      ),
    [traffic.data?.torrent_activity]
  )
  const [adultCoversVisible] = useAdultCoverVisibility()

  React.useEffect(() => {
    const total = bookmarks.data?.total
    if (total === undefined || offset === 0 || offset < total) {
      return
    }
    setOffset(
      Math.max(
        0,
        Math.floor((Math.max(total, 1) - 1) / bookmarkPageSize) *
          bookmarkPageSize
      )
    )
  }, [bookmarks.data?.total, offset])

  return (
    <PageLayout className="gap-6">
      <h1 className="flex items-center gap-3 font-heading text-3xl font-bold">
        <BookmarkIcon className="size-8" />
        我的收藏
        {bookmarks.data && bookmarks.data.total > 0 ? (
          <span className="text-lg font-normal text-muted-foreground">
            ({bookmarks.data.total.toLocaleString("zh-CN")})
          </span>
        ) : null}
      </h1>

      {session.isPending && <TorrentListSkeleton />}

      {session.isError && (
        <BookmarkAlert
          title="会话状态暂时无法读取"
          description={requestErrorDescription(
            session.error,
            "会话请求未能完成，请稍后刷新页面。"
          )}
          retry={() => void session.refetch()}
        />
      )}

      {!session.isPending && !session.isError && !session.data && (
        <BookmarkAccessCard
          icon={<LogInIcon />}
          title="登录后查看收藏"
          description="收藏只对本人可见，登录后可以在不同设备上继续查看。"
          action={
            <Link to="/login" className={buttonVariants()}>
              <LogInIcon data-icon="inline-start" />
              前往登录
            </Link>
          }
        />
      )}

      {session.data && capabilities.isPending && <TorrentListSkeleton />}

      {session.data && capabilities.isError && (
        <BookmarkAlert
          title="暂时无法确认收藏权限"
          description={requestErrorDescription(
            capabilities.error,
            "权限请求未能完成，请稍后再试。"
          )}
          retry={() => void capabilities.refetch()}
        />
      )}

      {session.data && capabilities.data && !canRead && (
        <BookmarkAccessCard
          icon={<ShieldXIcon />}
          title="当前账户不能查看收藏"
          description="如有疑问，请联系站点管理人员。"
          action={
            <Link to="/" className={buttonVariants({ variant: "outline" })}>
              返回首页
            </Link>
          }
        />
      )}

      {session.data && canRead && bookmarks.isPending && (
        <TorrentListSkeleton />
      )}

      {session.data && canRead && bookmarks.isError && (
        <BookmarkAlert
          title="收藏暂时无法查看"
          description={requestErrorDescription(
            bookmarks.error,
            "收藏请求未能完成，请稍后再试。"
          )}
          retry={() => void bookmarks.refetch()}
        />
      )}

      {session.data && canRead && bookmarks.data && (
        <section aria-label="已收藏种子" className="flex flex-col gap-3">
          {bookmarks.data.items.length === 0 ? (
            <p className="py-8 text-center text-muted-foreground">
              暂无收藏的种子
            </p>
          ) : (
            <>
              <TorrentTable
                torrents={torrents}
                bookmarkControls={bookmarkControls}
                layout="bookmarks"
                adultCoversVisible={adultCoversVisible}
                activityByTorrentId={activityByTorrentId}
              />
              <TorrentCards
                torrents={torrents}
                poster={false}
                bookmarkControls={bookmarkControls}
                timestampByTorrentId={bookmarkTimes}
                adultCoversVisible={adultCoversVisible}
                activityByTorrentId={activityByTorrentId}
              />
              <OffsetPagination
                total={bookmarks.data.total}
                limit={bookmarkPageSize}
                offset={bookmarks.data.offset}
                onOffsetChange={setOffset}
                ariaLabel="收藏分页"
              />
            </>
          )}
        </section>
      )}
    </PageLayout>
  )
}

function BookmarkAlert({
  title,
  description,
  retry,
}: {
  title: string
  description: string
  retry: () => void
}) {
  return (
    <Alert variant="destructive">
      <CircleAlertIcon />
      <AlertTitle>{title}</AlertTitle>
      <AlertDescription>{description}</AlertDescription>
      <AlertAction>
        <Button type="button" variant="outline" size="sm" onClick={retry}>
          <RefreshCwIcon data-icon="inline-start" />
          重试
        </Button>
      </AlertAction>
    </Alert>
  )
}

function BookmarkAccessCard({
  icon,
  title,
  description,
  action,
}: {
  icon: React.ReactNode
  title: string
  description: string
  action: React.ReactNode
}) {
  return (
    <Card>
      <CardContent>
        <Empty>
          <EmptyHeader>
            <EmptyMedia variant="icon">{icon}</EmptyMedia>
            <EmptyTitle>{title}</EmptyTitle>
            <EmptyDescription>{description}</EmptyDescription>
          </EmptyHeader>
          <EmptyContent>{action}</EmptyContent>
        </Empty>
      </CardContent>
    </Card>
  )
}

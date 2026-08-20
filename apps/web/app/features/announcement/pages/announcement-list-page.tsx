import * as React from "react"
import { Link, useSearchParams } from "react-router"
import {
  CircleAlertIcon,
  NewspaperIcon,
  PinIcon,
  RefreshCwIcon,
} from "lucide-react"

import {
  Alert,
  AlertAction,
  AlertDescription,
  AlertTitle,
} from "~/components/ui/alert"
import { Button } from "~/components/ui/button"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "~/components/ui/card"
import { Skeleton } from "~/components/ui/skeleton"
import {
  useAnnouncementPage,
  useSiteInfo,
} from "~/features/site/api/site.queries"
import { OffsetPagination } from "~/shared/components/offset-pagination"
import { PageLayout } from "~/shared/components/page-layout"
import { formatCompactDateTime } from "~/shared/formatters/date-time"

const announcementPageSize = 20
const maximumAnnouncementOffset = 1_000_000

export function AnnouncementListPage() {
  const [searchParams, setSearchParams] = useSearchParams()
  const pageNumber = announcementPageNumber(searchParams.get("page"))
  const offset = (pageNumber - 1) * announcementPageSize
  const announcements = useAnnouncementPage(announcementPageSize, offset)
  const siteInfo = useSiteInfo()
  const publisherLabel = `${siteInfo.data?.name ?? "PeerGo"} 站务`

  React.useEffect(() => {
    if (
      !announcements.data ||
      offset === 0 ||
      offset < announcements.data.total
    ) {
      return
    }
    const lastOffset = Math.max(
      0,
      Math.floor(
        (Math.max(announcements.data.total, 1) - 1) / announcementPageSize
      ) * announcementPageSize
    )
    setAnnouncementOffset(lastOffset, true)
  }, [announcements.data, offset])

  function setAnnouncementOffset(nextOffset: number, replace = false) {
    const next = new URLSearchParams(searchParams)
    const nextPage = Math.floor(nextOffset / announcementPageSize) + 1
    if (nextPage <= 1) {
      next.delete("page")
    } else {
      next.set("page", String(nextPage))
    }
    setSearchParams(next, { replace, preventScrollReset: true })
  }

  return (
    <PageLayout className="gap-6">
      <h1 className="flex items-center gap-3 font-heading text-3xl font-bold">
        <NewspaperIcon className="size-8" />
        公告
      </h1>

      {announcements.isPending ? <AnnouncementListSkeleton /> : null}

      {announcements.isError ? (
        <Alert variant="destructive">
          <CircleAlertIcon />
          <AlertTitle>公告列表暂时无法读取</AlertTitle>
          <AlertDescription>
            首页和其他公开内容仍可继续使用，请稍后重试。
          </AlertDescription>
          <AlertAction>
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={() => void announcements.refetch()}
            >
              <RefreshCwIcon data-icon="inline-start" />
              重试
            </Button>
          </AlertAction>
        </Alert>
      ) : null}

      {announcements.data ? (
        announcements.data.items.length === 0 ? (
          <Card className="gap-0 rounded-lg border py-0 shadow-sm ring-0">
            <CardContent className="py-12 text-center text-muted-foreground">
              <NewspaperIcon className="mx-auto mb-4 size-12 opacity-50" />
              <p className="text-lg">暂无公告</p>
              <p className="mt-2 text-sm">目前还没有发布任何公告</p>
            </CardContent>
          </Card>
        ) : (
          <section aria-label="公告列表" className="flex flex-col gap-4">
            <ol className="flex flex-col gap-4">
              {announcements.data.items.map((announcement) => (
                <li key={announcement.id}>
                  <Link
                    to={`/announcements/${encodeURIComponent(announcement.id)}`}
                    className="block rounded-lg outline-none focus-visible:ring-2 focus-visible:ring-ring"
                  >
                    <Card className="cursor-pointer gap-0 rounded-lg border py-0 shadow-sm ring-0 transition-colors hover:border-primary">
                      <CardContent className="p-4">
                        <div className="mb-2 flex items-center gap-2">
                          <PinIcon className="size-3.5 shrink-0 fill-primary text-primary" />
                          <h2 className="text-lg font-semibold break-words">
                            {announcement.title}
                          </h2>
                        </div>
                        <p className="text-sm text-muted-foreground">
                          由 {publisherLabel} 发布于{" "}
                          <time dateTime={announcement.published_at}>
                            {formatCompactDateTime(announcement.published_at)}
                          </time>
                        </p>
                      </CardContent>
                    </Card>
                  </Link>
                </li>
              ))}
            </ol>

            <OffsetPagination
              total={announcements.data.total}
              limit={announcements.data.limit}
              offset={announcements.data.offset}
              onOffsetChange={setAnnouncementOffset}
              ariaLabel="公告分页"
            />
          </section>
        )
      ) : null}
    </PageLayout>
  )
}

function AnnouncementListSkeleton() {
  return (
    <Card size="sm" className="gap-0 py-0" aria-busy="true">
      <CardHeader className="border-b">
        <Skeleton className="h-5 w-28" />
        <Skeleton className="h-4 w-64 max-w-full" />
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        {Array.from({ length: 5 }, (_, index) => (
          <div key={index} className="flex flex-col gap-2">
            <Skeleton className="h-5 w-2/3" />
            <Skeleton className="h-4 w-full" />
          </div>
        ))}
      </CardContent>
    </Card>
  )
}

function announcementPageNumber(value: string | null) {
  if (!value || !/^\d+$/.test(value)) {
    return 1
  }
  const page = Number(value)
  const maximumPage =
    Math.floor(maximumAnnouncementOffset / announcementPageSize) + 1
  return Number.isSafeInteger(page) && page >= 1 && page <= maximumPage
    ? page
    : 1
}

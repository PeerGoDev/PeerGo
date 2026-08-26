import { Link, useParams } from "react-router"
import {
  ArrowLeftIcon,
  CircleAlertIcon,
  MegaphoneIcon,
  PinIcon,
  RefreshCwIcon,
} from "lucide-react"

import { Alert, AlertDescription, AlertTitle } from "~/components/ui/alert"
import { Button } from "~/components/ui/button"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "~/components/ui/card"
import { Skeleton } from "~/components/ui/skeleton"
import { AnnouncementBody } from "~/features/announcement/components/announcement-body"
import { announcementCommentTarget } from "~/features/social/api/comments.queries"
import { CommentThreadCard } from "~/features/social/components/comment-thread-card"
import {
  isAnnouncementId,
  useAnnouncement,
  useSiteInfo,
} from "~/features/site/api/site.queries"
import { ApiProblemError } from "~/shared/api/problem"
import { PageLayout } from "~/shared/components/page-layout"
import { formatCompactDateTime } from "~/shared/formatters/date-time"

export function AnnouncementDetailPage() {
  const { announcementId = "" } = useParams()
  const validAnnouncementId = isAnnouncementId(announcementId)
  const announcement = useAnnouncement(announcementId, validAnnouncementId)
  const siteInfo = useSiteInfo()
  const publisherLabel = `${siteInfo.data?.name ?? "PeerGo"} 站务`

  if (!validAnnouncementId) {
    return (
      <AnnouncementLayout>
        <AnnouncementUnavailable invalidId />
      </AnnouncementLayout>
    )
  }
  if (announcement.isPending) {
    return (
      <AnnouncementLayout>
        <AnnouncementSkeleton />
      </AnnouncementLayout>
    )
  }
  if (announcement.isError) {
    return (
      <AnnouncementLayout>
        <AnnouncementUnavailable
          notFound={
            announcement.error instanceof ApiProblemError &&
            announcement.error.status === 404
          }
          retry={() => void announcement.refetch()}
        />
      </AnnouncementLayout>
    )
  }
  if (!announcement.data) return null

  return (
    <AnnouncementLayout>
      <div>
        <Button
          variant="ghost"
          size="sm"
          className="mb-4 w-fit"
          nativeButton={false}
          render={<Link to="/announcements" />}
        >
          <ArrowLeftIcon data-icon="inline-start" />
          返回公告列表
        </Button>

        <article>
          <Card className="gap-0 py-0">
            <CardHeader className="p-6">
              <CardTitle className="flex items-start gap-2 text-2xl leading-none font-semibold tracking-tight">
                <PinIcon className="mt-0.5 size-4 shrink-0 text-primary" />
                <h1 className="break-words">{announcement.data.title}</h1>
              </CardTitle>
              <CardDescription>
                由 {publisherLabel} 发布于{" "}
                {formatCompactDateTime(announcement.data.published_at)}
                {announcement.data.updated_at !== announcement.data.published_at
                  ? ` · 更新于 ${formatCompactDateTime(announcement.data.updated_at)}`
                  : null}
              </CardDescription>
            </CardHeader>
            <CardContent className="p-6 pt-0">
              {announcement.data.body_format === "legacy_bbcode" ? (
                <Alert className="mb-4">
                  <MegaphoneIcon />
                  <AlertTitle>原文格式提示</AlertTitle>
                  <AlertDescription>
                    这篇公告来自旧版文本格式，当前按纯文本显示，部分排版可能有所不同。
                  </AlertDescription>
                </Alert>
              ) : null}
              <AnnouncementBody
                body={announcement.data.body}
                legacy={announcement.data.body_format === "legacy_bbcode"}
              />
              <CommentThreadCard
                target={announcementCommentTarget(announcement.data.id)}
                description="围绕公告内容进行讨论。"
                composerPlaceholder="发表评论..."
                appearance="announcement"
              />
            </CardContent>
          </Card>
        </article>
      </div>
    </AnnouncementLayout>
  )
}

function AnnouncementUnavailable({
  invalidId = false,
  notFound = false,
  retry,
}: {
  invalidId?: boolean
  notFound?: boolean
  retry?: () => void
}) {
  const unavailable = invalidId || notFound
  return (
    <div className="flex items-center justify-center py-12">
      <Card className="w-full max-w-md py-0">
        <CardContent className="p-6">
          <div className="mb-4 flex items-center gap-3 text-destructive">
            <CircleAlertIcon className="size-6" />
            <h2 className="font-heading font-semibold">
              {unavailable ? "公告不存在" : "加载失败"}
            </h2>
          </div>
          <p className="mb-4 text-muted-foreground">
            {unavailable
              ? "公告不存在或已被删除"
              : "公告暂时无法读取，请稍后重试。"}
          </p>
          <div className="flex gap-2">
            <Button
              variant="outline"
              className="flex-1"
              nativeButton={false}
              render={<Link to="/announcements" />}
            >
              返回公告列表
            </Button>
            {!unavailable && retry ? (
              <Button className="flex-1" variant="outline" onClick={retry}>
                <RefreshCwIcon data-icon="inline-start" />
                重试
              </Button>
            ) : null}
          </div>
        </CardContent>
      </Card>
    </div>
  )
}

function AnnouncementSkeleton() {
  return (
    <div className="flex flex-col gap-6" aria-busy="true">
      <Skeleton className="h-7 w-24" />
      <Card>
        <CardHeader>
          <Skeleton className="h-5 w-24" />
          <Skeleton className="h-9 w-4/5" />
          <Skeleton className="h-4 w-2/5" />
        </CardHeader>
        <CardContent className="flex flex-col gap-3">
          <Skeleton className="h-4 w-full" />
          <Skeleton className="h-4 w-11/12" />
          <Skeleton className="h-4 w-3/4" />
        </CardContent>
      </Card>
      <Skeleton className="h-72 rounded-xl" />
    </div>
  )
}

function AnnouncementLayout({ children }: { children: React.ReactNode }) {
  return <PageLayout className="gap-6">{children}</PageLayout>
}

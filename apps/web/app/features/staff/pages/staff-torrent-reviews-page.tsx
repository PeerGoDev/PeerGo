import * as React from "react"
import { useQuery } from "@tanstack/react-query"
import {
  CircleAlertIcon,
  ClipboardCheckIcon,
  Clock3Icon,
  FileTextIcon,
  RefreshCwIcon,
  ShieldCheckIcon,
  UserRoundIcon,
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
import {
  type PendingTorrentReview,
  pendingTorrentReviewsQueryOptions,
} from "~/features/staff/api/torrent-review.queries"
import { StaffAccessGate } from "~/features/staff/components/staff-access-gate"
import { TorrentReviewDecisionDialog } from "~/features/staff/components/torrent-review-decision-dialog"
import { PageLayout } from "~/shared/components/page-layout"
import { formatBytes } from "~/shared/formatters/bytes"
import { formatDateTime } from "~/shared/formatters/date-time"

const reviewQueueLimit = 20

export function StaffTorrentReviewsPage() {
  return (
    <PageLayout className="max-w-[1172px] gap-6 px-10 py-6 pt-10! sm:px-6 sm:pt-12!">
      <header>
        <h1 className="font-heading text-2xl font-bold tracking-tight">
          种子审核终审
        </h1>
        <p className="mt-1 text-sm text-muted-foreground">
          处理工作组四票 2:2 的升级事项，以及必要的管理员直接结案
        </p>
      </header>

      <StaffAccessGate requiredAction="torrent.review" layout="embedded">
        {({ session }) => (
          <TorrentReviewQueueContent csrfToken={session.csrf_token} />
        )}
      </StaffAccessGate>
    </PageLayout>
  )
}

function TorrentReviewQueueContent({ csrfToken }: { csrfToken: string }) {
  const [decisionTarget, setDecisionTarget] =
    React.useState<PendingTorrentReview>()
  const [successMessage, setSuccessMessage] = React.useState("")
  const reviews = useQuery(pendingTorrentReviewsQueryOptions(reviewQueueLimit))

  return (
    <div className="flex flex-col gap-6">
      <div className="flex justify-end">
        <Button
          type="button"
          variant="outline"
          size="default"
          disabled={reviews.isFetching}
          onClick={() => void reviews.refetch()}
        >
          <RefreshCwIcon data-icon="inline-start" />
          {reviews.isFetching ? "刷新中…" : "刷新队列"}
        </Button>
      </div>

      {successMessage ? (
        <Alert>
          <ShieldCheckIcon />
          <AlertTitle>审核决定已保存</AlertTitle>
          <AlertDescription>{successMessage}</AlertDescription>
        </Alert>
      ) : null}

      {reviews.isPending ? <TorrentReviewQueueSkeleton /> : null}

      {reviews.isError ? (
        <Alert variant="destructive">
          <CircleAlertIcon />
          <AlertTitle>种子审核队列暂时无法读取</AlertTitle>
          <AlertDescription>
            后台会话或队列数据已经变化，请重新载入后再试。
          </AlertDescription>
          <AlertAction>
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={() => void reviews.refetch()}
            >
              <RefreshCwIcon data-icon="inline-start" />
              重试
            </Button>
          </AlertAction>
        </Alert>
      ) : null}

      {reviews.data ? (
        <section aria-label="待审核种子" className="flex flex-col gap-6">
          <div className="flex items-end">
            <div className="flex h-9 items-center gap-2 rounded-lg bg-primary px-3.5 text-sm font-medium text-primary-foreground">
              <Clock3Icon className="size-3.5" />
              待审核
              <Badge
                variant="destructiveSolid"
                className="h-5 min-w-5 rounded-full px-1.5"
              >
                {reviews.data.total.toLocaleString("zh-CN")}
              </Badge>
            </div>
          </div>

          {reviews.data.items.length === 0 ? (
            <Card className="gap-0 py-0">
              <CardContent className="p-0">
                <Empty className="min-h-60 border-0 py-12">
                  <EmptyHeader>
                    <EmptyMedia className="text-muted-foreground/60 [&_svg]:size-12">
                      <ClipboardCheckIcon />
                    </EmptyMedia>
                    <EmptyTitle className="text-base text-muted-foreground">
                      当前没有待审核的种子
                    </EmptyTitle>
                    <EmptyDescription>
                      新提交或重新送审的种子会按时间进入这里。
                    </EmptyDescription>
                  </EmptyHeader>
                </Empty>
              </CardContent>
            </Card>
          ) : (
            <div className="flex flex-col gap-4">
              {reviews.data.items.map((torrent) => (
                <Card key={torrent.id} className="h-[130px] gap-0 py-0">
                  <CardContent className="h-full p-4">
                    <article className="flex h-full flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
                      <div className="min-w-0 flex-1">
                        <div className="flex flex-wrap items-center gap-2">
                          <Badge variant="secondary">
                            {torrent.category_name}
                          </Badge>
                          <h2 className="min-w-0 truncate font-medium">
                            {torrent.title}
                          </h2>
                        </div>
                        <p className="mt-1 truncate text-sm text-muted-foreground">
                          {torrent.subtitle || torrent.content_name}
                        </p>
                        <div className="mt-3 flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-muted-foreground">
                          <span className="flex items-center gap-1 tabular-nums">
                            <FileTextIcon className="size-3" />
                            {formatBytes(torrent.total_size_bytes)}
                          </span>
                          <span className="flex items-center gap-1">
                            <UserRoundIcon className="size-3" />
                            {torrent.uploader_display_name}
                          </span>
                          <time
                            dateTime={torrent.review_requested_at}
                            className="flex items-center gap-1"
                          >
                            <Clock3Icon className="size-3" />
                            {formatDateTime(torrent.review_requested_at)}
                          </time>
                          <span className="tabular-nums">
                            {torrent.file_count.toLocaleString("zh-CN")} 个文件
                          </span>
                        </div>
                      </div>
                      <Button
                        type="button"
                        size="sm"
                        className="self-start sm:self-center"
                        onClick={() => {
                          setSuccessMessage("")
                          setDecisionTarget(torrent)
                        }}
                      >
                        最终处理
                      </Button>
                    </article>
                  </CardContent>
                </Card>
              ))}
            </div>
          )}
        </section>
      ) : null}

      {decisionTarget ? (
        <TorrentReviewDecisionDialog
          torrent={decisionTarget}
          csrfToken={csrfToken}
          onOpenChange={(open) => {
            if (!open) setDecisionTarget(undefined)
          }}
          onDecided={(result) => {
            setDecisionTarget(undefined)
            setSuccessMessage(
              result.state === "published"
                ? "种子已通过并发布。"
                : "种子已驳回，上传者会在自己的记录中看到反馈。"
            )
          }}
        />
      ) : null}
    </div>
  )
}

function TorrentReviewQueueSkeleton() {
  return (
    <Card aria-label="正在加载种子审核队列" aria-busy="true">
      <CardHeader>
        <CardTitle>
          <Skeleton className="h-5 w-28" />
        </CardTitle>
        <CardDescription>
          <Skeleton className="h-4 w-72 max-w-full" />
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        {Array.from({ length: 5 }, (_, index) => (
          <Skeleton key={index} className="h-24 w-full" />
        ))}
      </CardContent>
    </Card>
  )
}

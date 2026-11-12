import * as React from "react"
import { useQuery } from "@tanstack/react-query"
import { Link } from "react-router"
import {
  CircleAlertIcon,
  CircleCheckIcon,
  ClipboardCheckIcon,
  Clock3Icon,
  FileTextIcon,
  HistoryIcon,
  LogInIcon,
  RefreshCwIcon,
  ThumbsDownIcon,
  ThumbsUpIcon,
  UserRoundIcon,
} from "lucide-react"

import {
  Alert,
  AlertAction,
  AlertDescription,
  AlertTitle,
} from "~/components/ui/alert"
import { Badge } from "~/components/ui/badge"
import { Button, buttonVariants } from "~/components/ui/button"
import { Card, CardContent } from "~/components/ui/card"
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "~/components/ui/empty"
import { Skeleton } from "~/components/ui/skeleton"
import { ToggleGroup, ToggleGroupItem } from "~/components/ui/toggle-group"
import { useWebSession } from "~/features/auth/api/session.mutations"
import {
  type MyTorrentReviewAssignment,
  myTorrentReviewAssignmentsQueryOptions,
  myReviewedTorrentReviewsQueryOptions,
  type ReviewedTorrentReview,
} from "~/features/review/api/torrent-review-voting.queries"
import { ReviewCenterNavigation } from "~/features/torrent/components/review-center-navigation"
import { ApiProblemError, requestErrorDescription } from "~/shared/api/problem"
import { PageHeader, PageLayout } from "~/shared/components/page-layout"
import { formatBytes } from "~/shared/formatters/bytes"
import { formatDateTime } from "~/shared/formatters/date-time"

const reviewQueueLimit = 20

export function TorrentReviewQueuePage() {
  const session = useWebSession()
  const [view, setView] = React.useState<"pending" | "reviewed">("pending")
  const reviews = useQuery({
    ...myTorrentReviewAssignmentsQueryOptions(reviewQueueLimit),
    enabled: Boolean(session.data) && view === "pending",
  })
  const history = useQuery({
    ...myReviewedTorrentReviewsQueryOptions(reviewQueueLimit),
    enabled: Boolean(session.data) && view === "reviewed",
  })

  return (
    <PageLayout>
      <ReviewCenterNavigation canReview />

      <PageHeader
        title="审核队列"
        description="打开完整发布资料后独立判断；提交前不会显示赞成与反对票分布"
      />

      {session.isPending ? <TorrentReviewQueueSkeleton /> : null}
      {session.isError ? (
        <Alert variant="destructive">
          <CircleAlertIcon />
          <AlertTitle>会话状态暂时无法读取</AlertTitle>
          <AlertDescription>
            {requestErrorDescription(session.error)}
          </AlertDescription>
        </Alert>
      ) : null}
      {!session.isPending && !session.isError && !session.data ? (
        <Card>
          <CardContent className="flex min-h-48 flex-col items-center justify-center gap-4 text-center">
            <p className="text-sm text-muted-foreground">
              登录后才能查看自己的种审任务。
            </p>
            <Link to="/login" className={buttonVariants()}>
              <LogInIcon data-icon="inline-start" />
              前往登录
            </Link>
          </CardContent>
        </Card>
      ) : null}

      {session.data ? (
        <div className="flex flex-col gap-4">
          <div className="flex items-center justify-between gap-3">
            <ToggleGroup
              value={[view]}
              onValueChange={(values) => {
                const selected = values[0] as typeof view | undefined
                if (selected) setView(selected)
              }}
              variant="segment"
              size="sm"
              spacing={0.5}
              aria-label="切换种审记录"
            >
              <ToggleGroupItem value="pending">
                <Clock3Icon data-icon="inline-start" />
                待审核
                {reviews.data ? (
                  <Badge
                    variant={
                      reviews.data.total > 0 ? "destructiveSolid" : "secondary"
                    }
                    className="h-4 min-w-4 rounded-full px-1 tabular-nums"
                  >
                    {reviews.data.total.toLocaleString("zh-CN")}
                  </Badge>
                ) : null}
              </ToggleGroupItem>
              <ToggleGroupItem value="reviewed">
                <HistoryIcon data-icon="inline-start" />
                已审核
                {history.data ? (
                  <Badge
                    variant="secondary"
                    className="h-4 min-w-4 rounded-full px-1 tabular-nums"
                  >
                    {history.data.total.toLocaleString("zh-CN")}
                  </Badge>
                ) : null}
              </ToggleGroupItem>
            </ToggleGroup>
            <Button
              type="button"
              variant="outline"
              size="sm"
              disabled={
                view === "pending" ? reviews.isFetching : history.isFetching
              }
              onClick={() =>
                void (view === "pending"
                  ? reviews.refetch()
                  : history.refetch())
              }
            >
              <RefreshCwIcon data-icon="inline-start" />
              {(view === "pending" ? reviews.isFetching : history.isFetching)
                ? "刷新中…"
                : "刷新"}
            </Button>
          </div>

          {view === "pending" && reviews.isPending ? (
            <TorrentReviewQueueSkeleton />
          ) : null}
          {view === "pending" && reviews.isError ? (
            <ReviewQueueError error={reviews.error} retry={reviews.refetch} />
          ) : null}
          {view === "pending" && reviews.data ? (
            <section aria-label="种审任务" className="flex flex-col gap-3">
              {reviews.data.items.length === 0 ? (
                <Empty className="min-h-60 border">
                  <EmptyHeader>
                    <EmptyMedia variant="icon">
                      <ClipboardCheckIcon />
                    </EmptyMedia>
                    <EmptyTitle>当前没有待参与的种审任务</EmptyTitle>
                    <EmptyDescription>
                      新种子进入审核队列后会显示在这里。
                    </EmptyDescription>
                  </EmptyHeader>
                </Empty>
              ) : (
                <div className="flex flex-col gap-3">
                  {reviews.data.items.map((torrent) => (
                    <ReviewAssignmentCard key={torrent.id} torrent={torrent} />
                  ))}
                </div>
              )}
            </section>
          ) : null}
          {view === "reviewed" && history.isPending ? (
            <TorrentReviewQueueSkeleton />
          ) : null}
          {view === "reviewed" && history.isError ? (
            <ReviewQueueError error={history.error} retry={history.refetch} />
          ) : null}
          {view === "reviewed" && history.data ? (
            <ReviewedTorrentList items={history.data.items} />
          ) : null}
        </div>
      ) : null}
    </PageLayout>
  )
}

function ReviewAssignmentCard({
  torrent,
}: {
  torrent: MyTorrentReviewAssignment
}) {
  return (
    <Card className="gap-0 py-0">
      <CardContent className="h-full p-4">
        <article className="flex h-full flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
          <div className="min-w-0 flex-1">
            <div className="flex flex-wrap items-center gap-2">
              <Badge variant="secondary">{torrent.category_name}</Badge>
              <h2 className="min-w-0 truncate font-medium">{torrent.title}</h2>
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
                已投 {torrent.votes_cast}/{torrent.maximum_votes} 票
              </span>
            </div>
          </div>
          <Button
            nativeButton={false}
            size="sm"
            className="self-start sm:self-center"
            render={<Link to={`/review/torrent/${torrent.id}`} />}
          >
            参与审核
          </Button>
        </article>
      </CardContent>
    </Card>
  )
}

function ReviewedTorrentList({ items }: { items: ReviewedTorrentReview[] }) {
  if (items.length === 0) {
    return (
      <Empty className="min-h-60 border">
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <HistoryIcon />
          </EmptyMedia>
          <EmptyTitle>还没有审核记录</EmptyTitle>
          <EmptyDescription>提交审核票后会在这里看到结果。</EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  }
  return (
    <section aria-label="已审核种子" className="flex flex-col gap-3">
      {items.map((torrent) => (
        <Card key={torrent.vote_id} className="gap-0 py-0">
          <CardContent className="p-4">
            <article className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
              <div className="min-w-0 flex-1">
                <div className="flex flex-wrap items-center gap-2">
                  <Badge variant="secondary">{torrent.category_name}</Badge>
                  <h2 className="font-medium">{torrent.title}</h2>
                  <Badge
                    variant={
                      torrent.decision === "approve" ? "success" : "destructive"
                    }
                  >
                    {torrent.decision === "approve" ? (
                      <ThumbsUpIcon data-icon="inline-start" />
                    ) : (
                      <ThumbsDownIcon data-icon="inline-start" />
                    )}
                    我的票：{torrent.decision === "approve" ? "同意" : "拒绝"}
                  </Badge>
                </div>
                <p className="mt-1 text-sm text-muted-foreground">
                  {torrent.reason}
                </p>
                <div className="mt-3 flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground">
                  <span>同意 {torrent.approve_count} 票</span>
                  <span>拒绝 {torrent.reject_count} 票</span>
                  <time dateTime={torrent.voted_at}>
                    {formatDateTime(torrent.voted_at)}
                  </time>
                </div>
              </div>
              <Badge variant={reviewOutcomeVariant(torrent.outcome)}>
                <CircleCheckIcon data-icon="inline-start" />
                {reviewOutcomeLabel(torrent.outcome)}
              </Badge>
            </article>
          </CardContent>
        </Card>
      ))}
    </section>
  )
}

function reviewOutcomeLabel(outcome: ReviewedTorrentReview["outcome"]) {
  switch (outcome) {
    case "published":
      return "已发布"
    case "rejected":
      return "已驳回"
    case "escalated":
      return "管理员复核"
    default:
      return "等待其他审核员"
  }
}

function reviewOutcomeVariant(outcome: ReviewedTorrentReview["outcome"]) {
  switch (outcome) {
    case "published":
      return "success" as const
    case "rejected":
      return "destructive" as const
    default:
      return "secondary" as const
  }
}

function ReviewQueueError({
  error,
  retry,
}: {
  error: Error
  retry: () => unknown
}) {
  const membershipRequired =
    error instanceof ApiProblemError &&
    error.code === "torrent_review_membership_required"
  return (
    <Alert variant={membershipRequired ? "default" : "destructive"}>
      <CircleAlertIcon />
      <AlertTitle>
        {membershipRequired ? "需要有效种审组资格" : "种审任务暂时无法读取"}
      </AlertTitle>
      <AlertDescription>
        {membershipRequired
          ? "通过种审组申请并由管理员批准后，才能参与种子审核。"
          : requestErrorDescription(error)}
      </AlertDescription>
      <AlertAction>
        {membershipRequired ? (
          <Link
            to="/workgroups"
            className={buttonVariants({ variant: "outline", size: "sm" })}
          >
            查看工作组
          </Link>
        ) : (
          <Button type="button" variant="outline" size="sm" onClick={retry}>
            <RefreshCwIcon data-icon="inline-start" />
            重试
          </Button>
        )}
      </AlertAction>
    </Alert>
  )
}

function TorrentReviewQueueSkeleton() {
  return (
    <div className="flex flex-col gap-3" aria-label="正在加载种审任务">
      {[0, 1, 2].map((item) => (
        <Skeleton key={item} className="h-28 w-full rounded-xl" />
      ))}
    </div>
  )
}

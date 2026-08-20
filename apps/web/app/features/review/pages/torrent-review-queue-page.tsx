import * as React from "react"
import { useQuery } from "@tanstack/react-query"
import { Link } from "react-router"
import {
  CircleAlertIcon,
  ClipboardCheckIcon,
  Clock3Icon,
  FileTextIcon,
  LogInIcon,
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
import { useWebSession } from "~/features/auth/api/session.mutations"
import {
  type MyTorrentReviewAssignment,
  myTorrentReviewAssignmentsQueryOptions,
  type TorrentReviewVoteResult,
} from "~/features/review/api/torrent-review-voting.queries"
import { TorrentReviewVoteDialog } from "~/features/review/components/torrent-review-vote-dialog"
import { ReviewCenterNavigation } from "~/features/torrent/components/review-center-navigation"
import { ApiProblemError, requestErrorDescription } from "~/shared/api/problem"
import { PageLayout } from "~/shared/components/page-layout"
import { formatBytes } from "~/shared/formatters/bytes"
import { formatDateTime } from "~/shared/formatters/date-time"

const reviewQueueLimit = 20

export function TorrentReviewQueuePage() {
  const session = useWebSession()
  const [voteTarget, setVoteTarget] =
    React.useState<MyTorrentReviewAssignment>()
  const [successMessage, setSuccessMessage] = React.useState("")
  const reviews = useQuery({
    ...myTorrentReviewAssignmentsQueryOptions(reviewQueueLimit),
    enabled: Boolean(session.data),
  })

  return (
    <PageLayout className="max-w-[1172px] gap-6 px-10 py-6 pt-10! sm:px-6 sm:pt-12!">
      <ReviewCenterNavigation canReview />

      <header>
        <h1 className="font-heading text-2xl font-bold tracking-tight">
          种审任务
        </h1>
        <p className="mt-1 text-sm text-muted-foreground">
          仅显示您尚未投票的种子；投票分布在提交前保持隐藏
        </p>
      </header>

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
        <div className="flex flex-col gap-6">
          <div className="flex justify-end">
            <Button
              type="button"
              variant="outline"
              disabled={reviews.isFetching}
              onClick={() => void reviews.refetch()}
            >
              <RefreshCwIcon data-icon="inline-start" />
              {reviews.isFetching ? "刷新中…" : "刷新任务"}
            </Button>
          </div>

          {successMessage ? (
            <Alert className="border-success/40 bg-success/10">
              <ShieldCheckIcon className="text-success" />
              <AlertTitle>种审票已保存</AlertTitle>
              <AlertDescription>{successMessage}</AlertDescription>
            </Alert>
          ) : null}

          {reviews.isPending ? <TorrentReviewQueueSkeleton /> : null}
          {reviews.isError ? (
            <ReviewQueueError error={reviews.error} retry={reviews.refetch} />
          ) : null}
          {reviews.data ? (
            <section aria-label="种审任务" className="flex flex-col gap-6">
              <div className="flex items-end border-b">
                <div className="-mb-px flex h-[38px] items-center gap-2 border-b-2 border-primary px-4 text-sm font-medium text-primary">
                  <Clock3Icon className="size-3.5" />
                  待参与
                  <Badge
                    variant="destructiveSolid"
                    className="h-5 min-w-5 rounded-full px-1.5"
                  >
                    {reviews.data.total.toLocaleString("zh-CN")}
                  </Badge>
                </div>
              </div>

              {reviews.data.items.length === 0 ? (
                <Card className="gap-0 rounded-lg py-0 shadow-sm">
                  <CardContent className="p-0">
                    <Empty className="min-h-60 border-0 py-12">
                      <EmptyHeader>
                        <EmptyMedia className="text-muted-foreground/60 [&_svg]:size-12">
                          <ClipboardCheckIcon />
                        </EmptyMedia>
                        <EmptyTitle className="text-base text-muted-foreground">
                          当前没有待参与的种审任务
                        </EmptyTitle>
                        <EmptyDescription>
                          新种子进入审核队列后会显示在这里。
                        </EmptyDescription>
                      </EmptyHeader>
                    </Empty>
                  </CardContent>
                </Card>
              ) : (
                <div className="flex flex-col gap-4">
                  {reviews.data.items.map((torrent) => (
                    <ReviewAssignmentCard
                      key={torrent.id}
                      torrent={torrent}
                      onReview={() => {
                        setSuccessMessage("")
                        setVoteTarget(torrent)
                      }}
                    />
                  ))}
                </div>
              )}
            </section>
          ) : null}
        </div>
      ) : null}

      {session.data && voteTarget ? (
        <TorrentReviewVoteDialog
          torrent={voteTarget}
          csrfToken={session.data.csrf_token}
          onOpenChange={(open) => {
            if (!open) setVoteTarget(undefined)
          }}
          onVoted={(result) => {
            setVoteTarget(undefined)
            setSuccessMessage(voteSuccessMessage(result))
          }}
        />
      ) : null}
    </PageLayout>
  )
}

function ReviewAssignmentCard({
  torrent,
  onReview,
}: {
  torrent: MyTorrentReviewAssignment
  onReview: () => void
}) {
  return (
    <Card className="min-h-[130px] gap-0 rounded-lg py-0 shadow-sm">
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
            type="button"
            size="sm"
            className="self-start sm:self-center"
            onClick={onReview}
          >
            参与审核
          </Button>
        </article>
      </CardContent>
    </Card>
  )
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

function voteSuccessMessage(result: TorrentReviewVoteResult) {
  switch (result.outcome) {
    case "published":
      return "本票触发通过条件，种子已经发布。"
    case "rejected":
      return "本票触发驳回条件，上传者会收到审核反馈。"
    case "escalated":
      return "当前四票形成 2:2，本轮已经转管理员最终处理。"
    default:
      return `本轮当前已投 ${result.votes_cast}/${result.maximum_votes} 票，请等待其他审核员独立判断。`
  }
}

function TorrentReviewQueueSkeleton() {
  return (
    <div className="space-y-4" aria-label="正在加载种审任务">
      <Skeleton className="h-10 w-full" />
      {[0, 1, 2].map((item) => (
        <Skeleton key={item} className="h-[130px] w-full rounded-lg" />
      ))}
    </div>
  )
}

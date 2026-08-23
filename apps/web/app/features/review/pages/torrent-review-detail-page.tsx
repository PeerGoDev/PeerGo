import * as React from "react"
import { useQuery } from "@tanstack/react-query"
import { Link, useParams } from "react-router"
import {
  ArrowLeftIcon,
  CircleAlertIcon,
  CircleCheckIcon,
  FileArchiveIcon,
  ImageIcon,
  RefreshCwIcon,
  ShieldCheckIcon,
  ThumbsDownIcon,
  ThumbsUpIcon,
  UserRoundIcon,
} from "lucide-react"

import { Alert, AlertDescription, AlertTitle } from "~/components/ui/alert"
import { Badge } from "~/components/ui/badge"
import { Button, buttonVariants } from "~/components/ui/button"
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
import {
  Field,
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLabel,
} from "~/components/ui/field"
import {
  Progress,
  ProgressLabel,
  ProgressValue,
} from "~/components/ui/progress"
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectLabel,
  SelectTrigger,
  SelectValue,
} from "~/components/ui/select"
import { Skeleton } from "~/components/ui/skeleton"
import { Spinner } from "~/components/ui/spinner"
import { Textarea } from "~/components/ui/textarea"
import { ToggleGroup, ToggleGroupItem } from "~/components/ui/toggle-group"
import { useWebSession } from "~/features/auth/api/session.mutations"
import {
  type MyTorrentReviewDetail,
  myTorrentReviewDetailQueryOptions,
  myTorrentReviewFilesQueryOptions,
  type TorrentReviewDecisionRequest,
  type TorrentReviewVoteResult,
  useCreateTorrentReviewVote,
} from "~/features/review/api/torrent-review-voting.queries"
import { TorrentRichContentView } from "~/features/torrent/components/torrent-rich-content"
import { resolveApiUrl } from "~/shared/api/client"
import { ApiProblemError, requestErrorDescription } from "~/shared/api/problem"
import { PageLayout } from "~/shared/components/page-layout"
import { OffsetPagination } from "~/shared/components/offset-pagination"
import { formatBytes } from "~/shared/formatters/bytes"
import { formatDateTime } from "~/shared/formatters/date-time"

const filePageSize = 50
const minimumReasonCharacters = 10
const maximumReasonCharacters = 1_000

type ReviewDecision = TorrentReviewDecisionRequest["decision"]
type ReviewReasonCode = TorrentReviewDecisionRequest["reason_code"]

const rejectionReasonOptions: Array<{
  value: Exclude<ReviewReasonCode, "meets_requirements">
  label: string
}> = [
  { value: "metadata_incomplete", label: "元数据不完整" },
  { value: "duplicate_or_superseded", label: "重复或已有替代版本" },
  { value: "content_policy_violation", label: "不符合内容政策" },
  { value: "quality_requirements_not_met", label: "未达到质量要求" },
  { value: "uploader_action_required", label: "需要发布者处理" },
  { value: "other", label: "其他原因" },
]

export function TorrentReviewDetailPage() {
  const { torrentId: rawTorrentId } = useParams()
  const torrentId = Number(rawTorrentId)
  const validTorrentId = Number.isSafeInteger(torrentId) && torrentId > 0
  const session = useWebSession()
  const [voteResult, setVoteResult] = React.useState<TorrentReviewVoteResult>()
  const detail = useQuery({
    ...myTorrentReviewDetailQueryOptions(validTorrentId ? torrentId : 0),
    enabled: Boolean(session.data) && validTorrentId && !voteResult,
  })

  if (session.isPending || (session.data && detail.isPending && !voteResult)) {
    return <ReviewDetailSkeleton />
  }
  if (!session.data) {
    return (
      <PageLayout className="max-w-[1280px] gap-5 px-6 py-10">
        <ReviewDetailProblem
          title="需要登录"
          description="登录后才能读取待审种子的完整资料。"
          action={
            <Link to="/login" className={buttonVariants()}>
              前往登录
            </Link>
          }
        />
      </PageLayout>
    )
  }
  if (voteResult) {
    return (
      <PageLayout className="max-w-[900px] gap-5 px-6 py-10">
        <ReviewVoteSuccess result={voteResult} />
      </PageLayout>
    )
  }
  if (!validTorrentId || detail.isError || !detail.data) {
    const assignmentGone =
      detail.error instanceof ApiProblemError &&
      detail.error.code === "torrent_review_not_found"
    return (
      <PageLayout className="max-w-[1280px] gap-5 px-6 py-10">
        <ReviewDetailProblem
          title={assignmentGone ? "该审核任务已结束" : "审核资料暂时无法读取"}
          description={
            assignmentGone
              ? "该种子可能已经结案、转管理员处理，或您已在本轮投过票。"
              : validTorrentId
                ? requestErrorDescription(detail.error)
                : "种子编号无效。"
          }
          action={
            assignmentGone || !validTorrentId ? (
              <Link to="/review/queue" className={buttonVariants()}>
                返回审核队列
              </Link>
            ) : (
              <Button onClick={() => void detail.refetch()}>
                <RefreshCwIcon data-icon="inline-start" />
                重试
              </Button>
            )
          }
        />
      </PageLayout>
    )
  }

  return (
    <PageLayout className="max-w-[1320px] gap-5 px-6 py-8">
      <Button
        nativeButton={false}
        variant="ghost"
        size="sm"
        className="w-fit"
        render={<Link to="/review/queue" />}
      >
        <ArrowLeftIcon data-icon="inline-start" />
        返回审核队列
      </Button>
      <div className="grid items-start gap-5 lg:grid-cols-[minmax(0,1fr)_340px]">
        <main className="min-w-0 space-y-5">
          <ReviewEvidenceSummary detail={detail.data} />
          <TorrentRichContentView
            torrentId={detail.data.id}
            screenshotCount={detail.data.screenshot_count}
            mediaInfo={detail.data.media_info}
            description={detail.data.description}
            descriptionFormat={detail.data.description_format}
            screenshotUrl={reviewScreenshotUrl}
          />
          <ReviewFilesCard
            torrentId={detail.data.id}
            infoHash={detail.data.info_hash_v1}
          />
        </main>
        <aside className="lg:sticky lg:top-5">
          <ReviewVotePanel
            torrent={detail.data}
            csrfToken={session.data.csrf_token}
            onVoted={setVoteResult}
          />
        </aside>
      </div>
    </PageLayout>
  )
}

function ReviewEvidenceSummary({ detail }: { detail: MyTorrentReviewDetail }) {
  const [coverFailed, setCoverFailed] = React.useState(false)
  return (
    <Card className="gap-0 rounded-lg py-0 shadow-sm">
      <CardHeader className="border-b p-5">
        <div className="flex flex-wrap items-center gap-2">
          <Badge variant="secondary">{detail.category_name}</Badge>
          <Badge variant="outline">待审核</Badge>
        </div>
        <CardTitle className="text-xl leading-snug">{detail.title}</CardTitle>
        {detail.subtitle ? (
          <CardDescription>{detail.subtitle}</CardDescription>
        ) : null}
      </CardHeader>
      <CardContent className="grid gap-5 p-5 sm:grid-cols-[160px_minmax(0,1fr)]">
        <div className="relative aspect-[2/3] overflow-hidden rounded-lg border bg-muted">
          {!coverFailed && detail.screenshot_count > 0 ? (
            <img
              src={reviewCoverUrl(detail.id)}
              alt={detail.title + "封面"}
              className="size-full object-cover"
              onError={() => setCoverFailed(true)}
            />
          ) : (
            <div className="flex size-full flex-col items-center justify-center gap-2 text-sm text-muted-foreground">
              <ImageIcon aria-hidden="true" />
              暂无封面
            </div>
          )}
        </div>
        <div className="min-w-0 space-y-4">
          <dl className="grid gap-x-6 gap-y-3 text-sm sm:grid-cols-2">
            <ReviewFact label="内容名称" value={detail.content_name} />
            <ReviewFact
              label="上传者"
              value={
                <span className="inline-flex items-center gap-1">
                  <UserRoundIcon data-icon="inline-start" />
                  {detail.uploader_display_name}
                  {detail.anonymous ? "（发布后匿名）" : ""}
                </span>
              }
            />
            <ReviewFact
              label="总大小"
              value={formatBytes(detail.total_size_bytes)}
            />
            <ReviewFact
              label="有效载荷"
              value={formatBytes(detail.payload_size_bytes)}
            />
            <ReviewFact
              label="文件"
              value={
                detail.file_count.toLocaleString("zh-CN") +
                " 个（填充 " +
                detail.padding_file_count.toLocaleString("zh-CN") +
                " 个）"
              }
            />
            <ReviewFact
              label="分片"
              value={
                detail.piece_count.toLocaleString("zh-CN") +
                " × " +
                formatBytes(detail.piece_length_bytes)
              }
            />
            <ReviewFact
              label="送审时间"
              value={formatDateTime(detail.review_requested_at)}
            />
            <ReviewFact
              label="截图"
              value={detail.screenshot_count.toLocaleString("zh-CN") + " 张"}
            />
          </dl>
          {detail.facets.length > 0 ? (
            <div className="flex flex-wrap gap-2">
              {detail.facets.map((facet) => (
                <Badge
                  key={facet.facet_id + ":" + facet.option_key}
                  variant="outline"
                >
                  {facet.facet_name}：{facet.option_label}
                </Badge>
              ))}
            </div>
          ) : null}
          {detail.external_identifiers.length > 0 ? (
            <div className="flex flex-wrap gap-2 text-xs text-muted-foreground">
              {detail.external_identifiers.map((identifier) => (
                <code
                  key={identifier.provider}
                  className="rounded bg-muted px-2 py-1"
                >
                  {identifier.provider.toUpperCase()}：{identifier.external_id}
                </code>
              ))}
            </div>
          ) : null}
        </div>
      </CardContent>
    </Card>
  )
}

function ReviewFact({
  label,
  value,
}: {
  label: string
  value: React.ReactNode
}) {
  return (
    <div className="min-w-0">
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className="mt-0.5 break-words">{value}</dd>
    </div>
  )
}

function ReviewFilesCard({
  torrentId,
  infoHash,
}: {
  torrentId: number
  infoHash: string
}) {
  const [offset, setOffset] = React.useState(0)
  const files = useQuery(
    myTorrentReviewFilesQueryOptions(torrentId, filePageSize, offset)
  )
  return (
    <Card className="gap-0 rounded-lg py-0 shadow-sm">
      <CardHeader className="border-b p-5">
        <CardTitle className="flex flex-wrap items-center justify-between gap-3 text-base">
          <span className="inline-flex items-center gap-2">
            <FileArchiveIcon data-icon="inline-start" />
            文件列表
            {files.data
              ? "（" + files.data.total.toLocaleString("zh-CN") + " 个）"
              : ""}
          </span>
          <code className="max-w-64 truncate text-xs font-normal text-muted-foreground">
            {infoHash}
          </code>
        </CardTitle>
      </CardHeader>
      <CardContent className="p-5">
        {files.isPending ? (
          <div className="space-y-2" aria-label="正在加载文件清单">
            {Array.from({ length: 5 }, (_, index) => (
              <Skeleton key={index} className="h-9 w-full" />
            ))}
          </div>
        ) : null}
        {files.isError ? (
          <Alert variant="destructive">
            <CircleAlertIcon />
            <AlertTitle>文件清单暂时无法读取</AlertTitle>
            <AlertDescription>
              {requestErrorDescription(files.error)}
            </AlertDescription>
          </Alert>
        ) : null}
        {files.data ? (
          <>
            <div className="divide-y">
              {files.data.items.map((file) => (
                <div
                  key={file.file_index}
                  className="flex items-center justify-between gap-4 py-2 text-sm"
                >
                  <span className="min-w-0 truncate" title={file.display_path}>
                    {file.display_path}
                  </span>
                  <span className="shrink-0 text-muted-foreground tabular-nums">
                    {formatBytes(file.size_bytes)}
                  </span>
                </div>
              ))}
            </div>
            <OffsetPagination
              total={files.data.total}
              limit={files.data.limit}
              offset={files.data.offset}
              onOffsetChange={setOffset}
              ariaLabel="审核文件清单分页"
              summaryLabel="文件"
              buttonVariant="ghost"
              className="mt-3 border-t pt-3"
            />
          </>
        ) : null}
      </CardContent>
    </Card>
  )
}

function ReviewVotePanel({
  torrent,
  csrfToken,
  onVoted,
}: {
  torrent: MyTorrentReviewDetail
  csrfToken: string
  onVoted: (result: TorrentReviewVoteResult) => void
}) {
  const reasonCodeFieldId = React.useId()
  const reasonFieldId = React.useId()
  const [decision, setDecision] = React.useState<ReviewDecision>("approve")
  const [rejectionReasonCode, setRejectionReasonCode] = React.useState<
    Exclude<ReviewReasonCode, "meets_requirements">
  >("metadata_incomplete")
  const [reason, setReason] = React.useState("")
  const requestId = React.useRef<string>(undefined)
  const vote = useCreateTorrentReviewVote()
  const reasonCount = Array.from(reason.trim()).length
  const reasonInvalid =
    reasonCount > 0 &&
    (reasonCount < minimumReasonCharacters ||
      reasonCount > maximumReasonCharacters)

  function resetAttempt() {
    requestId.current = undefined
    vote.reset()
  }
  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (
      reasonCount < minimumReasonCharacters ||
      reasonCount > maximumReasonCharacters
    )
      return
    requestId.current ??= globalThis.crypto.randomUUID()
    try {
      const result = await vote.mutateAsync({
        torrentId: torrent.id,
        csrfToken,
        idempotencyKey: requestId.current,
        body: {
          expected_version: torrent.version,
          decision,
          reason_code:
            decision === "approve" ? "meets_requirements" : rejectionReasonCode,
          reason: reason.trim(),
        },
      })
      onVoted(result)
    } catch {
      // Preserve the idempotency key so an uncertain response is safe to retry.
    }
  }

  return (
    <Card className="gap-0 rounded-lg py-0 shadow-sm">
      <CardHeader className="border-b p-5">
        <CardTitle className="flex items-center gap-2 text-lg">
          <ShieldCheckIcon data-icon="inline-start" />
          审核建议
        </CardTitle>
        <CardDescription>请核对左侧完整资料后独立判断。</CardDescription>
      </CardHeader>
      <CardContent className="p-5">
        <Progress
          value={(torrent.votes_cast / torrent.maximum_votes) * 100}
          className="mb-5"
        >
          <ProgressLabel>本轮进度</ProgressLabel>
          <ProgressValue>
            {() => torrent.votes_cast + "/" + torrent.maximum_votes + " 票"}
          </ProgressValue>
        </Progress>
        <Alert className="mb-5">
          <ShieldCheckIcon />
          <AlertTitle>投票前隐藏立场分布</AlertTitle>
          <AlertDescription>
            提交后可在“已审核”中查看同意、拒绝票数与本轮结果。
          </AlertDescription>
        </Alert>
        <form onSubmit={submit} noValidate>
          <FieldGroup className="gap-4">
            <Field>
              <FieldLabel>审核结论</FieldLabel>
              <ToggleGroup
                value={[decision]}
                onValueChange={(values) => {
                  const selected = values[0] as ReviewDecision | undefined
                  if (!selected) return
                  setDecision(selected)
                  resetAttempt()
                }}
                variant="outline"
                spacing={1}
                className="grid w-full grid-cols-2"
                disabled={vote.isPending}
              >
                <ToggleGroupItem
                  value="approve"
                  className="h-11 w-full data-pressed:bg-primary data-pressed:text-primary-foreground"
                >
                  <ThumbsUpIcon data-icon="inline-start" />
                  同意
                </ToggleGroupItem>
                <ToggleGroupItem
                  value="reject"
                  className="data-pressed:text-destructive-foreground h-11 w-full data-pressed:bg-destructive"
                >
                  <ThumbsDownIcon data-icon="inline-start" />
                  拒绝
                </ToggleGroupItem>
              </ToggleGroup>
            </Field>
            {decision === "reject" ? (
              <Field>
                <FieldLabel htmlFor={reasonCodeFieldId}>问题类别</FieldLabel>
                <Select
                  items={rejectionReasonOptions}
                  value={rejectionReasonCode}
                  onValueChange={(value) => {
                    if (!value) return
                    setRejectionReasonCode(value)
                    resetAttempt()
                  }}
                  disabled={vote.isPending}
                >
                  <SelectTrigger id={reasonCodeFieldId} className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent alignItemWithTrigger={false}>
                    <SelectGroup>
                      <SelectLabel>问题类别</SelectLabel>
                      {rejectionReasonOptions.map((option) => (
                        <SelectItem key={option.value} value={option.value}>
                          {option.label}
                        </SelectItem>
                      ))}
                    </SelectGroup>
                  </SelectContent>
                </Select>
              </Field>
            ) : null}
            <Field data-invalid={reasonInvalid || undefined}>
              <FieldLabel htmlFor={reasonFieldId}>审核意见</FieldLabel>
              <Textarea
                id={reasonFieldId}
                rows={6}
                minLength={minimumReasonCharacters}
                maxLength={maximumReasonCharacters + 1}
                value={reason}
                aria-invalid={reasonInvalid || undefined}
                placeholder={
                  decision === "approve"
                    ? "写明已经核对的发布要求，至少 10 个字符。"
                    : "写明具体问题与修改建议，至少 10 个字符。"
                }
                disabled={vote.isPending}
                onChange={(event) => {
                  setReason(event.target.value)
                  resetAttempt()
                }}
              />
              <FieldDescription>
                {reasonCount.toLocaleString("zh-CN")}/
                {maximumReasonCharacters.toLocaleString("zh-CN")}
              </FieldDescription>
              {reasonInvalid ? (
                <FieldError>审核意见需要 10–1000 个字符。</FieldError>
              ) : null}
            </Field>
            {vote.isError ? (
              <Alert variant="destructive">
                <CircleAlertIcon />
                <AlertTitle>种审票未能保存</AlertTitle>
                <AlertDescription>
                  {reviewVoteErrorMessage(vote.error)}
                </AlertDescription>
              </Alert>
            ) : null}
            <Button
              type="submit"
              variant={decision === "reject" ? "destructive" : "default"}
              className="w-full"
              disabled={
                vote.isPending ||
                reasonCount < minimumReasonCharacters ||
                reasonCount > maximumReasonCharacters
              }
            >
              {vote.isPending ? <Spinner data-icon="inline-start" /> : null}
              {decision === "approve" ? "提交同意票" : "提交拒绝票"}
            </Button>
            <p className="text-center text-xs text-muted-foreground">
              审核票不可修改；系统保存当时的种审组资格与授权证据。
            </p>
          </FieldGroup>
        </form>
      </CardContent>
    </Card>
  )
}

function ReviewVoteSuccess({ result }: { result: TorrentReviewVoteResult }) {
  return (
    <Card className="gap-0 rounded-lg py-0 shadow-sm">
      <CardContent className="flex min-h-80 flex-col items-center justify-center gap-5 p-8 text-center">
        <div className="rounded-full bg-primary/10 p-4 text-primary">
          <CircleCheckIcon className="size-10" />
        </div>
        <div>
          <h1 className="text-xl font-semibold">审核票已保存</h1>
          <p className="mt-2 max-w-lg text-sm text-muted-foreground">
            {reviewVoteSuccessMessage(result)}
          </p>
        </div>
        <div className="flex flex-wrap justify-center gap-2">
          <Button nativeButton={false} render={<Link to="/review/queue" />}>
            返回审核队列
          </Button>
          {result.outcome === "published" ? (
            <Button
              nativeButton={false}
              variant="outline"
              render={<Link to={"/torrents/" + result.torrent_id} />}
            >
              查看已发布种子
            </Button>
          ) : null}
        </div>
      </CardContent>
    </Card>
  )
}

function ReviewDetailProblem({
  title,
  description,
  action,
}: {
  title: string
  description: string
  action: React.ReactNode
}) {
  return (
    <Empty className="min-h-80 border">
      <EmptyHeader>
        <EmptyMedia variant="icon">
          <CircleAlertIcon />
        </EmptyMedia>
        <EmptyTitle>{title}</EmptyTitle>
        <EmptyDescription>{description}</EmptyDescription>
      </EmptyHeader>
      {action}
    </Empty>
  )
}

function ReviewDetailSkeleton() {
  return (
    <PageLayout
      className="max-w-[1320px] gap-5 px-6 py-8"
      aria-label="正在加载审核资料"
    >
      <Skeleton className="h-9 w-32" />
      <div className="grid gap-5 lg:grid-cols-[minmax(0,1fr)_340px]">
        <div className="space-y-5">
          <Skeleton className="h-96 w-full rounded-lg" />
          <Skeleton className="h-64 w-full rounded-lg" />
          <Skeleton className="h-72 w-full rounded-lg" />
        </div>
        <Skeleton className="h-[560px] w-full rounded-lg" />
      </div>
    </PageLayout>
  )
}

function reviewVoteSuccessMessage(result: TorrentReviewVoteResult) {
  switch (result.outcome) {
    case "published":
      return "本票达到通过条件，种子已经发布。"
    case "rejected":
      return "本票达到驳回条件，上传者会收到审核反馈。"
    case "escalated":
      return "本轮四票形成 2:2，已经转管理员最终处理。"
    default:
      return (
        "本轮当前已投 " +
        result.votes_cast +
        "/" +
        result.maximum_votes +
        " 票，请等待其他审核员独立判断。"
      )
  }
}

function reviewVoteErrorMessage(error: Error) {
  if (error instanceof ApiProblemError) {
    switch (error.code) {
      case "torrent_review_already_voted":
        return "您已经参与本轮审核，请返回审核队列。"
      case "torrent_review_round_escalated":
        return "本轮已经形成 2:2，现已转管理员处理。"
      case "torrent_self_review_denied":
        return "不能审核自己上传的种子。"
      case "torrent_review_version_conflict":
      case "torrent_review_state_conflict":
        return "种子状态已经变化，请返回队列重新核对。"
      case "torrent_review_membership_required":
        return "种审组资格不存在或已经失效。"
      case "csrf_invalid":
        return "登录会话已经变化，请刷新页面后重试。"
    }
  }
  return requestErrorDescription(error, "种审票暂时无法保存，请稍后重试。")
}

function reviewCoverUrl(torrentId: number) {
  return resolveApiUrl(
    "/api/v1/me/torrent-reviews/" + encodeURIComponent(torrentId) + "/cover"
  )
}

function reviewScreenshotUrl(torrentId: number, position: number) {
  return resolveApiUrl(
    "/api/v1/me/torrent-reviews/" +
      encodeURIComponent(torrentId) +
      "/screenshots/" +
      position
  )
}

import * as React from "react"
import { Link } from "react-router"
import {
  AlignLeftIcon,
  CircleAlertIcon,
  CircleCheckBigIcon,
  ChevronRightIcon,
  ClockIcon,
  FileUpIcon,
  ImagesIcon,
  LogInIcon,
  PencilLineIcon,
  RefreshCwIcon,
  ShieldXIcon,
  Trash2Icon,
} from "lucide-react"

import {
  Alert,
  AlertAction,
  AlertDescription,
  AlertTitle,
} from "~/components/ui/alert"
import { Badge } from "~/components/ui/badge"
import { Button, buttonVariants } from "~/components/ui/button"
import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from "~/components/ui/card"
import {
  Empty,
  EmptyContent,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "~/components/ui/empty"
import { Skeleton } from "~/components/ui/skeleton"
import { ToggleGroup, ToggleGroupItem } from "~/components/ui/toggle-group"
import { useWebSession } from "~/features/auth/api/session.mutations"
import { useCapabilities } from "~/features/authz/api/capabilities.queries"
import {
  type MyTorrentSubmissionPage,
  useCategoryList,
  useMyTorrentSubmissions,
} from "~/features/torrent/api/torrent.queries"
import { TorrentPublishedContentDialog } from "~/features/torrent/components/torrent-published-content-dialog"
import { TorrentPublishedScreenshotDialog } from "~/features/torrent/components/torrent-published-screenshot-dialog"
import { TorrentWithdrawalDialog } from "~/features/torrent/components/torrent-withdrawal-dialog"
import {
  TorrentPublishedMetadataDialog,
  TorrentResubmissionDialog,
} from "~/features/torrent/components/torrent-resubmission-dialog"
import { ReviewCenterNavigation } from "~/features/torrent/components/review-center-navigation"
import { useMyWorkgroups } from "~/features/workgroups/api/workgroups.queries"
import { PageLayout } from "~/shared/components/page-layout"
import { formatBytes } from "~/shared/formatters/bytes"
import { formatDateTime } from "~/shared/formatters/date-time"
import { cn } from "~/lib/utils"
import { requestErrorDescription } from "~/shared/api/problem"

type MySubmission = MyTorrentSubmissionPage["items"][number]
type SubmissionFilter = "all" | "pending" | "revision" | "rejected"

const submissionState = {
  pending_review: {
    label: "待审核",
    className: "border-warning/40 bg-warning/10 text-warning-foreground",
  },
  published: {
    label: "已发布",
    className: "border-success/40 bg-success/10 text-success-foreground",
  },
  rejected: {
    label: "已驳回",
    className: "border-destructive/40 bg-destructive/10 text-destructive",
  },
  disabled: {
    label: "已停用",
    className: "border-muted-foreground/30 bg-muted text-muted-foreground",
  },
  deleted: {
    label: "已删除",
    className: "border-muted-foreground/30 bg-muted text-muted-foreground",
  },
} as const

const reviewReasonLabel: Record<string, string> = {
  meets_requirements: "符合发布要求",
  metadata_incomplete: "元数据不完整",
  duplicate_or_superseded: "重复或已有替代版本",
  content_policy_violation: "不符合内容政策",
  quality_requirements_not_met: "未达到质量要求",
  uploader_action_required: "需要发布者处理",
  other: "其他原因",
}

export function MyTorrentSubmissionsPage() {
  const [filter, setFilter] = React.useState<SubmissionFilter>("all")
  const [selectedAction, setSelectedAction] = React.useState<{
    mode: "resubmit" | "published" | "content" | "screenshots" | "withdrawal"
    submission: MySubmission
  }>()
  const [successMessage, setSuccessMessage] = React.useState<{
    title: string
    description: string
  }>()
  const session = useWebSession()
  const capabilities = useCapabilities(session.data?.user.id)
  const workgroups = useMyWorkgroups(session.data?.user.id)
  const canOpenReviewQueue = Boolean(
    workgroups.data?.items.some(
      (item) =>
        item.definition.kind === "review" &&
        item.membership?.status === "active"
    )
  )
  const canRead = capabilities.data?.items.some(
    (capability) => capability.action === "torrent.submission.read.self"
  )
  const canSubmit = capabilities.data?.items.some(
    (capability) => capability.action === "torrent.submit"
  )
  const canResubmit = capabilities.data?.items.some(
    (capability) => capability.action === "torrent.submission.resubmit.self"
  )
  const canMaintainPublished = capabilities.data?.items.some(
    (capability) => capability.action === "torrent.metadata.update.self"
  )
  const canMaintainPublishedContent = capabilities.data?.items.some(
    (capability) => capability.action === "torrent.content.change.submit.self"
  )
  const canMaintainPublishedScreenshots = capabilities.data?.items.some(
    (capability) =>
      capability.action === "torrent.screenshot.change.submit.self"
  )
  const canRequestWithdrawal = capabilities.data?.items.some(
    (capability) => capability.action === "torrent.withdraw.request.self"
  )
  const submissions = useMyTorrentSubmissions(
    session.data?.user.id,
    Boolean(session.data && canRead)
  )
  const hasResubmissionCandidate = submissions.data?.items.some(
    (submission) => submission.resubmission_allowed
  )
  const hasPublishedCandidate = submissions.data?.items.some(
    (submission) => submission.state === "published"
  )
  const categories = useCategoryList(
    Boolean(
      session.data &&
      canRead &&
      ((canResubmit && hasResubmissionCandidate) ||
        (canMaintainPublished && hasPublishedCandidate))
    )
  )
  const resubmissionReady = Boolean(categories.data?.length)
  const filteredPage = React.useMemo(() => {
    if (!submissions.data) return undefined
    const items = submissions.data.items.filter((submission) => {
      switch (filter) {
        case "pending":
          return submission.state === "pending_review"
        case "revision":
          return (
            submission.state === "rejected" && submission.resubmission_allowed
          )
        case "rejected":
          return (
            submission.state === "rejected" && !submission.resubmission_allowed
          )
        default:
          return true
      }
    })
    return { ...submissions.data, items }
  }, [filter, submissions.data])
  const filterCounts = React.useMemo(() => {
    const items = submissions.data?.items ?? []
    return {
      all: items.length,
      pending: items.filter((item) => item.state === "pending_review").length,
      revision: items.filter(
        (item) => item.state === "rejected" && item.resubmission_allowed
      ).length,
      rejected: items.filter(
        (item) => item.state === "rejected" && !item.resubmission_allowed
      ).length,
    }
  }, [submissions.data?.items])

  return (
    <PageLayout className="gap-6 p-10 lg:p-12">
      <ReviewCenterNavigation canReview={canOpenReviewQueue} />
      <header>
        <h1 className="font-heading text-2xl font-bold">我的上传审核状态</h1>
        <p className="mt-1 text-sm text-muted-foreground">
          查看您上传的种子审核进度
        </p>
      </header>

      {successMessage ? (
        <Alert className="border-success/40 bg-success/10">
          <CircleCheckBigIcon className="text-success" />
          <AlertTitle>{successMessage.title}</AlertTitle>
          <AlertDescription>{successMessage.description}</AlertDescription>
        </Alert>
      ) : null}

      {session.isPending && <MySubmissionsSkeleton />}

      {session.isError && (
        <Alert variant="destructive">
          <CircleAlertIcon />
          <AlertTitle>会话状态暂时无法读取</AlertTitle>
          <AlertDescription>
            {requestErrorDescription(
              session.error,
              "会话请求未能完成，请稍后刷新页面。"
            )}
          </AlertDescription>
        </Alert>
      )}

      {!session.isPending && !session.isError && !session.data && (
        <AccessCard
          icon={LogInIcon}
          title="需要登录"
          description="登录后只能读取自己的提交记录与审核反馈。"
          action={
            <Link to="/login" className={buttonVariants()}>
              <LogInIcon data-icon="inline-start" />
              前往登录
            </Link>
          }
        />
      )}

      {session.data && capabilities.isPending && <MySubmissionsSkeleton />}

      {session.data && capabilities.isError && (
        <Alert variant="destructive">
          <CircleAlertIcon />
          <AlertTitle>暂时无法确认查看权限</AlertTitle>
          <AlertDescription>
            {requestErrorDescription(
              capabilities.error,
              "权限请求未能完成，请稍后再试。"
            )}
          </AlertDescription>
          <AlertAction>
            <Button
              variant="outline"
              size="sm"
              onClick={() => void capabilities.refetch()}
            >
              <RefreshCwIcon data-icon="inline-start" />
              重试
            </Button>
          </AlertAction>
        </Alert>
      )}

      {session.data && capabilities.data && !canRead && (
        <AccessCard
          icon={ShieldXIcon}
          title="当前账户不能查看发布记录"
          description="当前账户没有查看发布记录的权限。"
          action={
            <Link to="/" className={buttonVariants({ variant: "outline" })}>
              返回首页
            </Link>
          }
        />
      )}

      {session.data && canRead && submissions.isPending && (
        <MySubmissionsSkeleton />
      )}

      {session.data && canRead && submissions.isError && (
        <Alert variant="destructive">
          <CircleAlertIcon />
          <AlertTitle>发布记录暂时无法读取</AlertTitle>
          <AlertDescription>
            {requestErrorDescription(
              submissions.error,
              "发布记录请求未能完成，请稍后再试。"
            )}
          </AlertDescription>
          <AlertAction>
            <Button
              variant="outline"
              size="sm"
              onClick={() => void submissions.refetch()}
            >
              <RefreshCwIcon data-icon="inline-start" />
              重试
            </Button>
          </AlertAction>
        </Alert>
      )}

      {session.data &&
        canRead &&
        canResubmit &&
        hasResubmissionCandidate &&
        categories.isError && (
          <Alert variant="destructive">
            <CircleAlertIcon />
            <AlertTitle>可用分类暂时无法查看</AlertTitle>
            <AlertDescription>
              {requestErrorDescription(
                categories.error,
                "整改入口会保留，请稍后重试。"
              )}
            </AlertDescription>
            <AlertAction>
              <Button
                variant="outline"
                size="sm"
                onClick={() => void categories.refetch()}
              >
                <RefreshCwIcon data-icon="inline-start" />
                重试
              </Button>
            </AlertAction>
          </Alert>
        )}

      {session.data &&
        canRead &&
        canResubmit &&
        hasResubmissionCandidate &&
        categories.isSuccess &&
        categories.data.length === 0 && (
          <Alert>
            <CircleAlertIcon />
            <AlertTitle>暂时没有可用分类</AlertTitle>
            <AlertDescription>
              站点恢复可用分类后，才能提交整改结果。
            </AlertDescription>
          </Alert>
        )}

      {session.data && canRead && submissions.data && filteredPage ? (
        <div className="flex flex-col gap-4">
          <ToggleGroup
            value={[filter]}
            onValueChange={(values) => {
              const selected = values[0] as SubmissionFilter | undefined
              if (selected) setFilter(selected)
            }}
            spacing={0}
            aria-label="按审核状态筛选"
            className="grid h-10 w-full max-w-md grid-cols-4 rounded-md bg-muted/60 p-1"
          >
            {(
              [
                ["all", "全部"],
                ["pending", "审核中"],
                ["revision", "待修改"],
                ["rejected", "已拒绝"],
              ] as const
            ).map(([value, label]) => (
              <ToggleGroupItem
                key={value}
                value={value}
                className="gap-2 rounded-sm! aria-pressed:bg-background aria-pressed:shadow-sm"
              >
                {label}
                <Badge
                  variant="secondary"
                  className={cn(
                    "size-5 rounded-full p-0 text-xs",
                    (value === "pending" || value === "revision") &&
                      "bg-warning text-warning-foreground",
                    value === "rejected" &&
                      "text-destructive-foreground bg-destructive"
                  )}
                >
                  {filterCounts[value]}
                </Badge>
              </ToggleGroupItem>
            ))}
          </ToggleGroup>

          <MySubmissionsContent
            page={filteredPage}
            filter={filter}
            canSubmit={Boolean(canSubmit)}
            canResubmit={Boolean(canResubmit)}
            canMaintainPublished={Boolean(canMaintainPublished)}
            canMaintainPublishedContent={Boolean(canMaintainPublishedContent)}
            canMaintainPublishedScreenshots={Boolean(
              canMaintainPublishedScreenshots
            )}
            canRequestWithdrawal={Boolean(canRequestWithdrawal)}
            resubmissionReady={resubmissionReady}
            onResubmit={(submission) => {
              setSuccessMessage(undefined)
              setSelectedAction({ mode: "resubmit", submission })
            }}
            onMaintainPublished={(submission) => {
              setSuccessMessage(undefined)
              setSelectedAction({ mode: "published", submission })
            }}
            onMaintainPublishedContent={(submission) => {
              setSuccessMessage(undefined)
              setSelectedAction({ mode: "content", submission })
            }}
            onMaintainPublishedScreenshots={(submission) => {
              setSuccessMessage(undefined)
              setSelectedAction({ mode: "screenshots", submission })
            }}
            onRequestWithdrawal={(submission) => {
              setSuccessMessage(undefined)
              setSelectedAction({ mode: "withdrawal", submission })
            }}
          />
        </div>
      ) : null}

      {session.data && selectedAction?.mode === "content" ? (
        <TorrentPublishedContentDialog
          submission={selectedAction.submission}
          userId={session.data.user.id}
          csrfToken={session.data.csrf_token}
          onOpenChange={(open) => {
            if (!open) setSelectedAction(undefined)
          }}
          onSubmitted={() => {
            const title = selectedAction.submission.title
            setSelectedAction(undefined)
            setSuccessMessage({
              title: "内容修改已送审",
              description: `“${title}”仍显示原公开内容；审核通过后才会切换到新版本。`,
            })
          }}
        />
      ) : null}

      {session.data && selectedAction?.mode === "withdrawal" ? (
        <TorrentWithdrawalDialog
          submission={selectedAction.submission}
          userId={session.data.user.id}
          csrfToken={session.data.csrf_token}
          onOpenChange={(open) => {
            if (!open) setSelectedAction(undefined)
          }}
          onSubmitted={(request) => {
            setSelectedAction(undefined)
            setSuccessMessage({
              title: "撤回申请已提交",
              description: `“${request.torrent_title}”已停止公开和 Tracker 准入，等待管理员处理。`,
            })
          }}
        />
      ) : null}

      {session.data && selectedAction?.mode === "screenshots" ? (
        <TorrentPublishedScreenshotDialog
          submission={selectedAction.submission}
          userId={session.data.user.id}
          csrfToken={session.data.csrf_token}
          onOpenChange={(open) => {
            if (!open) setSelectedAction(undefined)
          }}
          onSubmitted={() => {
            const title = selectedAction.submission.title
            setSelectedAction(undefined)
            setSuccessMessage({
              title: "截图修改已送审",
              description: `“${title}”仍显示原图集；审核通过后才会整体切换。`,
            })
          }}
        />
      ) : null}

      {session.data &&
      selectedAction?.mode === "resubmit" &&
      categories.data?.length ? (
        <TorrentResubmissionDialog
          submission={selectedAction.submission}
          categories={categories.data}
          userId={session.data.user.id}
          csrfToken={session.data.csrf_token}
          onOpenChange={(open) => {
            if (!open) setSelectedAction(undefined)
          }}
          onSubmitted={(result) => {
            setSelectedAction(undefined)
            setSuccessMessage({
              title: "已重新送审",
              description: `“${result.title}”已进入新一轮审核，原种子文件保持不变。`,
            })
          }}
        />
      ) : null}

      {session.data &&
      selectedAction?.mode === "published" &&
      categories.data?.length ? (
        <TorrentPublishedMetadataDialog
          submission={selectedAction.submission}
          categories={categories.data}
          userId={session.data.user.id}
          csrfToken={session.data.csrf_token}
          onOpenChange={(open) => {
            if (!open) setSelectedAction(undefined)
          }}
          onUpdated={(result) => {
            setSelectedAction(undefined)
            setSuccessMessage({
              title: "发布资料已更新",
              description: `“${result.title}”的基础资料已保存，原种子文件和信息哈希保持不变。`,
            })
          }}
        />
      ) : null}
    </PageLayout>
  )
}

function MySubmissionsContent({
  page,
  filter,
  canSubmit,
  canResubmit,
  canMaintainPublished,
  canMaintainPublishedContent,
  canMaintainPublishedScreenshots,
  canRequestWithdrawal,
  resubmissionReady,
  onResubmit,
  onMaintainPublished,
  onMaintainPublishedContent,
  onMaintainPublishedScreenshots,
  onRequestWithdrawal,
}: {
  page: MyTorrentSubmissionPage
  filter: SubmissionFilter
  canSubmit: boolean
  canResubmit: boolean
  canMaintainPublished: boolean
  canMaintainPublishedContent: boolean
  canMaintainPublishedScreenshots: boolean
  canRequestWithdrawal: boolean
  resubmissionReady: boolean
  onResubmit: (submission: MySubmission) => void
  onMaintainPublished: (submission: MySubmission) => void
  onMaintainPublishedContent: (submission: MySubmission) => void
  onMaintainPublishedScreenshots: (submission: MySubmission) => void
  onRequestWithdrawal: (submission: MySubmission) => void
}) {
  if (page.items.length === 0) {
    const emptyLabel = {
      all: "暂无待审核的种子",
      pending: "暂无审核中的种子",
      revision: "暂无待修改的种子",
      rejected: "暂无已拒绝的种子",
    }[filter]
    return (
      <Card className="gap-0 rounded-lg border py-0 shadow-sm ring-0">
        <CardContent className="px-0">
          <Empty className="min-h-60 rounded-none border-0 py-12">
            <EmptyHeader>
              <EmptyMedia className="text-muted-foreground/60 [&_svg]:size-12">
                <FileUpIcon />
              </EmptyMedia>
              <EmptyTitle className="text-base text-muted-foreground">
                {emptyLabel}
              </EmptyTitle>
            </EmptyHeader>
            {canSubmit && filter === "all" ? (
              <EmptyContent>
                <Link to="/upload" className={buttonVariants()}>
                  上传种子
                  <ChevronRightIcon data-icon="inline-end" />
                </Link>
              </EmptyContent>
            ) : null}
          </Empty>
        </CardContent>
      </Card>
    )
  }

  return (
    <section aria-label="我的种子提交记录" className="flex flex-col gap-4">
      {page.items.map((submission) => (
        <MySubmissionCard
          key={submission.id}
          submission={submission}
          canResubmit={canResubmit}
          canMaintainPublished={canMaintainPublished}
          canMaintainPublishedContent={canMaintainPublishedContent}
          canMaintainPublishedScreenshots={canMaintainPublishedScreenshots}
          canRequestWithdrawal={canRequestWithdrawal}
          resubmissionReady={resubmissionReady}
          onResubmit={onResubmit}
          onMaintainPublished={onMaintainPublished}
          onMaintainPublishedContent={onMaintainPublishedContent}
          onMaintainPublishedScreenshots={onMaintainPublishedScreenshots}
          onRequestWithdrawal={onRequestWithdrawal}
        />
      ))}
    </section>
  )
}

function MySubmissionCard({
  submission,
  canResubmit,
  canMaintainPublished,
  canMaintainPublishedContent,
  canMaintainPublishedScreenshots,
  canRequestWithdrawal,
  resubmissionReady,
  onResubmit,
  onMaintainPublished,
  onMaintainPublishedContent,
  onMaintainPublishedScreenshots,
  onRequestWithdrawal,
}: {
  submission: MySubmission
  canResubmit: boolean
  canMaintainPublished: boolean
  canMaintainPublishedContent: boolean
  canMaintainPublishedScreenshots: boolean
  canRequestWithdrawal: boolean
  resubmissionReady: boolean
  onResubmit: (submission: MySubmission) => void
  onMaintainPublished: (submission: MySubmission) => void
  onMaintainPublishedContent: (submission: MySubmission) => void
  onMaintainPublishedScreenshots: (submission: MySubmission) => void
  onRequestWithdrawal: (submission: MySubmission) => void
}) {
  return (
    <Card className="gap-0 rounded-lg border py-0 shadow-sm ring-0">
      <CardContent className="p-4">
        <div className="flex items-start justify-between gap-4">
          <div className="min-w-0 flex-1">
            <div className="flex min-w-0 items-center gap-2">
              <SubmissionTitle submission={submission} />
            </div>
            <div className="mt-2 flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-muted-foreground">
              <span className="flex items-center gap-1">
                <ClockIcon className="size-3" />
                提交于
                <time dateTime={submission.submitted_at}>
                  {formatDateTime(submission.submitted_at)}
                </time>
              </span>
              <span>
                {submission.category.name} ·{" "}
                {formatBytes(submission.total_size_bytes)} ·{" "}
                {submission.file_count.toLocaleString("zh-CN")} 个文件
              </span>
            </div>
            <div className="mt-3 max-w-2xl">
              <ReviewSummary submission={submission} />
              <ContentChangeSummary submission={submission} />
              <ScreenshotChangeSummary submission={submission} />
              <WithdrawalSummary submission={submission} />
            </div>
          </div>
          <div className="flex shrink-0 flex-col items-end gap-2">
            <SubmissionStateBadge state={submission.state} />
            {canResubmit ? (
              <ResubmissionButton
                submission={submission}
                enabled={resubmissionReady}
                onSelect={onResubmit}
              />
            ) : null}
            {canMaintainPublished && submission.state === "published" ? (
              <Button
                type="button"
                variant="outline"
                size="sm"
                disabled={!resubmissionReady}
                title={resubmissionReady ? undefined : "正在读取可用分类"}
                onClick={() => onMaintainPublished(submission)}
              >
                <PencilLineIcon data-icon="inline-start" />
                修改资料
              </Button>
            ) : null}
            {canMaintainPublishedContent && submission.state === "published" ? (
              <Button
                type="button"
                variant="outline"
                size="sm"
                disabled={
                  submission.latest_content_change?.status === "pending"
                }
                onClick={() => onMaintainPublishedContent(submission)}
              >
                <AlignLeftIcon data-icon="inline-start" />
                {submission.latest_content_change?.status === "pending"
                  ? "等待内容审核"
                  : "修改内容"}
              </Button>
            ) : null}
            {canRequestWithdrawal && submission.state === "published" ? (
              <Button
                type="button"
                variant="destructive"
                size="sm"
                disabled={
                  submission.latest_content_change?.status === "pending" ||
                  submission.latest_screenshot_change?.status === "pending"
                }
                title={
                  submission.latest_content_change?.status === "pending" ||
                  submission.latest_screenshot_change?.status === "pending"
                    ? "请等待内容或截图修改审核完成"
                    : undefined
                }
                onClick={() => onRequestWithdrawal(submission)}
              >
                <Trash2Icon data-icon="inline-start" />
                申请撤回
              </Button>
            ) : null}
            {canMaintainPublishedScreenshots &&
            submission.state === "published" ? (
              <Button
                type="button"
                variant="outline"
                size="sm"
                disabled={
                  submission.latest_screenshot_change?.status === "pending"
                }
                onClick={() => onMaintainPublishedScreenshots(submission)}
              >
                <ImagesIcon data-icon="inline-start" />
                {submission.latest_screenshot_change?.status === "pending"
                  ? "等待截图审核"
                  : "修改截图"}
              </Button>
            ) : null}
          </div>
        </div>
      </CardContent>
    </Card>
  )
}

function ContentChangeSummary({ submission }: { submission: MySubmission }) {
  const change = submission.latest_content_change
  if (!change) return null
  const label = {
    pending: "内容修改等待审核",
    approved: "最近内容修改已通过",
    rejected: "最近内容修改已驳回",
  }[change.status]
  return (
    <div className="mt-2 rounded-md border bg-muted/30 px-3 py-2 text-xs">
      <p className="font-medium">{label}</p>
      {change.decision_reason ? (
        <p className="mt-1 text-muted-foreground">{change.decision_reason}</p>
      ) : (
        <p className="mt-1 text-muted-foreground">
          提交于 {formatDateTime(change.submitted_at)}，审核前仍显示原公开内容。
        </p>
      )}
    </div>
  )
}

function ScreenshotChangeSummary({ submission }: { submission: MySubmission }) {
  const change = submission.latest_screenshot_change
  if (!change) return null
  const label = {
    pending: "截图修改等待审核",
    approved: "最近截图修改已通过",
    rejected: "最近截图修改已驳回",
  }[change.status]
  return (
    <div className="mt-2 rounded-md border bg-muted/30 px-3 py-2 text-xs">
      <p className="font-medium">{label}</p>
      <p className="mt-1 text-muted-foreground">
        {change.decision_reason ??
          `提交于 ${formatDateTime(change.submitted_at)}，审核前仍显示原公开图集。`}
      </p>
    </div>
  )
}

function WithdrawalSummary({ submission }: { submission: MySubmission }) {
  const withdrawal = submission.latest_withdrawal
  if (!withdrawal) return null
  const label = {
    pending: "撤回申请等待审核",
    approved: "撤回申请已批准",
    rejected: "最近撤回申请已驳回",
  }[withdrawal.status]
  return (
    <div className="mt-2 rounded-md border bg-muted/30 px-3 py-2 text-xs">
      <p className="font-medium">{label}</p>
      {withdrawal.decision_reason ? (
        <p className="mt-1 text-muted-foreground">
          {withdrawal.decision_reason}
        </p>
      ) : (
        <p className="mt-1 text-muted-foreground">
          提交于 {formatDateTime(withdrawal.submitted_at)}，当前已经停止公开和
          Tracker 准入。
        </p>
      )}
    </div>
  )
}

function SubmissionTitle({ submission }: { submission: MySubmission }) {
  if (submission.state !== "published") {
    return <span className="truncate font-medium">{submission.title}</span>
  }
  return (
    <Link
      to={`/torrents/${submission.id}`}
      className="truncate rounded-sm font-medium underline-offset-4 hover:text-primary hover:underline focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
    >
      {submission.title}
    </Link>
  )
}

function SubmissionStateBadge({ state }: { state: MySubmission["state"] }) {
  const display = submissionState[state]
  return (
    <Badge variant="outline" className={display.className}>
      {display.label}
    </Badge>
  )
}

function ReviewSummary({ submission }: { submission: MySubmission }) {
  if (!submission.latest_review) {
    return (
      <p className="text-xs text-muted-foreground">
        {submission.state === "pending_review"
          ? "等待审核决定"
          : "暂无可公开的审核反馈"}
      </p>
    )
  }
  const feedback = submission.latest_review
  const isResubmitted =
    submission.state === "pending_review" && feedback.outcome === "rejected"
  return (
    <div className="flex flex-col gap-1 text-xs">
      <span className="font-medium">
        {isResubmitted
          ? "已整改，等待复核"
          : (reviewReasonLabel[feedback.reason_code] ?? feedback.reason_code)}
      </span>
      <span
        className="line-clamp-2 text-muted-foreground"
        title={feedback.reason}
      >
        {isResubmitted ? `上轮反馈：${feedback.reason}` : feedback.reason}
      </span>
      <time className="text-muted-foreground" dateTime={feedback.decided_at}>
        {formatDateTime(feedback.decided_at)}
      </time>
    </div>
  )
}

function ResubmissionButton({
  submission,
  enabled,
  onSelect,
  className,
}: {
  submission: MySubmission
  enabled: boolean
  onSelect: (submission: MySubmission) => void
  className?: string
}) {
  if (!submission.resubmission_allowed) return null
  return (
    <Button
      type="button"
      variant="outline"
      size="sm"
      className={className}
      disabled={!enabled}
      title={enabled ? undefined : "正在读取可用分类"}
      onClick={() => onSelect(submission)}
    >
      <PencilLineIcon data-icon="inline-start" />
      整改并重新提交
    </Button>
  )
}

function AccessCard({
  icon: Icon,
  title,
  description,
  action,
}: {
  icon: typeof LogInIcon
  title: string
  description: string
  action: React.ReactNode
}) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>{title}</CardTitle>
        <CardDescription>{description}</CardDescription>
      </CardHeader>
      <CardContent>
        <Alert>
          <Icon />
          <AlertTitle>{title}</AlertTitle>
          <AlertDescription>{description}</AlertDescription>
        </Alert>
      </CardContent>
      <CardFooter>{action}</CardFooter>
    </Card>
  )
}

function MySubmissionsSkeleton() {
  return (
    <Card aria-label="正在加载发布记录" aria-busy="true">
      <CardHeader>
        <CardTitle>
          <Skeleton className="h-5 w-24" />
        </CardTitle>
        <CardDescription>
          <Skeleton className="h-4 w-64 max-w-full" />
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        {Array.from({ length: 5 }, (_, index) => (
          <Skeleton key={index} className="h-14 w-full" />
        ))}
      </CardContent>
    </Card>
  )
}

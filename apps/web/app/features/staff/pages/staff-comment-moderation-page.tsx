import * as React from "react"
import { useQuery } from "@tanstack/react-query"
import { Link } from "react-router"
import {
  CheckCircle2Icon,
  CircleAlertIcon,
  Clock3Icon,
  FlagIcon,
  RefreshCwIcon,
  ShieldCheckIcon,
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
  CardAction,
  CardContent,
  CardHeader,
  CardTitle,
} from "~/components/ui/card"
import { Empty, EmptyHeader, EmptyTitle } from "~/components/ui/empty"
import { Skeleton } from "~/components/ui/skeleton"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "~/components/ui/table"
import {
  type CommentModerationCase,
  type CommentModerationDecisionResult,
  commentModerationCasesQueryOptions,
} from "~/features/staff/api/comment-moderation.queries"
import { StaffAccessGate } from "~/features/staff/components/staff-access-gate"
import { StaffPageFrame } from "~/features/staff/components/staff-page-frame"
import { CommentModerationDecisionDialog } from "~/features/staff/components/comment-moderation-decision-dialog"
import { hasCapability } from "~/features/staff/model/capability"
import { commentReportReasonLabel } from "~/features/social/model/comment-moderation"
import type { components } from "~/generated/api"
import { OffsetPagination } from "~/shared/components/offset-pagination"
import { formatDateTime } from "~/shared/formatters/date-time"

const moderationPageSize = 20
type CapabilityList = components["schemas"]["CapabilityList"]

export function StaffCommentModerationPage() {
  return (
    <StaffAccessGate
      requiredAction="social.report.read"
      pageHeader={{
        title: "评论审核",
        description: "集中核对评论举报，并保留完整处置记录。",
      }}
    >
      {({ session, capabilities }) => (
        <CommentModerationContent
          csrfToken={session.csrf_token}
          capabilities={capabilities}
        />
      )}
    </StaffAccessGate>
  )
}

function CommentModerationContent({
  csrfToken,
  capabilities,
}: {
  csrfToken: string
  capabilities: CapabilityList
}) {
  const [offset, setOffset] = React.useState(0)
  const [decisionTarget, setDecisionTarget] =
    React.useState<CommentModerationCase>()
  const [successMessage, setSuccessMessage] = React.useState("")
  const cases = useQuery(
    commentModerationCasesQueryOptions(moderationPageSize, offset)
  )
  const canResolve = hasCapability(capabilities, "social.report.resolve")

  React.useEffect(() => {
    const total = cases.data?.total
    if (total === undefined || offset === 0 || offset < total) return
    setOffset(lastPageOffset(total, moderationPageSize))
  }, [cases.data?.total, offset])

  if (cases.isPending) {
    return <CommentModerationSkeleton />
  }
  if (cases.isError || !cases.data) {
    return (
      <ModerationFrame>
        <ModerationHeader />
        <Alert variant="destructive">
          <CircleAlertIcon />
          <AlertTitle>评论审核队列暂时无法读取</AlertTitle>
          <AlertDescription>
            暂时无法取得评论举报队列，请稍后重试。
          </AlertDescription>
          <AlertAction>
            <Button
              variant="outline"
              size="sm"
              onClick={() => void cases.refetch()}
            >
              <RefreshCwIcon data-icon="inline-start" />
              重试
            </Button>
          </AlertAction>
        </Alert>
      </ModerationFrame>
    )
  }

  return (
    <ModerationFrame>
      <ModerationHeader>
        <Button
          variant="outline"
          disabled={cases.isFetching}
          onClick={() => void cases.refetch()}
        >
          <RefreshCwIcon data-icon="inline-start" />
          {cases.isFetching ? "刷新中…" : "刷新"}
        </Button>
      </ModerationHeader>

      {successMessage ? (
        <Alert>
          <CheckCircle2Icon />
          <AlertTitle>案件处置已提交</AlertTitle>
          <AlertDescription>{successMessage}</AlertDescription>
        </Alert>
      ) : null}

      <Card className="gap-0 py-0">
        <CardHeader className="min-h-[68px] content-center p-4">
          <CardTitle className="flex items-center gap-2 text-lg font-semibold">
            <span>评论举报案件</span>
            <Badge variant="secondary" className="tabular-nums">
              {cases.data.total.toLocaleString("zh-CN")}
            </Badge>
          </CardTitle>
          <CardAction className="hidden items-center gap-2 self-center lg:flex">
            <Badge variant="outline">举报人匿名</Badge>
            <Badge variant="outline">同评论聚合</Badge>
            <Badge variant="outline">保存前复核</Badge>
            {!canResolve ? <Badge variant="secondary">仅可查看</Badge> : null}
          </CardAction>
        </CardHeader>
        <CardContent className="px-4 pb-4">
          {cases.data.items.length === 0 ? (
            <Empty className="min-h-[121px] p-4">
              <EmptyHeader>
                <EmptyTitle className="font-normal text-muted-foreground">
                  暂无待处理评论举报
                </EmptyTitle>
              </EmptyHeader>
            </Empty>
          ) : (
            <>
              <ModerationCaseTable
                cases={cases.data.items}
                canResolve={canResolve}
                onResolve={(item) => {
                  setSuccessMessage("")
                  setDecisionTarget(item)
                }}
              />
              <div className="grid gap-3 md:hidden">
                {cases.data.items.map((item) => (
                  <ModerationCaseCard
                    key={item.id}
                    moderationCase={item}
                    canResolve={canResolve}
                    onResolve={() => {
                      setSuccessMessage("")
                      setDecisionTarget(item)
                    }}
                  />
                ))}
              </div>
            </>
          )}

          <OffsetPagination
            total={cases.data.total}
            limit={cases.data.limit}
            offset={cases.data.offset}
            onOffsetChange={(nextOffset) => {
              setDecisionTarget(undefined)
              setOffset(nextOffset)
            }}
            ariaLabel="评论审核分页"
            className="mt-4 border-t pt-4"
          />
        </CardContent>
      </Card>

      {decisionTarget ? (
        <CommentModerationDecisionDialog
          key={`${decisionTarget.id}:${decisionTarget.version}:${decisionTarget.comment.version}`}
          moderationCase={decisionTarget}
          csrfToken={csrfToken}
          onOpenChange={(open) => {
            if (!open) setDecisionTarget(undefined)
          }}
          onResolved={(result) => {
            setSuccessMessage(decisionSuccessMessage(result))
            setDecisionTarget(undefined)
          }}
        />
      ) : null}
    </ModerationFrame>
  )
}

function ModerationCaseCard({
  moderationCase,
  canResolve,
  onResolve,
}: {
  moderationCase: CommentModerationCase
  canResolve: boolean
  onResolve: () => void
}) {
  return (
    <article className="rounded-lg border p-4 transition-colors hover:bg-muted/50">
      <div className="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
        <div className="flex min-w-0 flex-1 flex-col gap-3">
          <div className="flex flex-wrap items-center gap-2">
            <FlagIcon className="size-3.5 text-destructive" />
            <ReportReasonBadges moderationCase={moderationCase} />
            <span className="flex items-center gap-1 text-xs text-muted-foreground">
              <Clock3Icon className="size-3" />
              <time dateTime={moderationCase.opened_at}>
                {formatDateTime(moderationCase.opened_at)}
              </time>
            </span>
          </div>
          <CaseComment moderationCase={moderationCase} />
          <ReportDetails moderationCase={moderationCase} />
          <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
            <span>
              共 {moderationCase.report_count.toLocaleString("zh-CN")}{" "}
              份匿名举报
            </span>
            <span aria-hidden="true">·</span>
            <span>
              最近 {formatDateTime(moderationCase.latest_reported_at)}
            </span>
          </div>
        </div>
        {canResolve ? (
          <Button size="sm" onClick={onResolve}>
            <ShieldCheckIcon data-icon="inline-start" />
            处理
          </Button>
        ) : (
          <Badge variant="secondary">只读</Badge>
        )}
      </div>
    </article>
  )
}

function ModerationCaseTable({
  cases,
  canResolve,
  onResolve,
}: {
  cases: CommentModerationCase[]
  canResolve: boolean
  onResolve: (moderationCase: CommentModerationCase) => void
}) {
  return (
    <div className="hidden overflow-x-auto rounded-lg border md:block">
      <Table className="min-w-[980px] table-fixed">
        <TableHeader className="bg-muted/50">
          <TableRow className="h-11">
            <TableHead className="w-[28%] px-3">目标</TableHead>
            <TableHead className="w-[15%] px-3">举报原因</TableHead>
            <TableHead className="w-[29%] px-3">评论内容</TableHead>
            <TableHead className="w-[9%] px-3">状态</TableHead>
            <TableHead className="w-[13%] px-3">举报时间</TableHead>
            <TableHead className="w-[6%] px-3 text-center">操作</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {cases.map((item) => (
            <TableRow key={item.id} className="h-[72px]">
              <TableCell className="px-3 py-2.5">
                <div className="flex min-w-0 flex-col gap-1">
                  <Link
                    to={moderationTargetPath(item)}
                    className="truncate font-medium hover:text-primary hover:underline hover:underline-offset-4"
                  >
                    {item.target.title}
                  </Link>
                  <span className="text-xs text-muted-foreground">
                    {moderationTargetKindLabel(item)} · {item.report_count}{" "}
                    份匿名举报
                  </span>
                </div>
              </TableCell>
              <TableCell className="px-3 py-2.5">
                <ReportReasonBadges moderationCase={item} />
              </TableCell>
              <TableCell className="px-3 py-2.5">
                <p className="line-clamp-2 text-sm leading-relaxed break-words">
                  {moderatedCommentBody(item)}
                </p>
                <p className="mt-0.5 truncate text-xs text-muted-foreground">
                  @{item.comment.author.display_name}
                </p>
              </TableCell>
              <TableCell className="px-3 py-2.5">
                <Badge
                  variant="outline"
                  className="border-warning/40 bg-warning/10 text-warning-foreground"
                >
                  待处理
                </Badge>
              </TableCell>
              <TableCell className="px-3 py-2.5">
                <time
                  dateTime={item.opened_at}
                  className="text-xs text-muted-foreground"
                >
                  {formatDateTime(item.opened_at)}
                </time>
              </TableCell>
              <TableCell className="px-3 py-2.5 text-center">
                {canResolve ? (
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    className="size-8"
                    onClick={() => onResolve(item)}
                    aria-label={"处理评论举报 " + item.target.title}
                  >
                    <ShieldCheckIcon />
                  </Button>
                ) : (
                  <Badge variant="secondary">只读</Badge>
                )}
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  )
}

function moderationTargetPath(moderationCase: CommentModerationCase) {
  return moderationCase.target.kind === "torrent"
    ? "/torrents/" + (moderationCase.target.torrent_id ?? 0) + "#comments"
    : moderationCase.target.kind === "post"
      ? "/social/post/" + (moderationCase.target.post_id ?? "") + "#comments"
      : "/announcements/" +
        encodeURIComponent(moderationCase.target.announcement_id ?? "") +
        "#comments"
}

function moderationTargetKindLabel(moderationCase: CommentModerationCase) {
  return moderationCase.target.kind === "torrent"
    ? "种子"
    : moderationCase.target.kind === "post"
      ? "动态"
      : "公告"
}

function moderatedCommentBody(moderationCase: CommentModerationCase) {
  return moderationCase.comment.state === "visible"
    ? moderationCase.comment.body
    : moderationCase.comment.state === "author_deleted"
      ? "该评论已由作者删除。"
      : "该评论已被管理人员隐藏。"
}

function CaseComment({
  moderationCase,
}: {
  moderationCase: CommentModerationCase
}) {
  const targetPath = moderationTargetPath(moderationCase)
  return (
    <div className="flex min-w-0 flex-col gap-1">
      <div className="flex min-w-0 items-center gap-2">
        <Badge variant="outline">
          {moderationTargetKindLabel(moderationCase)}
        </Badge>
        <Link
          to={targetPath}
          className="line-clamp-1 font-medium hover:text-primary hover:underline hover:underline-offset-4"
        >
          {moderationCase.target.title}
        </Link>
      </div>
      <div className="rounded-md bg-muted/50 p-3">
        <p className="text-xs text-muted-foreground">
          评论者：@{moderationCase.comment.author.display_name}
        </p>
        <p className="mt-1 line-clamp-3 text-sm leading-relaxed break-words whitespace-pre-wrap">
          {moderatedCommentBody(moderationCase)}
        </p>
      </div>
    </div>
  )
}

function ReportReasonBadges({
  moderationCase,
}: {
  moderationCase: CommentModerationCase
}) {
  return (
    <>
      {moderationCase.reports.map((report, index) => (
        <Badge key={`${report.created_at}:${index}`} variant="destructive">
          {commentReportReasonLabel(report.reason_code)}
        </Badge>
      ))}
    </>
  )
}

function ReportDetails({
  moderationCase,
}: {
  moderationCase: CommentModerationCase
}) {
  const details = moderationCase.reports
    .filter((report) => report.details)
    .slice(0, 2)

  return details.length > 0 ? (
    <div className="flex flex-col gap-1">
      {moderationCase.reports
        .filter((report) => report.details)
        .slice(0, 2)
        .map((report, index) => (
          <p
            key={`${report.created_at}:details:${index}`}
            className="line-clamp-2 text-xs text-muted-foreground"
          >
            {report.details}
          </p>
        ))}
    </div>
  ) : null
}

function ModerationHeader({ children }: { children?: React.ReactNode }) {
  return (
    <header className="flex min-h-14 flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
      <div>
        <h1 className="font-heading text-2xl font-bold">评论审核</h1>
        <p className="mt-1 text-sm text-muted-foreground">
          集中核对评论举报，并保留完整处置记录。
        </p>
      </div>
      {children ? (
        <div className="flex items-center gap-2 sm:pt-2">{children}</div>
      ) : null}
    </header>
  )
}

function ModerationFrame({ children }: { children: React.ReactNode }) {
  return <StaffPageFrame>{children}</StaffPageFrame>
}

function CommentModerationSkeleton() {
  return (
    <ModerationFrame>
      <div className="flex flex-col gap-2" aria-busy="true">
        <Skeleton className="h-8 w-40" />
        <Skeleton className="h-4 w-full max-w-2xl" />
      </div>
      <Skeleton className="h-[70px] rounded-lg" />
      <Skeleton className="h-72 rounded-lg" />
    </ModerationFrame>
  )
}

function decisionSuccessMessage(result: CommentModerationDecisionResult) {
  return result.decision === "hide_comment"
    ? `评论已隐藏，举报案件已更新并留下处置记录。`
    : `举报已按无违规关闭，评论内容保持不变。`
}

function lastPageOffset(total: number, limit: number) {
  return Math.max(0, Math.floor((Math.max(total, 1) - 1) / limit) * limit)
}

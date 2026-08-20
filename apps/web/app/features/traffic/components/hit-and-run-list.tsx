import type { MouseEvent } from "react"
import { Link } from "react-router"
import {
  CircleCheckBigIcon,
  Clock3Icon,
  DatabaseIcon,
  MessageSquareWarningIcon,
} from "lucide-react"

import { Badge } from "~/components/ui/badge"
import { Button } from "~/components/ui/button"
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardFooter,
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
  Pagination,
  PaginationContent,
  PaginationItem,
  PaginationLink,
  PaginationNext,
  PaginationPrevious,
} from "~/components/ui/pagination"
import { Progress } from "~/components/ui/progress"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "~/components/ui/table"
import type {
  HitAndRunFilter,
  HitAndRunPageData,
} from "~/features/traffic/api/hnr.queries"
import {
  formatHNRDuration,
  formatHNRRatio,
  hnrProgressPercent,
} from "~/features/traffic/model/hnr-format"
import { formatDateTime } from "~/shared/formatters/date-time"

type HitAndRunEntry = HitAndRunPageData["items"][number]

export function HitAndRunList({
  page,
  filter,
  pageNumber,
  canCreateAppeal,
  onAppeal,
  onPrevious,
  onNext,
}: {
  page: HitAndRunPageData
  filter: HitAndRunFilter
  pageNumber: number
  canCreateAppeal: boolean
  onAppeal: (entry: HitAndRunEntry) => void
  onPrevious: (() => void) | undefined
  onNext: (() => void) | undefined
}) {
  const showAppeals =
    canCreateAppeal || page.items.some((entry) => entry.appeal !== null)
  return (
    <Card className="gap-0 rounded-lg py-0 shadow-sm">
      <CardHeader className="border-b py-3.5">
        <CardTitle>{filterTitle(filter)}</CardTitle>
        <CardDescription>
          按完成时间从新到旧显示，满足做种时长或实际分享率任一条件即可达标。
        </CardDescription>
        <CardAction>
          <Badge variant="outline">第 {pageNumber} 页</Badge>
        </CardAction>
      </CardHeader>
      <CardContent className="px-0">
        {page.items.length === 0 ? (
          <HitAndRunEmpty filter={filter} total={page.summary.total} />
        ) : (
          <>
            <div className="hidden md:block">
              <Table>
                <TableHeader className="bg-secondary/70">
                  <TableRow>
                    <TableHead className="w-full min-w-72 pl-4 text-muted-foreground">
                      种子 / 完成时间
                    </TableHead>
                    <TableHead className="text-muted-foreground">
                      状态
                    </TableHead>
                    <TableHead className="min-w-52 text-muted-foreground">
                      做种进度
                    </TableHead>
                    <TableHead className="min-w-36 text-right text-muted-foreground">
                      实际分享率
                    </TableHead>
                    <TableHead className="min-w-44 pr-4 text-right text-muted-foreground">
                      时间节点
                    </TableHead>
                    {showAppeals ? (
                      <TableHead className="min-w-28 pr-4 text-right text-muted-foreground">
                        申诉
                      </TableHead>
                    ) : null}
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {page.items.map((entry) => (
                    <HitAndRunTableRow
                      key={entry.id}
                      entry={entry}
                      showAppeals={showAppeals}
                      canCreateAppeal={canCreateAppeal}
                      onAppeal={onAppeal}
                    />
                  ))}
                </TableBody>
              </Table>
            </div>
            <div className="grid divide-y md:hidden">
              {page.items.map((entry) => (
                <HitAndRunMobileCard
                  key={entry.id}
                  entry={entry}
                  canCreateAppeal={canCreateAppeal}
                  onAppeal={onAppeal}
                />
              ))}
            </div>
          </>
        )}
      </CardContent>
      {(onPrevious || onNext) && (
        <CardFooter className="border-t px-4 py-3">
          <HitAndRunPagination
            pageNumber={pageNumber}
            onPrevious={onPrevious}
            onNext={onNext}
          />
        </CardFooter>
      )}
    </Card>
  )
}

function HitAndRunTableRow({
  entry,
  showAppeals,
  canCreateAppeal,
  onAppeal,
}: {
  entry: HitAndRunEntry
  showAppeals: boolean
  canCreateAppeal: boolean
  onAppeal: (entry: HitAndRunEntry) => void
}) {
  return (
    <TableRow className="h-14 hover:bg-accent/70">
      <TableCell className="max-w-0 pl-4">
        <div className="flex min-w-0 flex-col gap-1">
          <Link
            to={`/torrents/${entry.torrent.id}`}
            className="truncate font-medium hover:underline"
          >
            {entry.torrent.title}
          </Link>
          <span className="text-xs text-muted-foreground">
            完成于 {formatDateTime(entry.completed_at)}
          </span>
        </div>
      </TableCell>
      <TableCell>
        <HNRStatusBadge status={entry.status} />
      </TableCell>
      <TableCell>
        <SeedProgress entry={entry} />
      </TableCell>
      <TableCell className="text-right tabular-nums">
        <RatioMeasure entry={entry} />
      </TableCell>
      <TableCell className="pr-4 text-right text-xs text-muted-foreground">
        <Deadline entry={entry} />
      </TableCell>
      {showAppeals ? (
        <TableCell className="pr-4 text-right">
          <HNRAppealAction
            entry={entry}
            canCreateAppeal={canCreateAppeal}
            onAppeal={onAppeal}
          />
        </TableCell>
      ) : null}
    </TableRow>
  )
}

function HitAndRunMobileCard({
  entry,
  canCreateAppeal,
  onAppeal,
}: {
  entry: HitAndRunEntry
  canCreateAppeal: boolean
  onAppeal: (entry: HitAndRunEntry) => void
}) {
  return (
    <article className="flex flex-col gap-4 p-4">
      <div className="flex items-start justify-between gap-3">
        <div className="flex min-w-0 flex-col gap-1">
          <Link
            to={`/torrents/${entry.torrent.id}`}
            className="line-clamp-2 font-medium hover:underline"
          >
            {entry.torrent.title}
          </Link>
          <span className="text-xs text-muted-foreground">
            完成于 {formatDateTime(entry.completed_at)}
          </span>
        </div>
        <HNRStatusBadge status={entry.status} />
      </div>
      <SeedProgress entry={entry} />
      <dl className="grid grid-cols-2 gap-3 text-xs">
        <div className="flex flex-col gap-1">
          <dt className="text-muted-foreground">实际分享率</dt>
          <dd className="font-medium tabular-nums">
            <RatioMeasure entry={entry} />
          </dd>
        </div>
        <div className="flex flex-col gap-1 text-right">
          <dt className="text-muted-foreground">时间节点</dt>
          <dd className="font-medium">
            <Deadline entry={entry} />
          </dd>
        </div>
      </dl>
      {entry.appeal || (canCreateAppeal && entry.can_appeal) ? (
        <div className="flex items-center justify-between gap-3 border-t pt-3">
          <span className="text-xs text-muted-foreground">H&amp;R 申诉</span>
          <HNRAppealAction
            entry={entry}
            canCreateAppeal={canCreateAppeal}
            onAppeal={onAppeal}
          />
        </div>
      ) : null}
    </article>
  )
}

function SeedProgress({ entry }: { entry: HitAndRunEntry }) {
  const percent = hnrProgressPercent(
    entry.seeded_seconds,
    entry.required_seed_seconds
  )
  if (entry.required_seed_seconds === "0") {
    return (
      <span className="text-xs text-muted-foreground">无需累计做种时长</span>
    )
  }
  return (
    <div className="flex min-w-44 flex-col gap-2">
      <div className="flex items-center justify-between gap-2 text-xs tabular-nums">
        <span>
          {formatHNRDuration(entry.seeded_seconds)} /{" "}
          {formatHNRDuration(entry.required_seed_seconds)}
        </span>
        <span className="text-muted-foreground">{Math.floor(percent)}%</span>
      </div>
      <Progress
        value={percent}
        aria-label={`${entry.torrent.title} 做种时长进度 ${Math.floor(percent)}%`}
      />
    </div>
  )
}

function RatioMeasure({ entry }: { entry: HitAndRunEntry }) {
  return (
    <span>
      {formatHNRRatio(entry.raw_ratio_basis_points)} /{" "}
      {formatHNRRatio(entry.required_ratio_basis_points)}
    </span>
  )
}

function Deadline({ entry }: { entry: HitAndRunEntry }) {
  if (entry.status === "satisfied" && entry.satisfied_at) {
    return (
      <time dateTime={entry.satisfied_at}>
        达标于 {formatDateTime(entry.satisfied_at)}
      </time>
    )
  }
  if (entry.status === "exempt") {
    return (
      <span>
        {entry.appeal?.status === "approved" ? "申诉批准豁免" : "完成时已豁免"}
      </span>
    )
  }
  if (entry.status === "tracking") {
    return (
      <time dateTime={entry.assessment_due_at}>
        考察至 {formatDateTime(entry.assessment_due_at)}
      </time>
    )
  }
  if (entry.status === "grace") {
    return (
      <time dateTime={entry.grace_ends_at}>
        宽限至 {formatDateTime(entry.grace_ends_at)}
      </time>
    )
  }
  return (
    <time dateTime={entry.grace_ends_at}>
      宽限结束于 {formatDateTime(entry.grace_ends_at)}
    </time>
  )
}

function HNRAppealAction({
  entry,
  canCreateAppeal,
  onAppeal,
}: {
  entry: HitAndRunEntry
  canCreateAppeal: boolean
  onAppeal: (entry: HitAndRunEntry) => void
}) {
  if (entry.appeal) {
    return <HNRAppealBadge status={entry.appeal.status} />
  }
  if (canCreateAppeal && entry.can_appeal) {
    return (
      <Button variant="outline" size="xs" onClick={() => onAppeal(entry)}>
        <MessageSquareWarningIcon data-icon="inline-start" />
        申诉
      </Button>
    )
  }
  return <span className="text-xs text-muted-foreground">—</span>
}

function HNRAppealBadge({
  status,
}: {
  status: NonNullable<HitAndRunEntry["appeal"]>["status"]
}) {
  if (status === "pending") return <Badge variant="secondary">待处理</Badge>
  if (status === "approved") return <Badge variant="outline">已批准</Badge>
  if (status === "rejected") return <Badge variant="destructive">已驳回</Badge>
  return <Badge variant="outline">义务已达标</Badge>
}

function HNRStatusBadge({ status }: { status: HitAndRunEntry["status"] }) {
  const variant =
    status === "overdue"
      ? "destructive"
      : status === "satisfied"
        ? "default"
        : status === "grace" || status === "exempt"
          ? "secondary"
          : "outline"
  return <Badge variant={variant}>{statusLabel(status)}</Badge>
}

function HitAndRunEmpty({
  filter,
  total,
}: {
  filter: HitAndRunFilter
  total: string
}) {
  const hasHistory = total !== "0"
  return (
    <Empty className="min-h-72 rounded-none border-0 py-14">
      <EmptyHeader>
        <EmptyMedia className="mb-2 size-auto bg-transparent text-muted-foreground/50 [&_svg]:size-14">
          {filter === "open" && hasHistory ? (
            <CircleCheckBigIcon />
          ) : hasHistory ? (
            <DatabaseIcon />
          ) : (
            <Clock3Icon />
          )}
        </EmptyMedia>
        <EmptyTitle className="text-lg text-muted-foreground">
          {filter === "open" && hasHistory
            ? "当前没有需要处理的 H&R"
            : hasHistory
              ? "这个筛选下没有记录"
              : "还没有 H&R 记录"}
        </EmptyTitle>
        <EmptyDescription>
          {filter === "open" && hasHistory
            ? "已经达标或豁免的记录可在“全部”中查看。"
            : hasHistory
              ? "切换上方状态筛选查看其他记录。"
              : "完成下载后，符合考察条件的记录会在这里出现。"}
        </EmptyDescription>
      </EmptyHeader>
    </Empty>
  )
}

function HitAndRunPagination({
  pageNumber,
  onPrevious,
  onNext,
}: {
  pageNumber: number
  onPrevious: (() => void) | undefined
  onNext: (() => void) | undefined
}) {
  function activate(event: MouseEvent<HTMLAnchorElement>, action: () => void) {
    event.preventDefault()
    action()
  }
  return (
    <Pagination aria-label="H&R 分页">
      <PaginationContent>
        {onPrevious ? (
          <PaginationItem>
            <PaginationPrevious
              href="#"
              text="上一页"
              onClick={(event) => activate(event, onPrevious)}
            />
          </PaginationItem>
        ) : null}
        <PaginationItem>
          <PaginationLink
            href="#"
            isActive
            onClick={(event) => event.preventDefault()}
          >
            {pageNumber}
          </PaginationLink>
        </PaginationItem>
        {onNext ? (
          <PaginationItem>
            <PaginationNext
              href="#"
              text="下一页"
              onClick={(event) => activate(event, onNext)}
            />
          </PaginationItem>
        ) : null}
      </PaginationContent>
    </Pagination>
  )
}

function statusLabel(status: HitAndRunEntry["status"]) {
  switch (status) {
    case "tracking":
      return "考察中"
    case "grace":
      return "宽限期"
    case "overdue":
      return "待补做"
    case "satisfied":
      return "已达标"
    case "exempt":
      return "已豁免"
  }
}

function filterTitle(filter: HitAndRunFilter) {
  switch (filter) {
    case "open":
      return "待关注记录"
    case "all":
      return "全部记录"
    default:
      return statusLabel(filter)
  }
}

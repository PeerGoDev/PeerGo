import * as React from "react"
import { Link } from "react-router"
import { CircleXIcon, Clock3Icon, FileUpIcon } from "lucide-react"

import { Badge } from "~/components/ui/badge"
import { Card, CardContent, CardHeader, CardTitle } from "~/components/ui/card"
import { Empty, EmptyHeader, EmptyTitle } from "~/components/ui/empty"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "~/components/ui/table"
import { ToggleGroup, ToggleGroupItem } from "~/components/ui/toggle-group"
import type { MyTorrentSubmissionPage } from "~/features/torrent/api/torrent.queries"
import { formatBytes } from "~/shared/formatters/bytes"
import { formatCompactDateTime } from "~/shared/formatters/date-time"
import { cn } from "~/lib/utils"

type Submission = MyTorrentSubmissionPage["items"][number]
type ActivityTab = "published" | "pending_review" | "rejected"

const activityTabs = [
  { value: "published", label: "发布", icon: FileUpIcon },
  { value: "pending_review", label: "审核中", icon: Clock3Icon },
  { value: "rejected", label: "已拒绝", icon: CircleXIcon },
] as const

const stateLabel: Record<ActivityTab, string> = {
  published: "已发布",
  pending_review: "审核中",
  rejected: "已拒绝",
}

/**
 * 复刻 Rousi 用户资料页的种子活动区域，但只展示 Core 当前能够可靠
 * 投影的发布、审核中和拒绝状态。做种/下载进度属于 tracker 当前 peer
 * 投影，不能用静态 0 或提交记录推断，待对应读模型接入后再扩展标签。
 */
export function UserTorrentActivityCard({
  page,
  loading = false,
  className,
}: {
  page: MyTorrentSubmissionPage | undefined
  loading?: boolean
  className?: string
}) {
  const [activeTab, setActiveTab] = React.useState<ActivityTab>("published")
  const items = React.useMemo(
    () =>
      (page?.items ?? []).filter(
        (submission) => submission.state === activeTab
      ),
    [activeTab, page?.items]
  )
  const counts = React.useMemo(
    () => countByState(page?.items ?? []),
    [page?.items]
  )
  const windowIsComplete = Boolean(page && page.total <= page.items.length)

  return (
    <Card
      className={cn(
        "min-h-[218px] gap-0 rounded-lg border py-0 shadow-sm ring-0",
        className
      )}
      aria-busy={loading}
    >
      <CardHeader className="overflow-x-auto px-6 pt-6 pb-0">
        <CardTitle className="sr-only">种子活动</CardTitle>
        <ToggleGroup
          value={[activeTab]}
          onValueChange={(values) => {
            const selected = values[0] as ActivityTab | undefined
            if (selected) setActiveTab(selected)
          }}
          aria-label="按种子活动状态筛选"
          className="w-max"
        >
          {activityTabs.map(({ value, label, icon: Icon }) => (
            <ToggleGroupItem
              key={value}
              value={value}
              className="gap-1.5 rounded-md bg-muted/50 px-3 text-muted-foreground aria-pressed:bg-primary aria-pressed:text-primary-foreground"
            >
              <Icon data-icon="inline-start" />
              {label}
              <span className="text-xs opacity-80">
                {formatTabCount(counts[value], page, windowIsComplete)}
              </span>
            </ToggleGroupItem>
          ))}
        </ToggleGroup>
      </CardHeader>
      <CardContent className="px-6 pt-4 pb-6">
        {loading && !page ? (
          <ActivityEmpty title="正在加载种子活动…" />
        ) : items.length === 0 ? (
          <ActivityEmpty title={`暂无${stateLabel[activeTab]}种子`} />
        ) : (
          <ActivityTable items={items} activeTab={activeTab} />
        )}
      </CardContent>
    </Card>
  )
}

function ActivityTable({
  items,
  activeTab,
}: {
  items: Submission[]
  activeTab: ActivityTab
}) {
  const showState = activeTab !== "published"

  return (
    <div className="overflow-hidden rounded-lg border">
      <Table>
        <TableHeader className="bg-muted text-xs text-muted-foreground">
          <TableRow>
            <TableHead className="w-full min-w-72 pl-3">名称</TableHead>
            {showState ? (
              <TableHead className="w-24 text-center">状态</TableHead>
            ) : null}
            <TableHead className="w-24 text-right">分类</TableHead>
            <TableHead className="w-28 text-right">大小</TableHead>
            <TableHead className="w-36 pr-3 text-right">状态时间</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {items.map((submission) => (
            <TableRow key={submission.id}>
              <TableCell className="max-w-0 pl-3">
                <SubmissionName submission={submission} />
              </TableCell>
              {showState ? (
                <TableCell className="text-center">
                  <SubmissionStateBadge submission={submission} />
                </TableCell>
              ) : null}
              <TableCell className="text-right text-muted-foreground">
                {submission.category.name}
              </TableCell>
              <TableCell className="text-right">
                {formatBytes(submission.total_size_bytes)}
              </TableCell>
              <TableCell className="pr-3 text-right text-xs text-muted-foreground">
                <time dateTime={submission.state_changed_at}>
                  {formatCompactDateTime(submission.state_changed_at)}
                </time>
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  )
}

function SubmissionName({ submission }: { submission: Submission }) {
  const content = (
    <>
      <span className="truncate font-medium">{submission.title}</span>
      <span className="truncate text-xs text-muted-foreground">
        {submission.subtitle || submission.content_name}
      </span>
    </>
  )

  if (submission.state !== "published") {
    return <div className="flex min-w-0 flex-col gap-0.5">{content}</div>
  }

  return (
    <Link
      to={`/torrents/${submission.id}`}
      className="flex min-w-0 flex-col gap-0.5 rounded-sm underline-offset-4 hover:text-primary focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
    >
      {content}
    </Link>
  )
}

function SubmissionStateBadge({ submission }: { submission: Submission }) {
  if (submission.state === "pending_review") {
    return <Badge variant="secondary">审核中</Badge>
  }
  return (
    <Badge variant="destructive">
      {submission.resubmission_allowed ? "待修改" : "已拒绝"}
    </Badge>
  )
}

function ActivityEmpty({ title }: { title: string }) {
  return (
    <Empty className="min-h-20 rounded-none p-0">
      <EmptyHeader>
        <EmptyTitle className="font-normal text-muted-foreground">
          {title}
        </EmptyTitle>
      </EmptyHeader>
    </Empty>
  )
}

function countByState(items: Submission[]) {
  return items.reduce<Record<ActivityTab, number>>(
    (counts, submission) => {
      if (
        submission.state === "published" ||
        submission.state === "pending_review" ||
        submission.state === "rejected"
      ) {
        counts[submission.state] += 1
      }
      return counts
    },
    { published: 0, pending_review: 0, rejected: 0 }
  )
}

function formatTabCount(
  count: number,
  page: MyTorrentSubmissionPage | undefined,
  windowIsComplete: boolean
) {
  if (!page) return "—"
  return `${count.toLocaleString("zh-CN")}${windowIsComplete ? "" : "+"}`
}

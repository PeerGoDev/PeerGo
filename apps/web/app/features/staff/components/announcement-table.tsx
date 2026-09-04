import { Link } from "react-router"
import {
  CalendarClockIcon,
  EllipsisIcon,
  EyeIcon,
  FilePenLineIcon,
  MegaphoneIcon,
  SendIcon,
  Undo2Icon,
} from "lucide-react"

import { Badge } from "~/components/ui/badge"
import { Button, buttonVariants } from "~/components/ui/button"
import {
  Card,
  CardAction,
  CardContent,
  CardHeader,
  CardTitle,
} from "~/components/ui/card"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "~/components/ui/dropdown-menu"
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "~/components/ui/empty"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "~/components/ui/table"
import type {
  AnnouncementPublicationAction,
  ManagedAnnouncementSummary,
} from "~/features/staff/api/announcement-administration.queries"
import { cn } from "~/lib/utils"
import { formatDateTime } from "~/shared/formatters/date-time"

export function AnnouncementTable({
  announcements,
  hasFilters = false,
  canUpdate,
  canPublish,
  canWithdraw,
  onOpen,
  onPublication,
}: {
  announcements: ManagedAnnouncementSummary[]
  hasFilters?: boolean
  canUpdate: boolean
  canPublish: boolean
  canWithdraw: boolean
  onOpen: (announcement: ManagedAnnouncementSummary) => void
  onPublication: (
    announcement: ManagedAnnouncementSummary,
    action: AnnouncementPublicationAction
  ) => void
}) {
  if (announcements.length === 0) {
    return (
      <Empty className="min-h-64 border">
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <MegaphoneIcon />
          </EmptyMedia>
          <EmptyTitle>
            {hasFilters ? "没有匹配公告" : "尚未建立公告"}
          </EmptyTitle>
          <EmptyDescription>
            {hasFilters
              ? "请调整标题、标识、摘要、发布状态或修订状态后重试。"
              : "新公告先保存为草稿，完成预览后再选择立即或预约发布。"}
          </EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  }

  return (
    <>
      <div className="hidden overflow-hidden rounded-lg border bg-card shadow-sm md:block">
        <Table>
          <TableHeader className="bg-muted">
            <TableRow>
              <TableHead className="h-auto px-4 py-3 font-semibold">
                公告
              </TableHead>
              <TableHead className="h-auto px-4 py-3 font-semibold">
                标识 / 版本
              </TableHead>
              <TableHead className="h-auto px-4 py-3 font-semibold">
                状态
              </TableHead>
              <TableHead className="h-auto px-4 py-3 font-semibold">
                公开 / 排期
              </TableHead>
              <TableHead className="h-auto px-4 py-3 font-semibold">
                最近变更
              </TableHead>
              <TableHead className="h-auto px-4 py-3 text-right font-semibold">
                操作
              </TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {announcements.map((announcement) => (
              <TableRow key={announcement.id} className="h-[70px]">
                <TableCell className="max-w-sm px-4 py-2.5 whitespace-normal">
                  <div className="flex min-w-48 flex-col gap-0.5">
                    <div className="flex flex-wrap items-center gap-2">
                      <span className="line-clamp-1 font-medium break-words">
                        {announcement.title}
                      </span>
                      {announcement.has_unpublished_changes ? (
                        <Badge variant="secondary">有未发布修订</Badge>
                      ) : null}
                    </div>
                    <span className="line-clamp-1 text-xs text-muted-foreground">
                      {announcement.summary}
                    </span>
                  </div>
                </TableCell>
                <TableCell className="px-4 py-2.5">
                  <div className="flex flex-col gap-0.5">
                    <code className="max-w-44 truncate text-[11px] text-muted-foreground">
                      {announcement.id}
                    </code>
                    <span className="text-[11px] text-muted-foreground">
                      状态第 {announcement.version} 版 · 正文第
                      {announcement.revision_number} 稿
                    </span>
                  </div>
                </TableCell>
                <TableCell className="px-4 py-2.5">
                  <AnnouncementStatusBadge status={announcement.status} />
                </TableCell>
                <TableCell className="px-4 py-2.5">
                  <AnnouncementTiming announcement={announcement} />
                </TableCell>
                <TableCell className="px-4 py-2.5">
                  <time
                    dateTime={announcement.updated_at}
                    className="text-xs text-muted-foreground"
                  >
                    {formatDateTime(announcement.updated_at)}
                  </time>
                </TableCell>
                <TableCell className="px-4 py-2.5 text-right">
                  <AnnouncementActions
                    announcement={announcement}
                    canUpdate={canUpdate}
                    canPublish={canPublish}
                    canWithdraw={canWithdraw}
                    iconOnly
                    onOpen={onOpen}
                    onPublication={onPublication}
                  />
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>

      <div className="grid gap-3 md:hidden">
        {announcements.map((announcement) => (
          <Card key={announcement.id} size="sm">
            <CardHeader>
              <CardTitle className="pr-24 leading-snug">
                {announcement.title}
              </CardTitle>
              <code className="text-xs text-muted-foreground">
                {announcement.id} · 状态第 {announcement.version} 版 / 正文第
                {announcement.revision_number} 稿
              </code>
              <CardAction>
                <AnnouncementStatusBadge status={announcement.status} />
              </CardAction>
            </CardHeader>
            <CardContent className="flex flex-col gap-3 text-xs">
              <p className="line-clamp-3 leading-relaxed text-muted-foreground">
                {announcement.summary}
              </p>
              <AnnouncementTiming announcement={announcement} />
              <div className="flex items-center justify-between gap-3 border-t pt-3">
                <time
                  dateTime={announcement.updated_at}
                  className="text-muted-foreground"
                >
                  更新于 {formatDateTime(announcement.updated_at)}
                </time>
                <AnnouncementActions
                  announcement={announcement}
                  canUpdate={canUpdate}
                  canPublish={canPublish}
                  canWithdraw={canWithdraw}
                  onOpen={onOpen}
                  onPublication={onPublication}
                />
              </div>
            </CardContent>
          </Card>
        ))}
      </div>
    </>
  )
}

export function AnnouncementStatusBadge({
  status,
}: {
  status: ManagedAnnouncementSummary["status"]
}) {
  switch (status) {
    case "published":
      return (
        <Badge
          variant="outline"
          className="border-success/30 bg-success/10 text-success-foreground"
        >
          已发布
        </Badge>
      )
    case "scheduled":
      return (
        <Badge
          variant="outline"
          className="border-primary/30 bg-primary/10 text-primary"
        >
          已排期
        </Badge>
      )
    case "withdrawn":
      return <Badge variant="destructive">已撤回</Badge>
    case "draft":
      return <Badge variant="secondary">草稿</Badge>
  }
}

function AnnouncementTiming({
  announcement,
}: {
  announcement: ManagedAnnouncementSummary
}) {
  if (announcement.status === "scheduled" && announcement.scheduled_for) {
    return (
      <span className="flex items-center gap-1.5 text-xs text-primary">
        <CalendarClockIcon className="size-3.5" />
        {formatDateTime(announcement.scheduled_for)}
      </span>
    )
  }
  if (announcement.published_at) {
    return (
      <span className="text-xs text-muted-foreground">
        发布于 {formatDateTime(announcement.published_at)}
      </span>
    )
  }
  return <span className="text-xs text-muted-foreground">尚未公开</span>
}

function AnnouncementActions({
  announcement,
  canUpdate,
  canPublish,
  canWithdraw,
  iconOnly = false,
  onOpen,
  onPublication,
}: {
  announcement: ManagedAnnouncementSummary
  canUpdate: boolean
  canPublish: boolean
  canWithdraw: boolean
  iconOnly?: boolean
  onOpen: (announcement: ManagedAnnouncementSummary) => void
  onPublication: (
    announcement: ManagedAnnouncementSummary,
    action: AnnouncementPublicationAction
  ) => void
}) {
  const publishable =
    canPublish &&
    announcement.has_unpublished_changes &&
    announcement.status !== "scheduled"
  const cancelable = canPublish && announcement.status === "scheduled"
  const withdrawable =
    canWithdraw &&
    (announcement.status === "published" || announcement.status === "scheduled")
  const hasMenu = publishable || cancelable || withdrawable

  return (
    <div className="inline-flex items-center gap-1">
      <Button
        variant="ghost"
        size={iconOnly ? "icon-sm" : "sm"}
        className={iconOnly ? "size-8" : undefined}
        onClick={() => onOpen(announcement)}
        aria-label={`${canUpdate ? "编辑" : "查看"}公告 ${announcement.title}`}
      >
        <FilePenLineIcon data-icon={iconOnly ? undefined : "inline-start"} />
        {iconOnly ? null : canUpdate ? "编辑" : "查看"}
      </Button>
      {announcement.status === "published" ? (
        <Link
          to={`/announcements/${encodeURIComponent(announcement.id)}`}
          target="_blank"
          rel="noreferrer"
          aria-label={`查看公开公告 ${announcement.title}`}
          className={cn(
            buttonVariants({
              variant: "ghost",
              size: iconOnly ? "icon-sm" : "sm",
            }),
            iconOnly && "size-8"
          )}
        >
          <EyeIcon data-icon={iconOnly ? undefined : "inline-start"} />
          {iconOnly ? null : "公开页"}
        </Link>
      ) : null}
      {hasMenu ? (
        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <Button
                variant="ghost"
                size="icon-sm"
                aria-label={`管理公告 ${announcement.title}`}
              />
            }
          >
            <EllipsisIcon />
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="w-44">
            <DropdownMenuLabel>发布状态</DropdownMenuLabel>
            {publishable ? (
              <>
                <DropdownMenuItem
                  onClick={() => onPublication(announcement, "publish_now")}
                >
                  <SendIcon />
                  立即发布草稿
                </DropdownMenuItem>
                <DropdownMenuItem
                  onClick={() => onPublication(announcement, "schedule")}
                >
                  <CalendarClockIcon />
                  预约发布草稿
                </DropdownMenuItem>
              </>
            ) : null}
            {cancelable ? (
              <DropdownMenuItem
                onClick={() => onPublication(announcement, "cancel_schedule")}
              >
                <Undo2Icon />
                取消排期
              </DropdownMenuItem>
            ) : null}
            {withdrawable ? (
              <>
                <DropdownMenuSeparator />
                <DropdownMenuItem
                  variant="destructive"
                  onClick={() => onPublication(announcement, "withdraw")}
                >
                  <Undo2Icon />
                  撤回公告
                </DropdownMenuItem>
              </>
            ) : null}
          </DropdownMenuContent>
        </DropdownMenu>
      ) : null}
    </div>
  )
}

import * as React from "react"
import { useQuery } from "@tanstack/react-query"
import {
  CheckCircle2Icon,
  CircleAlertIcon,
  MegaphoneIcon,
  PlusIcon,
  RefreshCwIcon,
  SearchIcon,
  XIcon,
} from "lucide-react"

import {
  Alert,
  AlertAction,
  AlertDescription,
  AlertTitle,
} from "~/components/ui/alert"
import { Button } from "~/components/ui/button"
import { Field, FieldLabel } from "~/components/ui/field"
import {
  InputGroup,
  InputGroupAddon,
  InputGroupButton,
  InputGroupInput,
} from "~/components/ui/input-group"
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
import {
  type AnnouncementPublicationAction,
  type ManagedAnnouncementSummary,
  managedAnnouncementListQueryOptions,
} from "~/features/staff/api/announcement-administration.queries"
import { AnnouncementEditorSheet } from "~/features/staff/components/announcement-editor-sheet"
import { AnnouncementPublicationDialog } from "~/features/staff/components/announcement-publication-dialog"
import { AnnouncementTable } from "~/features/staff/components/announcement-table"
import { StaffAccessGate } from "~/features/staff/components/staff-access-gate"
import { StaffPageHeader } from "~/features/staff/components/staff-page-header"
import { StaffPageFrame } from "~/features/staff/components/staff-page-frame"
import { hasCapability } from "~/features/staff/model/capability"
import { publicationActionLabel } from "~/features/staff/model/announcement-form"
import {
  type AnnouncementRevisionFilter,
  type AnnouncementStatusFilter,
  filterManagedAnnouncements,
} from "~/features/staff/model/announcement-list-filter"
import type { components } from "~/generated/api"

type CapabilityList = components["schemas"]["CapabilityList"]

type EditorState = { mode: "create" } | { mode: "edit"; id: string }
type PublicationState = {
  announcement: ManagedAnnouncementSummary
  action: AnnouncementPublicationAction
}

const pageSize = 50

const statusOptions: Array<{
  label: string
  value: AnnouncementStatusFilter
}> = [
  { label: "全部", value: "all" },
  { label: "草稿", value: "draft" },
  { label: "已排期", value: "scheduled" },
  { label: "已发布", value: "published" },
  { label: "已撤回", value: "withdrawn" },
]

const revisionOptions: Array<{
  label: string
  value: AnnouncementRevisionFilter
}> = [
  { label: "全部", value: "all" },
  { label: "有未发布修订", value: "changed" },
  { label: "已同步公开版本", value: "current" },
]

export function StaffAnnouncementsPage() {
  return (
    <StaffAccessGate
      requiredAction="announcement.manage.read"
      pageHeader={{
        title: "公告管理",
        icon: MegaphoneIcon,
        variant: "hero",
        frameClassName: "gap-0",
        contentClassName: "px-4 py-8",
      }}
    >
      {({ session, capabilities }) => (
        <AnnouncementsContent
          csrfToken={session.csrf_token}
          capabilities={capabilities}
        />
      )}
    </StaffAccessGate>
  )
}

function AnnouncementsContent({
  csrfToken,
  capabilities,
}: {
  csrfToken: string
  capabilities: CapabilityList
}) {
  const [offset, setOffset] = React.useState(0)
  const announcements = useQuery(
    managedAnnouncementListQueryOptions(pageSize, offset)
  )
  const [editor, setEditor] = React.useState<EditorState>()
  const [publication, setPublication] = React.useState<PublicationState>()
  const [successMessage, setSuccessMessage] = React.useState("")
  const [query, setQuery] = React.useState("")
  const [status, setStatus] = React.useState<AnnouncementStatusFilter>("all")
  const [revision, setRevision] =
    React.useState<AnnouncementRevisionFilter>("all")
  const canCreate = hasCapability(capabilities, "announcement.create")
  const canUpdate = hasCapability(capabilities, "announcement.update")
  const canPublish = hasCapability(capabilities, "announcement.publish")
  const canWithdraw = hasCapability(capabilities, "announcement.withdraw")

  if (announcements.isPending) {
    return <AnnouncementsSkeleton />
  }
  if (announcements.isError || !announcements.data) {
    return (
      <AnnouncementsFrame>
        <AnnouncementsHeader />
        <Alert variant="destructive">
          <CircleAlertIcon />
          <AlertTitle>公告工作队列暂时无法读取</AlertTitle>
          <AlertDescription>
            暂时无法取得公告数据，已经发布的公告不受影响。
          </AlertDescription>
          <AlertAction>
            <Button
              variant="outline"
              size="sm"
              onClick={() => void announcements.refetch()}
            >
              <RefreshCwIcon data-icon="inline-start" />
              重试
            </Button>
          </AlertAction>
        </Alert>
      </AnnouncementsFrame>
    )
  }

  const page = announcements.data
  const filteredAnnouncements = filterManagedAnnouncements(page.items, {
    query,
    status,
    revision,
  })
  const pageEnd = Math.min(page.offset + page.items.length, page.total)
  const hasPrevious = page.offset > 0
  const hasNext = pageEnd < page.total

  return (
    <AnnouncementsFrame>
      <AnnouncementsHeader />

      <AnnouncementToolbar
        query={query}
        status={status}
        revision={revision}
        visibleCount={filteredAnnouncements.length}
        loadedCount={page.items.length}
        totalCount={page.total}
        canCreate={canCreate}
        isFetching={announcements.isFetching}
        onQueryChange={setQuery}
        onStatusChange={setStatus}
        onRevisionChange={setRevision}
        onCreate={() => {
          setSuccessMessage("")
          setEditor({ mode: "create" })
        }}
        onRefresh={() => void announcements.refetch()}
      />

      {successMessage ? (
        <Alert>
          <CheckCircle2Icon />
          <AlertTitle>公告工作流已更新</AlertTitle>
          <AlertDescription>{successMessage}</AlertDescription>
        </Alert>
      ) : null}

      {!canCreate && !canUpdate && !canPublish && !canWithdraw ? (
        <Alert>
          <MegaphoneIcon />
          <AlertTitle>当前权限仅可查看</AlertTitle>
          <AlertDescription>
            可以查看正文、版本与发布状态，但不能编辑或改变公开状态。
          </AlertDescription>
        </Alert>
      ) : null}

      <AnnouncementTable
        announcements={filteredAnnouncements}
        hasFilters={
          Boolean(query.trim()) || status !== "all" || revision !== "all"
        }
        canUpdate={canUpdate}
        canPublish={canPublish}
        canWithdraw={canWithdraw}
        onOpen={(announcement) => {
          setSuccessMessage("")
          setEditor({ mode: "edit", id: announcement.id })
        }}
        onPublication={(announcement, action) => {
          setSuccessMessage("")
          setPublication({ announcement, action })
        }}
      />
      {page.total > pageSize ? (
        <div className="flex flex-wrap items-center justify-between gap-3 rounded-lg border bg-card px-4 py-3">
          <span className="text-xs text-muted-foreground">
            显示 {page.offset + 1}–{pageEnd}，共 {page.total} 篇
          </span>
          <div className="flex gap-2">
            <Button
              variant="outline"
              size="sm"
              disabled={!hasPrevious || announcements.isFetching}
              onClick={() => setOffset(Math.max(0, offset - pageSize))}
            >
              上一页
            </Button>
            <Button
              variant="outline"
              size="sm"
              disabled={!hasNext || announcements.isFetching}
              onClick={() => setOffset(offset + pageSize)}
            >
              下一页
            </Button>
          </div>
        </div>
      ) : null}

      {editor ? (
        <AnnouncementEditorSheet
          key={editor.mode === "create" ? "create" : editor.id}
          announcementId={editor.mode === "edit" ? editor.id : undefined}
          csrfToken={csrfToken}
          canUpdate={canUpdate}
          onOpenChange={(open) => {
            if (!open) {
              setEditor(undefined)
            }
          }}
          onSaved={(saved, mode) => {
            setSuccessMessage(
              mode === "created"
                ? `已创建“${saved.title}”的首个草稿，尚未公开。`
                : `已为“${saved.title}”追加第 ${saved.revision_number} 稿，当前公开版本未被直接覆盖。`
            )
            setEditor(undefined)
          }}
        />
      ) : null}

      {publication ? (
        <AnnouncementPublicationDialog
          key={`${publication.announcement.id}:${publication.action}:${publication.announcement.version}`}
          announcement={publication.announcement}
          action={publication.action}
          csrfToken={csrfToken}
          onOpenChange={(open) => {
            if (!open) {
              setPublication(undefined)
            }
          }}
          onSaved={(saved, action) => {
            setSuccessMessage(
              `${publicationActionLabel(action)}已完成：“${saved.title}”当前为第 ${saved.version} 版。`
            )
            setPublication(undefined)
          }}
        />
      ) : null}
    </AnnouncementsFrame>
  )
}

function AnnouncementsHeader({
  children,
  meta,
}: {
  children?: React.ReactNode
  meta?: string
}) {
  return (
    <StaffPageHeader
      title="公告管理"
      meta={meta}
      actions={children}
      icon={MegaphoneIcon}
      variant="hero"
    />
  )
}

function AnnouncementToolbar({
  query,
  status,
  revision,
  visibleCount,
  loadedCount,
  totalCount,
  canCreate,
  isFetching,
  onQueryChange,
  onStatusChange,
  onRevisionChange,
  onCreate,
  onRefresh,
}: {
  query: string
  status: AnnouncementStatusFilter
  revision: AnnouncementRevisionFilter
  visibleCount: number
  loadedCount: number
  totalCount: number
  canCreate: boolean
  isFetching: boolean
  onQueryChange: (value: string) => void
  onStatusChange: (value: AnnouncementStatusFilter) => void
  onRevisionChange: (value: AnnouncementRevisionFilter) => void
  onCreate: () => void
  onRefresh: () => void
}) {
  return (
    <section aria-label="公告筛选与操作" className="flex flex-col gap-8">
      <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        {canCreate ? (
          <Button variant="actionInfo" className="w-35" onClick={onCreate}>
            <PlusIcon data-icon="inline-start" />
            创建新公告
          </Button>
        ) : (
          <span />
        )}

        <Field className="w-full sm:max-w-md">
          <FieldLabel htmlFor="announcement-search" className="sr-only">
            搜索公告
          </FieldLabel>
          <InputGroup className="h-[42px] rounded-lg">
            <InputGroupInput
              id="announcement-search"
              value={query}
              maxLength={160}
              onChange={(event) => onQueryChange(event.target.value)}
              placeholder="搜索标题、标识或摘要…"
            />
            <InputGroupAddon>
              <SearchIcon />
            </InputGroupAddon>
            {query ? (
              <InputGroupAddon align="inline-end">
                <InputGroupButton
                  size="icon-xs"
                  aria-label="清空公告搜索"
                  onClick={() => onQueryChange("")}
                >
                  <XIcon />
                </InputGroupButton>
              </InputGroupAddon>
            ) : null}
          </InputGroup>
        </Field>
      </div>

      <div className="flex flex-wrap items-center gap-x-4 gap-y-3">
        <AnnouncementSelectFilter
          id="announcement-status-filter"
          label="发布状态"
          value={status}
          options={statusOptions}
          onChange={onStatusChange}
        />
        <AnnouncementSelectFilter
          id="announcement-revision-filter"
          label="修订状态"
          value={revision}
          options={revisionOptions}
          onChange={onRevisionChange}
        />
        <span className="ml-auto text-sm text-muted-foreground">
          显示 {visibleCount.toLocaleString("zh-CN")} /{" "}
          {loadedCount.toLocaleString("zh-CN")} 条公告
          {totalCount > loadedCount
            ? `，共 ${totalCount.toLocaleString("zh-CN")} 条`
            : ""}
        </span>
        <Button
          variant="outline"
          size="icon-sm"
          className="size-8"
          onClick={onRefresh}
          disabled={isFetching}
          aria-label={isFetching ? "正在刷新公告列表" : "刷新公告列表"}
        >
          <RefreshCwIcon />
        </Button>
      </div>
    </section>
  )
}

function AnnouncementSelectFilter<T extends string>({
  id,
  label,
  value,
  options,
  onChange,
}: {
  id: string
  label: string
  value: T
  options: Array<{ label: string; value: T }>
  onChange: (value: T) => void
}) {
  return (
    <Field orientation="horizontal" className="w-auto gap-2">
      <FieldLabel htmlFor={id} className="whitespace-nowrap">
        {label}:
      </FieldLabel>
      <Select
        items={options}
        value={value}
        onValueChange={(nextValue) => {
          if (nextValue) onChange(nextValue)
        }}
      >
        <SelectTrigger id={id} size="xs" aria-label={label}>
          <SelectValue />
        </SelectTrigger>
        <SelectContent alignItemWithTrigger={false}>
          <SelectGroup>
            <SelectLabel>{label}</SelectLabel>
            {options.map((option) => (
              <SelectItem key={option.value} value={option.value}>
                {option.label}
              </SelectItem>
            ))}
          </SelectGroup>
        </SelectContent>
      </Select>
    </Field>
  )
}

function AnnouncementsFrame({ children }: { children: React.ReactNode }) {
  return (
    <StaffPageFrame className="gap-0">
      <div className="flex min-w-0 flex-col gap-6 px-4 py-6 md:py-8">
        {children}
      </div>
    </StaffPageFrame>
  )
}

function AnnouncementsSkeleton() {
  return (
    <AnnouncementsFrame>
      <div
        className="flex flex-col gap-2"
        aria-label="正在加载公告工作队列"
        aria-busy="true"
      >
        <Skeleton className="h-9 w-48" />
        <Skeleton className="h-4 w-full max-w-2xl" />
      </div>
      <Skeleton className="h-[30rem] w-full rounded-xl" />
    </AnnouncementsFrame>
  )
}

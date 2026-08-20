import * as React from "react"
import { useQuery } from "@tanstack/react-query"
import { useSearchParams } from "react-router"
import {
  ChevronLeftIcon,
  ChevronRightIcon,
  CircleAlertIcon,
  HardDriveIcon,
  RefreshCwIcon,
  SearchIcon,
  ShieldCheckIcon,
  XIcon,
} from "lucide-react"

import {
  Alert,
  AlertAction,
  AlertDescription,
  AlertTitle,
} from "~/components/ui/alert"
import { Button } from "~/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "~/components/ui/card"
import { Field, FieldLabel } from "~/components/ui/field"
import {
  InputGroup,
  InputGroupAddon,
  InputGroupButton,
  InputGroupInput,
} from "~/components/ui/input-group"
import {
  Pagination,
  PaginationContent,
  PaginationItem,
} from "~/components/ui/pagination"
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "~/components/ui/select"
import { Skeleton } from "~/components/ui/skeleton"
import { ToggleGroup, ToggleGroupItem } from "~/components/ui/toggle-group"
import {
  managedTorrentListQueryOptions,
  type ManagedTorrent,
  type ManagedTorrentPage,
} from "~/features/staff/api/torrent-administration.queries"
import { ManagedTorrentTable } from "~/features/staff/components/managed-torrent-table"
import { PublishedContentChangeReview } from "~/features/staff/components/published-content-change-review"
import { PublishedScreenshotChangeReview } from "~/features/staff/components/published-screenshot-change-review"
import { StaffAccessGate } from "~/features/staff/components/staff-access-gate"
import { StaffPageFrame } from "~/features/staff/components/staff-page-frame"
import { TorrentAvailabilityDialog } from "~/features/staff/components/torrent-availability-dialog"
import { TorrentPurchasePriceDialog } from "~/features/staff/components/torrent-purchase-price-dialog"
import { TorrentWithdrawalReview } from "~/features/staff/components/torrent-withdrawal-review"
import { TorrentReportReview } from "~/features/staff/components/torrent-report-review"
import { hasCapability } from "~/features/staff/model/capability"
import {
  managedTorrentSearchParams,
  managedTorrentStateLabel,
  parseManagedTorrentFilters,
  type ManagedTorrentFilters,
  type ManagedTorrentStateFilter,
} from "~/features/staff/model/torrent-administration"

const stateFilters: ManagedTorrentStateFilter[] = [
  "all",
  "pending_review",
  "published",
  "rejected",
  "disabled",
  "deleted",
]

export function StaffTorrentsPage() {
  return (
    <StaffAccessGate
      requiredAction="torrent.manage.read"
      pageHeader={{
        title: "种子管理",
        description:
          "统一查询种子生命周期、分类、上传者、优惠展示与 Tracker 统计。",
      }}
    >
      {({ session, capabilities }) => (
        <TorrentWorkbench
          csrfToken={session.csrf_token}
          canChangeAvailability={hasCapability(
            capabilities,
            "torrent.lifecycle.update"
          )}
          canManagePurchase={hasCapability(
            capabilities,
            "torrent.purchase.manage.update"
          )}
          canReviewContent={hasCapability(
            capabilities,
            "torrent.content.change.review"
          )}
          canReviewScreenshots={hasCapability(
            capabilities,
            "torrent.screenshot.change.review"
          )}
          canReviewWithdrawals={hasCapability(
            capabilities,
            "torrent.withdraw.review"
          )}
          canReviewReports={hasCapability(
            capabilities,
            "torrent.report.review"
          )}
        />
      )}
    </StaffAccessGate>
  )
}

function TorrentWorkbench({
  csrfToken,
  canChangeAvailability,
  canManagePurchase,
  canReviewContent,
  canReviewScreenshots,
  canReviewWithdrawals,
  canReviewReports,
}: {
  csrfToken: string
  canChangeAvailability: boolean
  canManagePurchase: boolean
  canReviewContent: boolean
  canReviewScreenshots: boolean
  canReviewWithdrawals: boolean
  canReviewReports: boolean
}) {
  const [searchParams, setSearchParams] = useSearchParams()
  const filters = React.useMemo(
    () => parseManagedTorrentFilters(searchParams),
    [searchParams]
  )
  const torrents = useQuery(managedTorrentListQueryOptions(filters))
  const [queryDraft, setQueryDraft] = React.useState(filters.query)
  const [availabilityTarget, setAvailabilityTarget] =
    React.useState<ManagedTorrent>()
  const [purchasePriceTarget, setPurchasePriceTarget] =
    React.useState<ManagedTorrent>()
  const [successMessage, setSuccessMessage] = React.useState("")

  React.useEffect(() => setQueryDraft(filters.query), [filters.query])

  React.useEffect(() => {
    if (!torrents.data) return
    const lastPage = Math.ceil(torrents.data.total / torrents.data.limit)
    if (lastPage > 0 && filters.page > lastPage) {
      setSearchParams(
        managedTorrentSearchParams({ ...filters, page: lastPage }),
        { replace: true }
      )
    }
  }, [filters, setSearchParams, torrents.data])

  function updateFilters(update: Partial<ManagedTorrentFilters>) {
    setSuccessMessage("")
    setSearchParams(managedTorrentSearchParams({ ...filters, ...update }))
  }

  function handleSearch(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    updateFilters({ query: queryDraft.trim(), page: 1 })
  }

  if (torrents.isPending) return <TorrentWorkbenchSkeleton />
  if (torrents.isError || !torrents.data) {
    return (
      <StaffPageFrame className="gap-4">
        <h1 className="font-heading text-xl font-semibold">种子管理</h1>
        <Alert variant="destructive">
          <CircleAlertIcon />
          <AlertTitle>种子工作台暂时无法读取</AlertTitle>
          <AlertDescription>
            请检查后台登录状态与 Core 服务后重试。
          </AlertDescription>
          <AlertAction>
            <Button
              variant="outline"
              size="sm"
              onClick={() => void torrents.refetch()}
            >
              <RefreshCwIcon data-icon="inline-start" />
              重试
            </Button>
          </AlertAction>
        </Alert>
      </StaffPageFrame>
    )
  }

  const data = torrents.data
  const totalPages = Math.ceil(data.total / data.limit)
  const categoryItems = [
    { label: "全部分类", value: "all" },
    ...data.categories.map((category) => ({
      label: category.enabled ? category.name : `${category.name}（已停用）`,
      value: category.id,
    })),
  ]
  return (
    <StaffPageFrame className="gap-4">
      {!canChangeAvailability ? (
        <Alert>
          <ShieldCheckIcon />
          <AlertTitle>当前权限仅可查看</AlertTitle>
          <AlertDescription>
            可以查询全部种子状态，但不能下架或恢复种子。
          </AlertDescription>
        </Alert>
      ) : null}
      {successMessage ? (
        <Alert>
          <ShieldCheckIcon />
          <AlertTitle>操作已经提交</AlertTitle>
          <AlertDescription>{successMessage}</AlertDescription>
        </Alert>
      ) : null}

      <PublishedContentChangeReview
        enabled={canReviewContent}
        csrfToken={csrfToken}
        onDecided={setSuccessMessage}
      />

      <PublishedScreenshotChangeReview
        enabled={canReviewScreenshots}
        csrfToken={csrfToken}
        onDecided={setSuccessMessage}
      />

      <TorrentWithdrawalReview
        enabled={canReviewWithdrawals}
        csrfToken={csrfToken}
        onDecided={setSuccessMessage}
      />

      <TorrentReportReview
        enabled={canReviewReports}
        csrfToken={csrfToken}
        onDecided={setSuccessMessage}
      />

      <Card className="gap-0 py-0">
        <CardHeader className="p-6 pb-3">
          <div className="flex flex-col gap-3 xl:flex-row xl:items-start xl:justify-between">
            <CardTitle className="flex min-h-8 items-center gap-2 text-2xl">
              <HardDriveIcon className="size-5" aria-hidden="true" />
              <h1>种子管理</h1>
              <span className="text-sm font-normal text-muted-foreground">
                ({data.total.toLocaleString("zh-CN")} 条记录)
              </span>
            </CardTitle>
            <div className="grid min-w-0 grid-cols-1 gap-2 sm:grid-cols-[minmax(220px,340px)_160px_32px]">
              <form onSubmit={handleSearch}>
                <Field>
                  <FieldLabel
                    htmlFor="managed-torrent-search"
                    className="sr-only"
                  >
                    搜索种子 ID、名称或上传者
                  </FieldLabel>
                  <InputGroup className="h-8">
                    <InputGroupInput
                      id="managed-torrent-search"
                      value={queryDraft}
                      maxLength={100}
                      placeholder="搜索 ID / 名称 / 上传者..."
                      onChange={(event) => setQueryDraft(event.target.value)}
                    />
                    {queryDraft ? (
                      <InputGroupAddon align="inline-end">
                        <InputGroupButton
                          size="icon-xs"
                          aria-label="清空种子搜索"
                          onClick={() => setQueryDraft("")}
                        >
                          <XIcon />
                        </InputGroupButton>
                      </InputGroupAddon>
                    ) : null}
                    <InputGroupAddon align="inline-end">
                      <InputGroupButton
                        type="submit"
                        size="icon-xs"
                        aria-label="搜索种子"
                      >
                        <SearchIcon />
                      </InputGroupButton>
                    </InputGroupAddon>
                  </InputGroup>
                </Field>
              </form>
              <Select
                items={categoryItems}
                value={filters.categoryId || "all"}
                onValueChange={(value) =>
                  updateFilters({
                    categoryId: value === "all" || !value ? "" : value,
                    page: 1,
                  })
                }
              >
                <SelectTrigger
                  size="xs"
                  className="w-full"
                  aria-label="按种子分类筛选"
                >
                  <SelectValue>
                    {categoryItems.find(
                      (item) => item.value === (filters.categoryId || "all")
                    )?.label ?? "全部分类"}
                  </SelectValue>
                </SelectTrigger>
                <SelectContent align="end" alignItemWithTrigger={false}>
                  <SelectGroup>
                    {categoryItems.map((item) => (
                      <SelectItem key={item.value} value={item.value}>
                        {item.label}
                      </SelectItem>
                    ))}
                  </SelectGroup>
                </SelectContent>
              </Select>
              <Button
                variant="outline"
                size="icon-sm"
                className="size-8"
                onClick={() => void torrents.refetch()}
                disabled={torrents.isFetching}
                aria-label="刷新种子工作台"
              >
                <RefreshCwIcon />
              </Button>
            </div>
          </div>

          <div className="overflow-x-auto pb-1">
            <ToggleGroup
              value={[filters.state]}
              onValueChange={(values) => {
                const next = values[0] as ManagedTorrentStateFilter | undefined
                if (next) updateFilters({ state: next, page: 1 })
              }}
              variant="outline"
              size="sm"
              spacing={0}
              aria-label="按种子生命周期筛选"
            >
              {stateFilters.map((state) => (
                <ToggleGroupItem
                  key={state}
                  value={state}
                  aria-label={managedTorrentStateLabel(state)}
                >
                  {managedTorrentStateLabel(state)}
                  <span className="text-xs text-muted-foreground tabular-nums">
                    {stateCount(data, state).toLocaleString("zh-CN")}
                  </span>
                </ToggleGroupItem>
              ))}
            </ToggleGroup>
          </div>
        </CardHeader>

        <CardContent className="flex flex-col gap-3 p-6 pt-0">
          <ManagedTorrentTable
            torrents={data.items}
            hasFilters={
              Boolean(filters.query || filters.categoryId) ||
              filters.state !== "all"
            }
            canChangeAvailability={canChangeAvailability}
            onChangeAvailability={setAvailabilityTarget}
            canManagePurchase={canManagePurchase}
            onManagePurchase={setPurchasePriceTarget}
          />
          {totalPages > 1 ? (
            <Pagination
              className="justify-between pt-1"
              aria-label="种子管理分页"
            >
              <span className="text-sm text-muted-foreground">
                共 {data.total.toLocaleString("zh-CN")} 条记录
              </span>
              <PaginationContent>
                <PaginationItem>
                  <Button
                    variant="outline"
                    size="sm"
                    disabled={filters.page === 1}
                    onClick={() => updateFilters({ page: filters.page - 1 })}
                  >
                    <ChevronLeftIcon data-icon="inline-start" />
                    上一页
                  </Button>
                </PaginationItem>
                <PaginationItem>
                  <span className="px-3 text-sm text-muted-foreground tabular-nums">
                    {filters.page} / {totalPages}
                  </span>
                </PaginationItem>
                <PaginationItem>
                  <Button
                    variant="outline"
                    size="sm"
                    disabled={filters.page === totalPages}
                    onClick={() => updateFilters({ page: filters.page + 1 })}
                  >
                    下一页
                    <ChevronRightIcon data-icon="inline-end" />
                  </Button>
                </PaginationItem>
              </PaginationContent>
            </Pagination>
          ) : null}
        </CardContent>
      </Card>

      <TorrentAvailabilityDialog
        torrent={availabilityTarget}
        csrfToken={csrfToken}
        onOpenChange={(open) => {
          if (!open) setAvailabilityTarget(undefined)
        }}
        onChanged={setSuccessMessage}
      />
      <TorrentPurchasePriceDialog
        torrent={purchasePriceTarget}
        csrfToken={csrfToken}
        onOpenChange={(open) => {
          if (!open) setPurchasePriceTarget(undefined)
        }}
        onChanged={setSuccessMessage}
      />
    </StaffPageFrame>
  )
}

function stateCount(
  data: ManagedTorrentPage,
  state: ManagedTorrentStateFilter
) {
  if (state === "all") {
    const counts = data.state_counts
    return (
      counts.pending_review +
      counts.published +
      counts.rejected +
      counts.disabled +
      counts.deleted
    )
  }
  return data.state_counts[state]
}

function TorrentWorkbenchSkeleton() {
  return (
    <StaffPageFrame
      className="gap-4"
      aria-label="正在加载种子管理"
      aria-busy="true"
    >
      <Skeleton className="h-9 w-48" />
      <Skeleton className="h-[38rem] w-full rounded-xl" />
    </StaffPageFrame>
  )
}

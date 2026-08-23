import * as React from "react"
import { Link, useSearchParams } from "react-router"
import type { LucideIcon } from "lucide-react"
import { LayoutGridIcon, SearchIcon } from "lucide-react"

import { Badge } from "~/components/ui/badge"
import { Button } from "~/components/ui/button"
import { Card, CardContent } from "~/components/ui/card"
import { Input } from "~/components/ui/input"
import {
  InputGroup,
  InputGroupAddon,
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
  SelectLabel,
  SelectTrigger,
  SelectValue,
} from "~/components/ui/select"
import { useWebSession } from "~/features/auth/api/session.mutations"
import { useSiteInfo } from "~/features/site/api/site.queries"
import {
  type TorrentPromotion,
  type TorrentSort,
  useCategoryList,
  useTorrentList,
} from "~/features/torrent/api/torrent.queries"
import { TorrentCards } from "~/features/torrent/components/torrent-cards"
import {
  TorrentListEmpty,
  TorrentListError,
  TorrentListSkeleton,
} from "~/features/torrent/components/torrent-list-state"
import { TorrentTable } from "~/features/torrent/components/torrent-table"
import { TorrentViewControls } from "~/features/torrent/components/torrent-view-controls"
import { useTorrentBookmarkControls } from "~/features/torrent/hooks/use-torrent-bookmark-controls"
import { useAdultCoverVisibility } from "~/features/torrent/hooks/use-adult-cover-visibility"
import { torrentCategoryIcon } from "~/features/torrent/model/category-icon"
import { useTorrentView } from "~/features/torrent/hooks/use-torrent-view"
import { cn } from "~/lib/utils"
import { PageLayout } from "~/shared/components/page-layout"

// Production Core installations predating the current contract still enforce
// the original 20-row public catalog ceiling. Keep the browser request inside
// that deployed boundary so the legacy-style list remains usable during the
// rolling Core upgrade window.
const DEFAULT_PAGE_SIZE = 20
const PAGE_SIZE_OPTIONS = [10, 20] as const
const PAGE_SIZE_STORAGE_KEY = "peergo.torrent-page-size.v1"

type PromotionSelectValue = "all" | TorrentPromotion

const promotionOptions: { label: string; value: PromotionSelectValue }[] = [
  { label: "全部", value: "all" },
  { label: "免费", value: "free" },
  { label: "2X", value: "double_upload" },
  { label: "2X免费", value: "double_upload_free" },
  { label: "50%", value: "half_download" },
  { label: "2X50%", value: "double_upload_half_download" },
  { label: "30%", value: "thirty_percent_download" },
]

export function TorrentCatalogPage() {
  const [searchParams, setSearchParams] = useSearchParams()
  const query = searchParams.get("q")?.trim() ?? ""
  const categoryId = searchParams.get("category") ?? ""
  const promotion = parsePromotion(searchParams.get("promotion"))
  const sort = parseSort(searchParams.get("sort"))
  const currentPage = parsePage(searchParams.get("page"))
  const [pageSize, setPageSize] = React.useState(readCatalogPageSize)
  const [draftQuery, setDraftQuery] = React.useState(query)
  const session = useWebSession()
  const siteInfo = useSiteInfo()
  const authenticated = Boolean(session.data)
  const categories = useCategoryList(authenticated)
  const torrents = useTorrentList(
    {
      query,
      categoryId,
      promotion,
      sort: sort === "published_desc" ? undefined : sort,
      limit: pageSize,
      offset: (currentPage - 1) * pageSize,
    },
    authenticated
  )
  const torrentIds = React.useMemo(
    () => torrents.data?.items.map((torrent) => torrent.id) ?? [],
    [torrents.data?.items]
  )
  const bookmarkControls = useTorrentBookmarkControls(torrentIds)
  const [view, setView] = useTorrentView(siteInfo.data?.default_torrent_view)
  const [adultCoversVisible, setAdultCoversVisible] = useAdultCoverVisibility()
  const totalPages = Math.max(
    1,
    Math.ceil((torrents.data?.total ?? 0) / pageSize)
  )
  const totalCategoryCount =
    categories.data?.reduce(
      (total, category) => total + (category.torrent_count ?? 0),
      0
    ) ?? 0
  // Keep enabled empty categories visible. Rousi presents the complete catalog
  // vocabulary, and hiding a zero-count category makes both discovery and the
  // stable display order depend on the current contents of the tracker.
  const catalogCategories = categories.data ?? []

  React.useEffect(() => setDraftQuery(query), [query])

  function updateFilters(
    updates: Record<string, string | undefined>,
    resetPage = true
  ) {
    setSearchParams((current) => {
      const next = new URLSearchParams(current)
      for (const [key, value] of Object.entries(updates)) {
        if (value) next.set(key, value)
        else next.delete(key)
      }
      if (resetPage) next.delete("page")
      return next
    })
  }

  function handleSearch(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    updateFilters({ q: draftQuery.trim() || undefined })
  }

  function setCatalogPage(page: number) {
    setSearchParams((current) => {
      const next = new URLSearchParams(current)
      if (page <= 1) next.delete("page")
      else next.set("page", String(page))
      return next
    })
  }

  function changePageSize(nextPageSize: number) {
    if (!isCatalogPageSize(nextPageSize)) return
    setPageSize(nextPageSize)
    storeCatalogPageSize(nextPageSize)
    setCatalogPage(1)
  }

  if (!session.isPending && !authenticated) {
    return (
      <PageLayout className="items-center justify-center">
        <Card className="w-full max-w-md shadow-none">
          <CardContent className="flex flex-col items-center gap-4 py-10 text-center">
            <SearchIcon className="size-8 text-muted-foreground" />
            <div>
              <h1 className="text-xl font-semibold">登录后浏览种子</h1>
              <p className="mt-1 text-sm text-muted-foreground">
                私有目录只向已登录成员开放。
              </p>
            </div>
            <Button nativeButton={false} render={<Link to="/login" />}>
              前往登录
            </Button>
          </CardContent>
        </Card>
      </PageLayout>
    )
  }

  return (
    <PageLayout id="torrent-catalog" className="gap-3 sm:gap-4">
      <h1 className="sr-only">种子</h1>

      <Card className="gap-0 rounded-lg border py-0 shadow-sm ring-0">
        <CardContent className="-mx-2 flex [scrollbar-width:none] flex-nowrap gap-1.5 overflow-x-auto p-2 sm:mx-0 sm:flex-wrap sm:gap-2 sm:p-4 [&::-webkit-scrollbar]:hidden">
          <CategoryFilterButton
            label="全部"
            count={totalCategoryCount}
            icon={LayoutGridIcon}
            active={!categoryId}
            onClick={() => updateFilters({ category: undefined })}
          />
          {catalogCategories.map((category) => (
            <CategoryFilterButton
              key={category.id}
              label={category.name}
              count={category.torrent_count ?? 0}
              icon={torrentCategoryIcon(category.id, category.name)}
              active={categoryId === category.id}
              onClick={() => updateFilters({ category: category.id })}
            />
          ))}
          {categories.isError ? (
            <Button
              variant="ghost"
              size="sm"
              className="text-destructive"
              onClick={() => void categories.refetch()}
            >
              分类读取失败，点击重试
            </Button>
          ) : null}
        </CardContent>
      </Card>

      <Card className="gap-0 rounded-lg border py-0 shadow-sm ring-0">
        <CardContent className="p-3 sm:p-4">
          <form
            role="search"
            onSubmit={handleSearch}
            className="grid grid-cols-[minmax(0,1fr)_auto] gap-x-2 gap-y-3 sm:gap-y-4"
          >
            <InputGroup className="h-10 rounded-md">
              <InputGroupAddon className="pl-3">
                <SearchIcon />
              </InputGroupAddon>
              <InputGroupInput
                value={draftQuery}
                onChange={(event) => setDraftQuery(event.target.value)}
                placeholder="搜索种子标题..."
                aria-label="搜索种子标题"
                maxLength={100}
                className="h-10"
              />
            </InputGroup>
            <Button type="submit" className="h-10 w-15 px-0">
              搜索
            </Button>
            <div className="col-span-2 flex w-[calc((100%_-_0.75rem)/2)] flex-col gap-1 md:w-[calc((100%_-_1.5rem)/3)] lg:w-[calc((100%_-_2.25rem)/4)] xl:w-[calc((100%_-_3.75rem)/6)]">
              <span className="text-xs leading-none text-muted-foreground">
                促销
              </span>
              <Select
                items={promotionOptions}
                value={promotion ?? "all"}
                onValueChange={(value) => {
                  if (!value) return
                  updateFilters({
                    promotion: value === "all" ? undefined : value,
                  })
                }}
              >
                <SelectTrigger className="h-8! w-full" aria-label="促销">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent alignItemWithTrigger={false}>
                  <SelectGroup>
                    <SelectLabel>促销</SelectLabel>
                    {promotionOptions.map((option) => (
                      <SelectItem key={option.value} value={option.value}>
                        {option.label}
                      </SelectItem>
                    ))}
                  </SelectGroup>
                </SelectContent>
              </Select>
            </div>
          </form>
        </CardContent>
      </Card>

      <section
        aria-labelledby="torrent-results-title"
        className="flex flex-col gap-2.5 sm:gap-4"
      >
        <div className="flex flex-wrap items-center justify-between gap-3">
          <h2
            id="torrent-results-title"
            className="text-sm font-normal text-muted-foreground"
          >
            {torrents.data ? (
              <>
                共{" "}
                <strong className="font-medium text-foreground">
                  {torrents.data.total}
                </strong>{" "}
                个种子
              </>
            ) : (
              "正在读取种子"
            )}
          </h2>
          <TorrentViewControls
            value={view}
            onValueChange={setView}
            adultCoversVisible={adultCoversVisible}
            onAdultCoversVisibleChange={setAdultCoversVisible}
          />
        </div>

        {(session.isPending || torrents.isPending) && <TorrentListSkeleton />}
        {torrents.isError && (
          <TorrentListError
            error={torrents.error}
            retry={() => void torrents.refetch()}
          />
        )}
        {torrents.data?.items.length === 0 && (
          <TorrentListEmpty query={query} />
        )}
        {torrents.data && torrents.data.items.length > 0 ? (
          <>
            {view === "list" ? (
              <>
                <TorrentTable
                  torrents={torrents.data.items}
                  bookmarkControls={bookmarkControls}
                  adultCoversVisible={adultCoversVisible}
                  sort={sort}
                  onSortChange={(nextSort) =>
                    updateFilters({
                      sort:
                        nextSort === "published_desc" ? undefined : nextSort,
                    })
                  }
                />
                <TorrentCards
                  torrents={torrents.data.items}
                  poster={false}
                  bookmarkControls={bookmarkControls}
                  adultCoversVisible={adultCoversVisible}
                />
              </>
            ) : (
              <TorrentCards
                torrents={torrents.data.items}
                poster
                bookmarkControls={bookmarkControls}
                adultCoversVisible={adultCoversVisible}
              />
            )}
          </>
        ) : null}
      </section>

      {torrents.data && totalPages > 1 ? (
        <CatalogPagination
          currentPage={Math.min(currentPage, totalPages)}
          totalPages={totalPages}
          pageSize={pageSize}
          onPageChange={setCatalogPage}
          onPageSizeChange={changePageSize}
        />
      ) : null}
    </PageLayout>
  )
}

function CategoryFilterButton({
  label,
  count,
  icon: Icon,
  active,
  onClick,
}: {
  label: string
  count: number
  icon: LucideIcon
  active: boolean
  onClick: () => void
}) {
  return (
    <Button
      type="button"
      variant="outline"
      size="sm"
      aria-pressed={active}
      onClick={onClick}
      className="h-8 shrink-0 gap-1 rounded-md bg-background px-2.5 text-xs font-normal aria-pressed:border-transparent aria-pressed:bg-primary aria-pressed:text-primary-foreground sm:h-9 sm:gap-2 sm:px-3 sm:text-sm dark:aria-pressed:border-transparent dark:aria-pressed:bg-primary dark:aria-pressed:text-primary-foreground"
    >
      <Icon aria-hidden="true" />
      <span>{label}</span>
      <Badge
        variant="secondary"
        className={cn(
          "ml-0.5 h-5 px-1 text-[10px] sm:ml-1 sm:h-6 sm:px-2 sm:text-xs",
          active
            ? "bg-chart-3 text-white"
            : "bg-muted-foreground text-background"
        )}
      >
        {count}
      </Badge>
    </Button>
  )
}

function CatalogPagination({
  currentPage,
  totalPages,
  pageSize,
  onPageChange,
  onPageSizeChange,
}: {
  currentPage: number
  totalPages: number
  pageSize: number
  onPageChange: (page: number) => void
  onPageSizeChange: (pageSize: number) => void
}) {
  const [jumpPage, setJumpPage] = React.useState("")

  function jumpToRequestedPage() {
    const requestedPage = Number.parseInt(jumpPage, 10)
    if (
      !Number.isSafeInteger(requestedPage) ||
      requestedPage < 1 ||
      requestedPage > totalPages
    ) {
      return
    }
    onPageChange(requestedPage)
    setJumpPage("")
  }

  return (
    <Pagination className="mt-2" aria-label="种子目录分页">
      <PaginationContent className="flex-wrap gap-2">
        <PaginationItem>
          <Button
            type="button"
            variant="outline"
            disabled={currentPage === 1}
            onClick={() => onPageChange(currentPage - 1)}
          >
            上一页
          </Button>
        </PaginationItem>
        <PaginationItem>
          <span className="px-1 text-sm text-muted-foreground tabular-nums">
            第 {currentPage} 页 / 共 {totalPages} 页
          </span>
        </PaginationItem>
        <PaginationItem>
          <Button
            type="button"
            variant="outline"
            disabled={currentPage === totalPages}
            onClick={() => onPageChange(currentPage + 1)}
          >
            下一页
          </Button>
        </PaginationItem>
        <PaginationItem
          aria-hidden="true"
          className="mx-2 hidden text-sm text-muted-foreground sm:block"
        >
          |
        </PaginationItem>
        <PaginationItem className="flex items-center gap-1">
          <Input
            type="number"
            min={1}
            max={totalPages}
            value={jumpPage}
            onChange={(event) => setJumpPage(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === "Enter") jumpToRequestedPage()
            }}
            placeholder="页码"
            aria-label="跳转页码"
            className="h-9 w-20 text-center"
          />
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={jumpToRequestedPage}
          >
            跳转
          </Button>
        </PaginationItem>
        <PaginationItem
          aria-hidden="true"
          className="mx-2 hidden text-sm text-muted-foreground sm:block"
        >
          |
        </PaginationItem>
        <PaginationItem className="flex items-center gap-1">
          <span className="text-sm text-muted-foreground">每页</span>
          <select
            value={pageSize}
            onChange={(event) =>
              onPageSizeChange(Number.parseInt(event.target.value, 10))
            }
            aria-label="每页条数"
            className="h-9 rounded-md border border-input bg-background px-2 text-sm"
          >
            {PAGE_SIZE_OPTIONS.map((option) => (
              <option key={option} value={option}>
                {option}
              </option>
            ))}
          </select>
          <span className="text-sm text-muted-foreground">条</span>
        </PaginationItem>
      </PaginationContent>
    </Pagination>
  )
}

function parsePromotion(value: string | null): TorrentPromotion | undefined {
  return promotionOptions.some(
    (option) => option.value !== "all" && option.value === value
  )
    ? (value as TorrentPromotion)
    : undefined
}

function parseSort(value: string | null): TorrentSort {
  switch (value) {
    case "published_asc":
    case "size_desc":
    case "size_asc":
    case "completed_desc":
      return value
    default:
      return "published_desc"
  }
}

function parsePage(value: string | null) {
  const page = Number.parseInt(value ?? "1", 10)
  return Number.isSafeInteger(page) && page > 0 ? page : 1
}

function readCatalogPageSize() {
  try {
    const stored = Number.parseInt(
      globalThis.localStorage?.getItem(PAGE_SIZE_STORAGE_KEY) ?? "",
      10
    )
    return isCatalogPageSize(stored) ? stored : DEFAULT_PAGE_SIZE
  } catch {
    return DEFAULT_PAGE_SIZE
  }
}

function storeCatalogPageSize(pageSize: number) {
  try {
    globalThis.localStorage?.setItem(PAGE_SIZE_STORAGE_KEY, String(pageSize))
  } catch {
    // Storage is an enhancement; pagination remains usable without it.
  }
}

function isCatalogPageSize(
  value: number
): value is (typeof PAGE_SIZE_OPTIONS)[number] {
  return PAGE_SIZE_OPTIONS.some((option) => option === value)
}

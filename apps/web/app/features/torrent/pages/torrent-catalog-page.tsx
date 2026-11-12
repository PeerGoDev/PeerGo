import * as React from "react"
import { Link, useSearchParams } from "react-router"
import { SearchIcon } from "lucide-react"

import { Button } from "~/components/ui/button"
import { Card, CardContent } from "~/components/ui/card"
import {
  InputGroup,
  InputGroupAddon,
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
import { useWebSession } from "~/features/auth/api/session.mutations"
import { useTrafficOverview } from "~/features/traffic/api/traffic.queries"
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
import { useNsfwFilter } from "~/features/torrent/hooks/use-nsfw-filter"
import { useTorrentView } from "~/features/torrent/hooks/use-torrent-view"
import { nsfwCategoryIds } from "~/features/torrent/model/nsfw"
import { NumberedPagination } from "~/shared/components/numbered-pagination"
import { PageLayout } from "~/shared/components/page-layout"

const DEFAULT_PAGE_SIZE = 25
const PAGE_SIZE_OPTIONS = [25, 50, 100] as const
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
  const traffic = useTrafficOverview(session.data?.user.id)
  const authenticated = Boolean(session.data)
  const [hideNsfw, setHideNsfw] = useNsfwFilter()
  const categories = useCategoryList(authenticated)
  const torrents = useTorrentList(
    {
      query,
      categoryId,
      excludeNsfw: hideNsfw,
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
  const activityByTorrentId = React.useMemo(
    () =>
      new Map(
        (traffic.data?.torrent_activity ?? []).map((activity) => [
          activity.torrent.id,
          activity,
        ])
      ),
    [traffic.data?.torrent_activity]
  )
  const bookmarkControls = useTorrentBookmarkControls(torrentIds)
  const [view, setView] = useTorrentView(siteInfo.data?.default_torrent_view)
  const totalPages = Math.max(
    1,
    Math.ceil((torrents.data?.total ?? 0) / pageSize)
  )
  const nsfwIds = React.useMemo(
    () => nsfwCategoryIds(categories.data),
    [categories.data]
  )
  const hiddenCoverCategoryIds = hideNsfw ? nsfwIds : undefined
  // Keep enabled empty categories visible. Rousi presents the complete catalog
  // vocabulary, and hiding a zero-count category makes both discovery and the
  // stable display order depend on the current contents of the tracker.
  const catalogCategories = (categories.data ?? []).filter(
    (category) => !hideNsfw || !category.nsfw
  )
  const totalCategoryCount = catalogCategories.reduce(
    (total, category) => total + (category.torrent_count ?? 0),
    0
  )

  React.useEffect(() => setDraftQuery(query), [query])

  React.useEffect(() => {
    if (torrents.isSuccess) storeCatalogPageSize(pageSize)
  }, [pageSize, torrents.isSuccess])

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
    setCatalogPage(1)
  }

  if (!session.isPending && !authenticated) {
    return (
      <PageLayout className="items-center justify-center">
        <Card className="w-full max-w-md">
          <CardContent className="flex flex-col items-center gap-4 py-6 text-center">
            <span className="flex size-11 items-center justify-center rounded-xl border bg-muted text-muted-foreground">
              <SearchIcon className="size-5" />
            </span>
            <div>
              <h1 className="text-lg font-semibold tracking-title">
                登录后浏览种子
              </h1>
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

  const pagination =
    torrents.isError || torrents.data ? (
      <NumberedPagination
        ariaLabel="种子目录分页"
        currentPage={Math.min(currentPage, totalPages)}
        totalPages={totalPages}
        onPageChange={setCatalogPage}
        pageSize={pageSize}
        pageSizeOptions={PAGE_SIZE_OPTIONS}
        onPageSizeChange={changePageSize}
        showJump
      />
    ) : null

  return (
    <PageLayout id="torrent-catalog" className="gap-3 sm:gap-4">
      <h1 className="sr-only">种子</h1>

      <div
        role="group"
        aria-label="分类筛选"
        className="-mx-4 flex [scrollbar-width:none] flex-nowrap gap-2 overflow-x-auto px-4 sm:mx-0 sm:flex-wrap sm:px-0 [&::-webkit-scrollbar]:hidden"
      >
        <CategoryFilterChip
          label="全部"
          count={totalCategoryCount}
          active={!categoryId}
          onClick={() => updateFilters({ category: undefined })}
        />
        {catalogCategories.map((category) => (
          <CategoryFilterChip
            key={category.id}
            label={category.name}
            count={category.torrent_count ?? 0}
            active={categoryId === category.id}
            onClick={() => updateFilters({ category: category.id })}
          />
        ))}
        {categories.isError ? (
          <Button
            variant="ghost"
            size="xs"
            className="shrink-0 text-destructive"
            onClick={() => void categories.refetch()}
          >
            分类读取失败，点击重试
          </Button>
        ) : null}
      </div>

      <form
        role="search"
        onSubmit={handleSearch}
        className="flex flex-wrap items-end gap-2.5"
      >
        <InputGroup className="min-w-0 flex-1">
          <InputGroupAddon className="pl-3">
            <SearchIcon />
          </InputGroupAddon>
          <InputGroupInput
            value={draftQuery}
            onChange={(event) => setDraftQuery(event.target.value)}
            placeholder="搜索种子标题..."
            aria-label="搜索种子标题"
            maxLength={100}
          />
        </InputGroup>
        <Button type="submit" className="w-16 shrink-0">
          搜索
        </Button>
        <div className="flex flex-col gap-1 max-sm:w-full sm:w-36">
          <span className="text-2xs leading-none text-faint">促销</span>
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
            <SelectTrigger size="sm" className="w-full" aria-label="促销">
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

      <section
        aria-labelledby="torrent-results-title"
        className="flex flex-col gap-3"
      >
        <div className="flex flex-wrap items-center justify-between gap-x-3 gap-y-2">
          <h2
            id="torrent-results-title"
            className="flex min-w-0 items-baseline gap-2.5"
          >
            <span className="text-base font-semibold tracking-title">
              {query ? "搜索结果" : "最新种子"}
            </span>
            <span className="text-xs font-normal text-faint">
              {torrents.data ? (
                <>
                  共{" "}
                  <span className="font-mono tabular-nums">
                    {torrents.data.total.toLocaleString("zh-CN")}
                  </span>{" "}
                  个种子
                </>
              ) : (
                "正在读取种子"
              )}
            </span>
          </h2>
          <TorrentViewControls
            value={view}
            onValueChange={setView}
            hideNsfw={hideNsfw}
            onHideNsfwChange={setHideNsfw}
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
          <TorrentListEmpty
            query={query}
            nsfwHidden={hideNsfw && nsfwIds.has(categoryId)}
          />
        )}
        {torrents.data && torrents.data.items.length > 0 ? (
          view === "list" ? (
            <>
              <TorrentTable
                torrents={torrents.data.items}
                bookmarkControls={bookmarkControls}
                hiddenCoverCategoryIds={hiddenCoverCategoryIds}
                activityByTorrentId={activityByTorrentId}
                sort={sort}
                onSortChange={(nextSort) =>
                  updateFilters({
                    sort: nextSort === "published_desc" ? undefined : nextSort,
                  })
                }
                footer={pagination}
              />
              <TorrentCards
                torrents={torrents.data.items}
                view="list"
                bookmarkControls={bookmarkControls}
                hiddenCoverCategoryIds={hiddenCoverCategoryIds}
                activityByTorrentId={activityByTorrentId}
              />
              <div className="md:hidden">{pagination}</div>
            </>
          ) : (
            <>
              <TorrentCards
                torrents={torrents.data.items}
                view={view}
                bookmarkControls={bookmarkControls}
                hiddenCoverCategoryIds={hiddenCoverCategoryIds}
                activityByTorrentId={activityByTorrentId}
              />
              {pagination}
            </>
          )
        ) : torrents.isError || (torrents.data && torrents.data.total > 0) ? (
          pagination
        ) : null}
      </section>
    </PageLayout>
  )
}

function CategoryFilterChip({
  label,
  count,
  active,
  onClick,
}: {
  label: string
  count: number
  active: boolean
  onClick: () => void
}) {
  return (
    <button
      type="button"
      aria-pressed={active}
      onClick={onClick}
      className="group/chip inline-flex h-7 shrink-0 items-center gap-1.5 rounded-md border border-input bg-card px-2.5 text-xs transition-colors outline-none hover:border-faint/60 focus-visible:ring-3 focus-visible:ring-ring/30 aria-pressed:border-primary aria-pressed:bg-primary sm:h-6"
    >
      <span className="font-medium text-foreground group-aria-pressed/chip:text-primary-foreground">
        {label}
      </span>
      <span className="font-mono text-2xs text-faint tabular-nums group-aria-pressed/chip:text-primary-foreground/80">
        {count.toLocaleString("zh-CN")}
      </span>
    </button>
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

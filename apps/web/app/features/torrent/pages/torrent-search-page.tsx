import * as React from "react"
import { Link, useSearchParams } from "react-router"
import {
  ChevronDownIcon,
  ChevronUpIcon,
  HistoryIcon,
  SearchIcon,
  SlidersHorizontalIcon,
  TrendingUpIcon,
  XIcon,
} from "lucide-react"

import { Button, buttonVariants } from "~/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "~/components/ui/card"
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "~/components/ui/collapsible"
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
  SelectTrigger,
  SelectValue,
} from "~/components/ui/select"
import { useWebSession } from "~/features/auth/api/session.mutations"
import {
  type TorrentSearchScope,
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
import { useAdultCoverVisibility } from "~/features/torrent/hooks/use-adult-cover-visibility"
import { useTorrentBookmarkControls } from "~/features/torrent/hooks/use-torrent-bookmark-controls"
import { OffsetPagination } from "~/shared/components/offset-pagination"
import { PageHeader, PageLayout } from "~/shared/components/page-layout"
import { cn } from "~/lib/utils"

const PAGE_SIZE = 25
const SEARCH_HISTORY_KEY = "peergo.torrent-search.history.v1"
const LEGACY_SEARCH_HISTORY_KEY = "pt_search_history"
const MAX_HISTORY_ITEMS = 10

const searchScopeOptions: { label: string; value: TorrentSearchScope }[] = [
  { label: "标题和副标题", value: "title_subtitle" },
  { label: "仅标题", value: "title" },
  { label: "仅副标题", value: "subtitle" },
]

const sortOptions: { label: string; value: TorrentSort }[] = [
  { label: "默认排序", value: "published_desc" },
  { label: "最新发布", value: "published_desc" },
  { label: "最早发布", value: "published_asc" },
  { label: "体积最大", value: "size_desc" },
  { label: "体积最小", value: "size_asc" },
  { label: "下载最多", value: "completed_desc" },
]

export function TorrentSearchPage() {
  const [searchParams, setSearchParams] = useSearchParams()
  const query = searchParams.get("q")?.trim() ?? ""
  const categoryId = searchParams.get("category") ?? ""
  const searchScope = parseSearchScope(searchParams.get("scope"))
  const sort = parseSort(searchParams.get("sort"))
  const offset = parseOffset(searchParams.get("offset"))
  const [draftQuery, setDraftQuery] = React.useState(query)
  const [advancedOpen, setAdvancedOpen] = React.useState(
    Boolean(categoryId || searchParams.has("scope") || searchParams.has("sort"))
  )
  const [history, setHistory] = React.useState<string[]>([])
  const session = useWebSession()
  const authenticated = Boolean(session.data)
  const categories = useCategoryList(authenticated)
  const torrents = useTorrentList(
    {
      query,
      searchScope,
      categoryId,
      sort,
      limit: PAGE_SIZE,
      offset,
    },
    authenticated && Boolean(query)
  )
  const torrentIds = React.useMemo(
    () => torrents.data?.items.map((torrent) => torrent.id) ?? [],
    [torrents.data?.items]
  )
  const bookmarkControls = useTorrentBookmarkControls(torrentIds)
  const [adultCoversVisible] = useAdultCoverVisibility()

  React.useEffect(() => setDraftQuery(query), [query])
  React.useEffect(() => setHistory(readSearchHistory()), [])
  React.useEffect(() => {
    if (!query || !torrents.data) return
    setHistory(storeSearchTerm(query))
  }, [query, torrents.data])

  function updateSearch(updates: Record<string, string | undefined>) {
    setSearchParams((current) => {
      const next = new URLSearchParams(current)
      for (const [key, value] of Object.entries(updates)) {
        if (value) next.set(key, value)
        else next.delete(key)
      }
      next.delete("offset")
      return next
    })
  }

  function performSearch(keyword: string) {
    const normalized = keyword.trim()
    if (!normalized) return
    updateSearch({ q: normalized })
  }

  function handleSubmit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    performSearch(draftQuery)
  }

  if (!session.isPending && !authenticated) {
    return (
      <PageLayout className="items-center justify-center">
        <Card className="w-full max-w-md shadow-sm">
          <CardContent className="flex flex-col items-center gap-4 py-10 text-center">
            <SearchIcon className="size-8 text-muted-foreground" />
            <div>
              <h1 className="text-xl font-semibold">登录后搜索种子</h1>
              <p className="mt-1 text-sm text-muted-foreground">
                私有目录只向已登录成员开放。
              </p>
            </div>
            <Link to="/login" className={buttonVariants()}>
              前往登录
            </Link>
          </CardContent>
        </Card>
      </PageLayout>
    )
  }

  return (
    <PageLayout id="torrent-search" className="gap-6">
      <PageHeader title="搜索种子" />

      <Card className="gap-0 rounded-lg border py-0 shadow-sm ring-0">
        <CardContent className="p-6">
          <Collapsible open={advancedOpen} onOpenChange={setAdvancedOpen}>
            <form onSubmit={handleSubmit} className="flex flex-col gap-4">
              <div className="flex gap-3">
                <InputGroup className="h-12 flex-1 rounded-md">
                  <InputGroupAddon className="pl-3">
                    <SearchIcon className="size-5" />
                  </InputGroupAddon>
                  <InputGroupInput
                    value={draftQuery}
                    onChange={(event) => setDraftQuery(event.target.value)}
                    placeholder="搜索种子标题、副标题..."
                    aria-label="搜索种子标题、副标题"
                    maxLength={100}
                    className="text-base md:text-base"
                  />
                  {draftQuery ? (
                    <InputGroupAddon align="inline-end" className="pr-2">
                      <InputGroupButton
                        size="icon-xs"
                        aria-label="清空搜索"
                        onClick={() => setDraftQuery("")}
                      >
                        <XIcon />
                      </InputGroupButton>
                    </InputGroupAddon>
                  ) : null}
                </InputGroup>
                <Button
                  type="submit"
                  size="lg"
                  className="w-23 rounded-md px-8"
                >
                  搜索
                </Button>
              </div>

              <div className="flex items-center justify-between">
                <CollapsibleTrigger
                  render={
                    <Button
                      type="button"
                      variant={advancedOpen ? "outline" : "ghost"}
                      size="sm"
                      className="w-32 text-muted-foreground"
                    />
                  }
                >
                  <SlidersHorizontalIcon data-icon="inline-start" />
                  高级筛选
                  {advancedOpen ? (
                    <ChevronUpIcon data-icon="inline-end" />
                  ) : (
                    <ChevronDownIcon data-icon="inline-end" />
                  )}
                </CollapsibleTrigger>
                {categoryId ||
                searchScope !== "title_subtitle" ||
                sort !== "published_desc" ? (
                  <Button
                    type="button"
                    variant="ghost"
                    size="sm"
                    className="text-muted-foreground"
                    onClick={() =>
                      updateSearch({
                        category: undefined,
                        scope: undefined,
                        sort: undefined,
                      })
                    }
                  >
                    <XIcon data-icon="inline-start" />
                    清除筛选
                  </Button>
                ) : null}
              </div>

              <CollapsibleContent className="border-t pt-4">
                <div className="grid gap-4 md:grid-cols-3">
                  <SearchSelect
                    id="torrent-search-scope"
                    label="搜索范围"
                    value={searchScope}
                    items={searchScopeOptions}
                    onValueChange={(value) =>
                      updateSearch({
                        scope: value === "title_subtitle" ? undefined : value,
                      })
                    }
                  />
                  <SearchSelect
                    id="torrent-search-category"
                    label="分类"
                    value={categoryId || "all"}
                    items={[
                      { label: "全部分类", value: "all" },
                      ...(categories.data?.map((category) => ({
                        label: category.name,
                        value: category.id,
                      })) ?? []),
                    ]}
                    onValueChange={(value) =>
                      updateSearch({
                        category: value === "all" ? undefined : value,
                      })
                    }
                  />
                  <SearchSelect
                    id="torrent-search-sort"
                    label="排序"
                    value={sort}
                    items={sortOptions}
                    onValueChange={(value) =>
                      updateSearch({
                        sort: value === "published_desc" ? undefined : value,
                      })
                    }
                  />
                </div>
              </CollapsibleContent>
            </form>
          </Collapsible>
        </CardContent>
      </Card>

      {!query ? (
        <div className="grid gap-6 md:grid-cols-2">
          {history.length ? (
            <SearchTermsCard
              title="搜索历史"
              icon={HistoryIcon}
              terms={history}
              tone="history"
              onSelect={(term) => {
                setDraftQuery(term)
                performSearch(term)
              }}
              onRemove={(term) => {
                const nextHistory = history.filter((item) => item !== term)
                globalThis.localStorage?.setItem(
                  SEARCH_HISTORY_KEY,
                  JSON.stringify(nextHistory)
                )
                setHistory(nextHistory)
              }}
              action={
                <Button
                  variant="ghost"
                  size="sm"
                  className="h-8 text-xs text-muted-foreground"
                  onClick={() => {
                    globalThis.localStorage?.setItem(SEARCH_HISTORY_KEY, "[]")
                    setHistory([])
                  }}
                >
                  清空
                </Button>
              }
            />
          ) : null}
          <SearchTermsCard
            title="热门搜索"
            icon={TrendingUpIcon}
            terms={[]}
            onSelect={(term) => {
              setDraftQuery(term)
              performSearch(term)
            }}
          />
        </div>
      ) : (
        <section
          aria-labelledby="torrent-search-results"
          className="flex flex-col gap-6"
        >
          <p id="torrent-search-results" className="text-muted-foreground">
            搜索{" "}
            <strong className="font-medium text-foreground">“{query}”</strong>{" "}
            {torrents.data ? (
              <>
                共找到{" "}
                <strong className="font-medium text-foreground">
                  {torrents.data.total.toLocaleString("zh-CN")}
                </strong>{" "}
                个结果
              </>
            ) : (
              "正在读取结果"
            )}
          </p>

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
          {torrents.data?.items.length ? (
            <>
              <TorrentTable
                torrents={torrents.data.items}
                bookmarkControls={bookmarkControls}
                adultCoversVisible={adultCoversVisible}
              />
              <TorrentCards
                torrents={torrents.data.items}
                poster={false}
                bookmarkControls={bookmarkControls}
                adultCoversVisible={adultCoversVisible}
              />
              <OffsetPagination
                total={torrents.data.total}
                limit={PAGE_SIZE}
                offset={offset}
                ariaLabel="搜索结果分页"
                onOffsetChange={(nextOffset) =>
                  setSearchParams((current) => {
                    const next = new URLSearchParams(current)
                    if (nextOffset) next.set("offset", String(nextOffset))
                    else next.delete("offset")
                    return next
                  })
                }
              />
            </>
          ) : null}
        </section>
      )}
    </PageLayout>
  )
}

function SearchSelect<T extends string>({
  id,
  label,
  value,
  items,
  onValueChange,
}: {
  id: string
  label: string
  value: T
  items: { label: string; value: T }[]
  onValueChange: (value: T) => void
}) {
  return (
    <Field className="gap-2">
      <FieldLabel htmlFor={id} className="leading-none">
        {label}
      </FieldLabel>
      <Select
        items={items}
        value={value}
        onValueChange={(nextValue) => {
          if (nextValue) onValueChange(nextValue)
        }}
      >
        <SelectTrigger id={id} className="h-9! w-full rounded-md">
          <SelectValue />
        </SelectTrigger>
        <SelectContent alignItemWithTrigger={false}>
          <SelectGroup>
            {items.map((item) => (
              <SelectItem key={item.value} value={item.value}>
                {item.label}
              </SelectItem>
            ))}
          </SelectGroup>
        </SelectContent>
      </Select>
    </Field>
  )
}

function SearchTermsCard({
  title,
  icon: Icon,
  terms,
  onSelect,
  onRemove,
  action,
  tone = "popular",
}: {
  title: string
  icon: React.ComponentType<{ className?: string }>
  terms: string[]
  onSelect: (term: string) => void
  onRemove?: (term: string) => void
  action?: React.ReactNode
  tone?: "history" | "popular"
}) {
  return (
    <Card className="gap-0 rounded-lg border py-0 shadow-sm ring-0 md:min-h-[246px]">
      <CardHeader className="flex flex-row items-center justify-between p-6 pb-3">
        <CardTitle className="flex items-center gap-2">
          <Icon className="size-4" />
          <h2>{title}</h2>
        </CardTitle>
        {action}
      </CardHeader>
      <CardContent className="flex flex-wrap content-start gap-2 px-6 pb-6.5">
        {terms.length
          ? terms.map((term) => (
              <span
                key={term}
                className={cn(
                  "group inline-flex h-[34px] items-center rounded-md text-sm transition-colors hover:bg-primary hover:text-primary-foreground",
                  tone === "history"
                    ? "bg-secondary text-secondary-foreground"
                    : "border bg-background"
                )}
              >
                <button
                  type="button"
                  className="h-full rounded-l-md px-3 outline-none focus-visible:ring-2 focus-visible:ring-ring"
                  onClick={() => onSelect(term)}
                >
                  {term}
                </button>
                {onRemove ? (
                  <button
                    type="button"
                    aria-label={`删除搜索历史 ${term}`}
                    className="mr-2 rounded-sm text-current opacity-0 transition-opacity group-hover:opacity-70 focus:opacity-70 focus:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                    onClick={() => onRemove(term)}
                  >
                    <XIcon className="size-3" />
                  </button>
                ) : null}
              </span>
            ))
          : null}
      </CardContent>
    </Card>
  )
}

function parseSearchScope(value: string | null): TorrentSearchScope {
  return value === "title" || value === "subtitle" ? value : "title_subtitle"
}

function parseSort(value: string | null): TorrentSort {
  return value === "published_asc" ||
    value === "size_desc" ||
    value === "size_asc" ||
    value === "completed_desc"
    ? value
    : "published_desc"
}

function parseOffset(value: string | null) {
  const parsed = Number.parseInt(value ?? "0", 10)
  return Number.isSafeInteger(parsed) && parsed >= 0 ? parsed : 0
}

function readSearchHistory() {
  try {
    const serialized =
      globalThis.localStorage?.getItem(SEARCH_HISTORY_KEY) ??
      globalThis.localStorage?.getItem(LEGACY_SEARCH_HISTORY_KEY)
    if (!serialized) return []
    const value: unknown = JSON.parse(serialized)
    return Array.isArray(value)
      ? value
          .filter((item): item is string => typeof item === "string")
          .slice(0, MAX_HISTORY_ITEMS)
      : []
  } catch {
    return []
  }
}

function storeSearchTerm(term: string) {
  const history = [
    term,
    ...readSearchHistory().filter((item) => item !== term),
  ].slice(0, MAX_HISTORY_ITEMS)
  try {
    globalThis.localStorage?.setItem(
      SEARCH_HISTORY_KEY,
      JSON.stringify(history)
    )
  } catch {
    // Storage can be unavailable in privacy modes; search remains functional.
  }
  return history
}

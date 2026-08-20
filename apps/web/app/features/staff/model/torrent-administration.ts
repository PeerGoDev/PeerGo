export type ManagedTorrentStateFilter =
  | "all"
  | "pending_review"
  | "published"
  | "rejected"
  | "disabled"
  | "deleted"

export type ManagedTorrentFilters = {
  query: string
  state: ManagedTorrentStateFilter
  categoryId: string
  page: number
  pageSize: number
}

export function parseManagedTorrentFilters(
  params: URLSearchParams
): ManagedTorrentFilters {
  const rawState = params.get("state") ?? "all"
  const state = isManagedTorrentStateFilter(rawState) ? rawState : "all"
  const page = boundedPositiveInteger(params.get("page"), 1)
  const pageSize = boundedPositiveInteger(params.get("page_size"), 20, 50)
  return {
    query: (params.get("query") ?? "").trim().slice(0, 100),
    state,
    categoryId: (params.get("category_id") ?? "").trim().slice(0, 64),
    page,
    pageSize,
  }
}

export function managedTorrentSearchParams(filters: ManagedTorrentFilters) {
  const params = new URLSearchParams()
  if (filters.query) params.set("query", filters.query)
  if (filters.state !== "all") params.set("state", filters.state)
  if (filters.categoryId) params.set("category_id", filters.categoryId)
  if (filters.page > 1) params.set("page", String(filters.page))
  if (filters.pageSize !== 20) params.set("page_size", String(filters.pageSize))
  return params
}

export function managedTorrentStateLabel(state: ManagedTorrentStateFilter) {
  switch (state) {
    case "all":
      return "全部"
    case "pending_review":
      return "待审核"
    case "published":
      return "已发布"
    case "rejected":
      return "已驳回"
    case "disabled":
      return "已下架"
    case "deleted":
      return "已删除"
  }
}

function isManagedTorrentStateFilter(
  value: string
): value is ManagedTorrentStateFilter {
  return [
    "all",
    "pending_review",
    "published",
    "rejected",
    "disabled",
    "deleted",
  ].includes(value)
}

function boundedPositiveInteger(
  value: string | null,
  fallback: number,
  maximum = 1_000_000
) {
  const parsed = Number(value)
  return Number.isSafeInteger(parsed) && parsed >= 1 && parsed <= maximum
    ? parsed
    : fallback
}

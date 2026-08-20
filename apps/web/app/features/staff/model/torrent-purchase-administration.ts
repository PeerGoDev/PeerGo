export type ManagedPurchaseStatusFilter = "all" | "active" | "refunded"
export type ManagedPurchaseSourceFilter =
  | "all"
  | "live_purchase"
  | "legacy_import"

export type ManagedPurchaseFilters = {
  query: string
  status: ManagedPurchaseStatusFilter
  source: ManagedPurchaseSourceFilter
  page: number
  pageSize: number
}

export function parseManagedPurchaseFilters(
  params: URLSearchParams
): ManagedPurchaseFilters {
  const rawStatus = params.get("status") ?? "all"
  const rawSource = params.get("source") ?? "all"
  return {
    query: (params.get("query") ?? "").trim().slice(0, 100),
    status: isStatus(rawStatus) ? rawStatus : "all",
    source: isSource(rawSource) ? rawSource : "all",
    page: boundedPositiveInteger(params.get("page"), 1),
    pageSize: boundedPositiveInteger(params.get("page_size"), 20, 50),
  }
}

export function managedPurchaseSearchParams(filters: ManagedPurchaseFilters) {
  const params = new URLSearchParams()
  if (filters.query) params.set("query", filters.query)
  if (filters.status !== "all") params.set("status", filters.status)
  if (filters.source !== "all") params.set("source", filters.source)
  if (filters.page > 1) params.set("page", String(filters.page))
  if (filters.pageSize !== 20) params.set("page_size", String(filters.pageSize))
  return params
}

function isStatus(value: string): value is ManagedPurchaseStatusFilter {
  return ["all", "active", "refunded"].includes(value)
}

function isSource(value: string): value is ManagedPurchaseSourceFilter {
  return ["all", "live_purchase", "legacy_import"].includes(value)
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

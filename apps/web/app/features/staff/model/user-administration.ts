import type { components } from "~/generated/api"

export type ManagedUserStatus = components["schemas"]["ManagedUserStatus"]
export type ManagedUserDirectoryFilter =
  components["schemas"]["ManagedUserDirectoryFilter"]
export type ManagedUserStatusFilter = ManagedUserDirectoryFilter | "all"

export interface ManagedUserFilters {
  query: string
  status: ManagedUserStatusFilter
  page: number
  pageSize: number
}

export const managedUserPageSize = 20

const managedUserStatuses = new Set<ManagedUserDirectoryFilter>([
  "active",
  "banned",
  "pending",
  "vip",
  "download_restricted",
  "unverified",
])

export function parseManagedUserFilters(
  searchParams: URLSearchParams
): ManagedUserFilters {
  const query = (searchParams.get("query") ?? "").trim()
  const rawStatus = searchParams.get("filter") ?? "all"
  const rawPage = Number.parseInt(searchParams.get("page") ?? "1", 10)
  return {
    query,
    status: managedUserStatuses.has(rawStatus as ManagedUserDirectoryFilter)
      ? (rawStatus as ManagedUserDirectoryFilter)
      : "all",
    page:
      Number.isSafeInteger(rawPage) && rawPage >= 1 && rawPage <= 100_000
        ? rawPage
        : 1,
    pageSize: managedUserPageSize,
  }
}

export function managedUserSearchParams(
  filters: ManagedUserFilters
): URLSearchParams {
  const next = new URLSearchParams()
  if (filters.query) {
    next.set("query", filters.query)
  }
  if (filters.status !== "all") {
    next.set("filter", filters.status)
  }
  if (filters.page > 1) {
    next.set("page", filters.page.toString())
  }
  return next
}

export function managedUserPageNumbers(current: number, total: number) {
  if (total <= 0) {
    return []
  }
  const start = Math.max(1, Math.min(current - 2, total - 4))
  const end = Math.min(total, start + 4)
  return Array.from({ length: end - start + 1 }, (_, index) => start + index)
}

export function managedUserInitial(displayName: string) {
  return Array.from(displayName.trim())[0] ?? "用"
}

export function managedUserStatusLabel(status: ManagedUserStatus) {
  switch (status) {
    case "active":
      return "正常"
    case "pending":
      return "待激活"
    case "disabled":
      return "已停用"
  }
}

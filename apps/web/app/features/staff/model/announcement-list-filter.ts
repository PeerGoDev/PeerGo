import type { ManagedAnnouncementSummary } from "~/features/staff/api/announcement-administration.queries"

export type AnnouncementStatusFilter =
  | "all"
  | ManagedAnnouncementSummary["status"]

export type AnnouncementRevisionFilter = "all" | "changed" | "current"

export type AnnouncementListFilters = {
  query: string
  status: AnnouncementStatusFilter
  revision: AnnouncementRevisionFilter
}

/**
 * Filters only the already-loaded administration page. Pagination remains an
 * API concern, while these controls mirror the quick, client-side narrowing
 * used by the legacy operator screen without inventing unsupported fields.
 */
export function filterManagedAnnouncements(
  announcements: ManagedAnnouncementSummary[],
  filters: AnnouncementListFilters
) {
  const query = filters.query.trim().toLocaleLowerCase("zh-CN")

  return announcements.filter((announcement) => {
    if (
      query &&
      ![announcement.title, announcement.id, announcement.summary].some(
        (value) => value.toLocaleLowerCase("zh-CN").includes(query)
      )
    ) {
      return false
    }

    if (filters.status !== "all" && announcement.status !== filters.status) {
      return false
    }

    if (
      filters.revision === "changed" &&
      !announcement.has_unpublished_changes
    ) {
      return false
    }
    if (
      filters.revision === "current" &&
      announcement.has_unpublished_changes
    ) {
      return false
    }

    return true
  })
}

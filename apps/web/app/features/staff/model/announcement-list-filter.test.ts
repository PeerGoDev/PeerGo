import { describe, expect, it } from "vitest"

import type { ManagedAnnouncementSummary } from "~/features/staff/api/announcement-administration.queries"
import { filterManagedAnnouncements } from "~/features/staff/model/announcement-list-filter"

const announcements: ManagedAnnouncementSummary[] = [
  {
    id: "welcome-to-peergo",
    title: "欢迎使用 PeerGo",
    summary: "站点首份公开说明",
    status: "published",
    version: 2,
    revision_number: 1,
    has_unpublished_changes: false,
    published_at: "2026-08-01T00:00:00Z",
    created_at: "2026-08-01T00:00:00Z",
    updated_at: "2026-08-01T00:00:00Z",
  },
  {
    id: "tracker-maintenance",
    title: "Tracker 维护预告",
    summary: "维护窗口与 announce 影响范围",
    status: "scheduled",
    version: 4,
    revision_number: 3,
    has_unpublished_changes: true,
    scheduled_for: "2026-08-15T00:00:00Z",
    created_at: "2026-08-10T00:00:00Z",
    updated_at: "2026-08-12T00:00:00Z",
  },
]

describe("filterManagedAnnouncements", () => {
  it("searches the title, public key, and summary case-insensitively", () => {
    expect(
      filterManagedAnnouncements(announcements, {
        query: "ANNOUNCE",
        status: "all",
        revision: "all",
      }).map((announcement) => announcement.id)
    ).toEqual(["tracker-maintenance"])
  })

  it("combines publication and revision filters", () => {
    expect(
      filterManagedAnnouncements(announcements, {
        query: "",
        status: "scheduled",
        revision: "changed",
      }).map((announcement) => announcement.id)
    ).toEqual(["tracker-maintenance"])

    expect(
      filterManagedAnnouncements(announcements, {
        query: "",
        status: "published",
        revision: "changed",
      })
    ).toEqual([])
  })
})

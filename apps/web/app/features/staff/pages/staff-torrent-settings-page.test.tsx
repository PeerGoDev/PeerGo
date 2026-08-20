import { QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import { MemoryRouter } from "react-router"
import { describe, expect, it } from "vitest"

import { torrentSettingsQueryOptions } from "~/features/staff/api/operations.queries"
import { StaffTorrentSettingsPage } from "~/features/staff/pages/staff-torrent-settings-page"
import { createStaffPageQueryClient } from "~/features/staff/test-utils/create-staff-page-query-client"

describe("StaffTorrentSettingsPage", () => {
  it("shows enforced upload rules without treating migration exceptions as defaults", async () => {
    const queryClient = createStaffPageQueryClient(["torrent.manage.read"])
    queryClient.setQueryData(torrentSettingsQueryOptions().queryKey, {
      generated_at: "2026-08-16T08:00:00Z",
      active_upload_policy: {
        id: "00000000-0000-0000-0000-000000000013",
        sequence: 1,
        effective_at: "1970-01-01T00:00:00Z",
        created_at: "1970-01-01T00:00:00Z",
        reason: "PeerGo 首版新上传与截图安全基线。",
        settings: {
          metainfo_max_bytes: 4194304,
          max_files: 100000,
          screenshot_max_count: 6,
          screenshot_max_bytes: 2097152,
          screenshot_max_pixels: 25000000,
          screenshot_formats: ["jpeg", "png", "webp"],
        },
      },
      scheduled_upload_policies: [],
      upload: {
        metainfo_max_bytes: "4194304",
        max_files: 100000,
        required_private: true,
        supported_protocol: "bittorrent-v1",
        duplicate_swarm_rejected: true,
        initial_state: "pending_review",
      },
      screenshots: {
        max_count: 6,
        max_bytes_per_file: "2097152",
        formats: ["jpeg", "png", "webp"],
        first_is_cover: true,
      },
      object: {
        original_stored_immutable: true,
        announce_rewritten_on_download: true,
        legacy_import_profile: "legacy_import",
        new_upload_profile: "strict_upload",
      },
      purchase: {
        enabled: true,
        currency_name: "魔力值",
        whole_units_only: true,
        tax_basis_points: 1000,
        policy_revision: "torrent-purchase-v1",
        policy_effective_from: "2026-08-16T00:00:00Z",
        priced_torrents: "378",
        legacy_entitlements: "32983",
        live_entitlements: "0",
        permanent_access: true,
        atomic_settlement: true,
        refund_connected: true,
      },
    })

    render(
      <MemoryRouter initialEntries={["/staff/settings/torrents"]}>
        <QueryClientProvider client={queryClient}>
          <StaffTorrentSettingsPage />
        </QueryClientProvider>
      </MemoryRouter>
    )

    expect(
      await screen.findByRole("heading", { name: "种子规则" })
    ).toBeVisible()
    expect(screen.getAllByText("4 MB").length).toBeGreaterThan(0)
    expect(screen.getAllByText("100,000").length).toBeGreaterThan(0)
    expect(screen.getByText("老站兼容只用于迁移")).toBeVisible()
    expect(screen.getByText("写入个人 Tracker 地址")).toBeVisible()
    expect(screen.getByRole("heading", { name: "种子购买" })).toBeVisible()
    expect(screen.getByText("32,983")).toBeVisible()
    expect(screen.getByText(/同一事务提交/)).toBeVisible()
  })
})

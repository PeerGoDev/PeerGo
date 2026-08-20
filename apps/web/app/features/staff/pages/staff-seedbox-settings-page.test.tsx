import { QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import { MemoryRouter } from "react-router"
import { describe, expect, it } from "vitest"

import {
  settlementSettingsQueryOptions,
  trackerSettingsQueryOptions,
} from "~/features/staff/api/operations.queries"
import { StaffSeedboxSettingsPage } from "~/features/staff/pages/staff-seedbox-settings-page"
import { createStaffPageQueryClient } from "~/features/staff/test-utils/create-staff-page-query-client"

describe("StaffSeedboxSettingsPage", () => {
  it("edits the signed Tracker seedbox policy without exposing addresses to Settlement", async () => {
    const queryClient = createStaffPageQueryClient([
      "tracker.policy.read",
      "tracker.policy.issue",
    ])
    queryClient.setQueryData(trackerSettingsQueryOptions().queryKey, {
      generated_at: "2026-08-16T08:00:00Z",
      configured: trackerPolicyRevision(),
      effective: {
        sequence: "1",
        revision: "tracker-runtime-default-v1",
        generated_at: "2026-08-16T08:00:00Z",
        settings: trackerPolicySettings(),
      },
      activation_pending: false,
      capacity: {
        peer_ttl_seconds: 2100,
        max_swarms: "100000",
        max_peers: "1000000",
        max_peers_per_swarm: "100000",
      },
    })
    queryClient.setQueryData(settlementSettingsQueryOptions().queryKey, {
      generated_at: "2026-08-16T08:00:00Z",
      hnr: emptyHNRPolicy(),
      seedbox: {
        settlement_primitive_supported: true,
        global_policy_configured: true,
        upload_factor_basis_points: 5000,
        classification_connected: false,
        registry_connected: false,
        speed_observation_connected: false,
      },
      global_ratio_watch_connected: false,
    })

    render(
      <MemoryRouter initialEntries={["/staff/settings/seedbox"]}>
        <QueryClientProvider client={queryClient}>
          <StaffSeedboxSettingsPage />
        </QueryClientProvider>
      </MemoryRouter>
    )

    expect(
      await screen.findByRole("heading", { name: "盒子设置" })
    ).toBeVisible()
    expect(screen.getByText("未启用")).toBeVisible()
    expect(screen.getByText("0.50x")).toBeVisible()
    expect(screen.getByRole("switch")).toBeVisible()
    expect(screen.getByLabelText("可信网段")).toHaveValue(
      "demo-box = 203.0.113.8/32"
    )
    expect(
      screen.getByText("Tracker 只发布分类证据，不把用户 IP 传给 Settlement。")
    ).toBeVisible()
  })
})

function trackerPolicyRevision() {
  return {
    sequence: "1",
    revision: "tracker-runtime-default-v1",
    settings: trackerPolicySettings(),
    reason: "初始 Tracker 运行政策基线。",
    created_at: "2026-08-16T07:59:00Z",
    issued_by: null,
  }
}

function trackerPolicySettings() {
  return {
    announce_interval_seconds: 1800,
    min_announce_interval_seconds: 900,
    default_numwant: 50,
    max_numwant: 100,
    scrape_enabled: true,
    max_scrape_hashes: 50,
    client_mode: "allow_all" as const,
    allowed_clients: [],
    user_requests_per_minute: 30,
    user_burst: 60,
    address_requests_per_minute: 120,
    address_burst: 240,
    seedbox: {
      enabled: false,
      upload_factor_basis_points: 5000,
      seedbox_speed_limit_bytes_per_second: 104857600,
      standard_speed_limit_bytes_per_second: 52428800,
      rules: [{ id: "demo-box", cidr: "203.0.113.8/32" }],
    },
  }
}

function emptyHNRPolicy() {
  return {
    configured: false,
    revision_id: "",
    effective_at: null,
    rule_id: "",
    rule_version: 0,
    mode: "" as const,
    required_seed_seconds: 0,
    required_ratio_basis_points: 0,
    assessment_window_seconds: 0,
    grace_period_seconds: 0,
    max_interval_credit_seconds: 0,
  }
}

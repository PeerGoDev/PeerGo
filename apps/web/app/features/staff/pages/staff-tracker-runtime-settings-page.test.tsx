import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import { MemoryRouter } from "react-router"
import { describe, expect, it } from "vitest"

import { sessionKeys } from "~/features/auth/api/session.mutations"
import { capabilityKeys } from "~/features/authz/api/capabilities.queries"
import { trackerSettingsQueryOptions } from "~/features/staff/api/operations.queries"
import { staffSessionKeys } from "~/features/staff/api/staff-session.mutations"
import { StaffTrackerRuntimeSettingsPage } from "~/features/staff/pages/staff-tracker-runtime-settings-page"

const administratorId = "0198f20a-6da8-7e51-9c64-111111111111"

describe("StaffTrackerRuntimeSettingsPage", () => {
  it("renders configured and effective Tracker policy separately from capacity", async () => {
    const queryClient = createQueryClient()
    render(
      <MemoryRouter initialEntries={["/staff/settings/tracker"]}>
        <QueryClientProvider client={queryClient}>
          <StaffTrackerRuntimeSettingsPage />
        </QueryClientProvider>
      </MemoryRouter>
    )

    expect(
      await screen.findByRole("heading", { name: "Tracker 设置" })
    ).toBeVisible()
    expect(screen.getAllByText("30 分钟").length).toBeGreaterThan(0)
    expect(screen.getByText("最小 15 分钟")).toBeVisible()
    expect(screen.getByText("Scrape")).toBeVisible()
    expect(screen.getByText("进程容量（只读）")).toBeVisible()
    expect(screen.getByText("已配置政策与 Tracker 实际状态一致")).toBeVisible()
    expect(screen.getByLabelText("修改原因")).not.toHaveAttribute("minlength")
    expect(screen.getByLabelText("修改原因")).toHaveAttribute(
      "placeholder",
      expect.stringContaining("系统会自动记录")
    )
    expect(screen.queryByText(/WAL|JetStream|service-token/)).toBeNull()
  })
})

function createQueryClient() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  })
  queryClient.setQueryData(sessionKeys.current(), {
    user: {
      id: administratorId,
      username: "admin",
      display_name: "管理员",
      email_verified: true,
    },
    expires_at: "2026-08-16T00:00:00Z",
    csrf_token: "c".repeat(43),
  })
  queryClient.setQueryData(capabilityKeys.current(administratorId), {
    policy_version: "policy-2026-08-15",
    items: [],
  })
  queryClient.setQueryData(staffSessionKeys.current(), {
    user: {
      id: administratorId,
      username: "admin",
      display_name: "管理员",
      email_verified: true,
    },
    expires_at: "2026-08-16T00:00:00Z",
    webauthn_authenticated_at: "2026-08-15T08:00:00Z",
    csrf_token: "s".repeat(43),
  })
  queryClient.setQueryData(staffSessionKeys.capabilities(administratorId), {
    policy_version: "policy-2026-08-15",
    items: ["tracker.policy.read", "tracker.policy.issue"].map((action) => ({
      action,
      description: action,
      scope: { type: "site", id: "peergo" },
      expires_at: "2026-08-16T00:00:00Z",
    })),
  })
  queryClient.setQueryData(trackerSettingsQueryOptions().queryKey, {
    generated_at: "2026-08-16T08:00:00Z",
    configured: {
      sequence: "1",
      revision: "tracker-runtime-default-v1",
      settings: trackerPolicySettings(),
      reason: "初始 Tracker 运行政策基线。",
      created_at: "2026-08-16T07:59:00Z",
      issued_by: null,
    },
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
  return queryClient
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
      download_factor_basis_points: 20000,
      seedbox_speed_limit_bytes_per_second: 0,
      standard_speed_limit_bytes_per_second: 0,
      rules: [],
    },
  }
}

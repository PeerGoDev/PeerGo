import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import { MemoryRouter } from "react-router"
import { describe, expect, it } from "vitest"

import { sessionKeys } from "~/features/auth/api/session.mutations"
import { capabilityKeys } from "~/features/authz/api/capabilities.queries"
import { vipProfileSettingsQueryOptions } from "~/features/staff/api/operations.queries"
import { staffSessionKeys } from "~/features/staff/api/staff-session.mutations"
import { StaffVIPProfileSettingsPage } from "~/features/staff/pages/staff-vip-profile-settings-page"

const administratorId = "0198f20a-6da8-7e51-9c64-111111111111"

describe("StaffVIPProfileSettingsPage", () => {
  it("shows the benefits enforced by Core without stale unsupported labels", async () => {
    const queryClient = createQueryClient()
    render(
      <MemoryRouter initialEntries={["/staff/settings/vip-profile"]}>
        <QueryClientProvider client={queryClient}>
          <StaffVIPProfileSettingsPage />
        </QueryClientProvider>
      </MemoryRouter>
    )

    expect(
      await screen.findByRole("heading", { name: "VIP 与用户资料" })
    ).toBeVisible()
    expect(screen.getByText("43")).toBeVisible()
    expect(screen.getByText("+20%")).toBeVisible()
    expect(screen.getByText("超速观察按 VIP 历史时段自动豁免")).toBeVisible()
    expect(
      screen.getByText("VIP 免费下载等权益照常生效，随后应用盒子上传/下载倍率")
    ).toBeVisible()
    expect(
      screen.queryByText("未接入 Tracker 控制规则")
    ).not.toBeInTheDocument()
    expect(screen.getByText("1 MB")).toBeVisible()
    expect(screen.getByText("32–1024 px 正方形")).toBeVisible()
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
    items: ["operations.monitor.read"].map((action) => ({
      action,
      description: action,
      scope: { type: "site", id: "peergo" },
      expires_at: "2026-08-16T00:00:00Z",
    })),
  })
  queryClient.setQueryData(vipProfileSettingsQueryOptions().queryKey, {
    generated_at: "2026-08-16T08:00:00Z",
    stats: {
      total_users: "12327",
      active_vip: "43",
      permanent_vip: "12",
      expiring_vip: "31",
      expired_vip: "7",
    },
    profile: {
      display_name_min_characters: 1,
      display_name_max_characters: 40,
      avatar_min_pixels: 32,
      avatar_max_pixels: 1024,
      avatar_max_bytes: "1048576",
      avatar_format: "jpeg",
    },
    benefits: {
      seeding_reward_policy_revision: "rousi-v1",
      seeding_reward_bonus_bps: 2000,
      free_download_enabled: false,
      share_ratio_exempt: false,
      newcomer_assessment_exempt: true,
      speed_limit_exempt: true,
      seedbox_no_discount: false,
    },
  })
  return queryClient
}

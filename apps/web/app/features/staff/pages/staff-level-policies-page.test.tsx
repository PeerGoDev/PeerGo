import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import { MemoryRouter } from "react-router"
import { describe, expect, it } from "vitest"

import { sessionKeys } from "~/features/auth/api/session.mutations"
import { capabilityKeys } from "~/features/authz/api/capabilities.queries"
import {
  contributionExperiencePolicyListQueryOptions,
  levelPolicyListQueryOptions,
} from "~/features/staff/api/seeding-reward-administration.queries"
import { staffSessionKeys } from "~/features/staff/api/staff-session.mutations"
import { StaffLevelPoliciesPage } from "~/features/staff/pages/staff-level-policies-page"

const administratorId = "0198f20a-6da8-7e51-9c64-111111111111"

describe("StaffLevelPoliciesPage", () => {
  it("renders actual thresholds, benefits and user distribution", async () => {
    const queryClient = createQueryClient()
    render(
      <MemoryRouter initialEntries={["/staff/settings/progression/levels"]}>
        <QueryClientProvider client={queryClient}>
          <StaffLevelPoliciesPage />
        </QueryClientProvider>
      </MemoryRouter>
    )

    expect(
      await screen.findByRole("heading", { name: "经验与等级" })
    ).toBeVisible()
    expect(screen.getByText("等级规则 #1")).toBeVisible()
    expect(screen.getByText("经验获取基线")).toBeVisible()
    expect(screen.getByText("+0.1 经验")).toBeVisible()
    expect(screen.getByText("已生效")).toBeVisible()
    expect(screen.getByRole("button", { name: "调整等级规则" })).toBeEnabled()
    expect(screen.getByText("Lv.2")).toBeVisible()
    expect(screen.getByText("1,000")).toBeVisible()
    expect(screen.getByText("2%")).toBeVisible()
    expect(screen.getByText("12,327")).toBeVisible()
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
    items: [
      "progression.level.policy.read",
      "progression.level.policy.issue",
      "progression.contribution.policy.read",
      "progression.contribution.policy.issue",
    ].map((action) => ({
      action,
      description: action,
      scope: { type: "site", id: "peergo" },
      expires_at: "2026-08-16T00:00:00Z",
    })),
  })
  queryClient.setQueryData(levelPolicyListQueryOptions().queryKey, {
    minimum_effective_at: "2026-08-19T00:00:00Z",
    items: [
      {
        policy_version: "rousi-v1",
        sequence: 1,
        effective_at: "1970-01-01T00:00:00Z",
        activation_status: "applied",
        applied_at: "1970-01-01T00:00:00Z",
        user_count: "12327",
        affected_user_count: "0",
        changed_level_count: "0",
        reason: "Rousi 迁移基线等级规则。",
        created_at: "1970-01-01T00:00:00Z",
        levels: [
          {
            level: 1,
            minimum_experience: "0",
            karma_bonus_bps: 0,
            seeding_count_bonus: 0,
            current_user_count: "12000",
          },
          {
            level: 2,
            minimum_experience: "1000",
            karma_bonus_bps: 200,
            seeding_count_bonus: 0,
            current_user_count: "327",
          },
        ],
      },
    ],
  })
  queryClient.setQueryData(
    contributionExperiencePolicyListQueryOptions().queryKey,
    {
      minimum_effective_from: "2026-08-21T00:00:00Z",
      total: "1",
      limit: 30,
      offset: 0,
      items: [
        {
          revision: "rousi-contribution-v1",
          effective_from: "2026-08-20T00:00:00Z",
          experience_per_upload_gib_milli: 100,
          experience_per_torrent_milli: 2_000,
          experience_per_account_day_milli: 1_000,
          snapshot_sha256: "a".repeat(64),
          reason: "Rousi 在线经验来源参数迁移后的首版基线。",
          created_at: "2026-08-19T23:00:00Z",
        },
      ],
    }
  )
  return queryClient
}

import { QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import { MemoryRouter } from "react-router"
import { describe, expect, it } from "vitest"

import { attendancePolicyListQueryOptions } from "~/features/staff/api/attendance-administration.queries"
import { economySettingsQueryOptions } from "~/features/staff/api/operations.queries"
import { StaffActivityRewardsSettingsPage } from "~/features/staff/pages/staff-activity-rewards-settings-page"
import {
  createStaffPageQueryClient,
  economySettingsFixture,
} from "~/features/staff/test-utils/create-staff-page-query-client"

describe("StaffActivityRewardsSettingsPage", () => {
  it("shows the real immutable attendance policy editor", async () => {
    const queryClient = createStaffPageQueryClient([
      "economy.attendance.policy.read",
      "economy.attendance.policy.issue",
    ])
    queryClient.setQueryData(
      economySettingsQueryOptions().queryKey,
      economySettingsFixture()
    )
    queryClient.setQueryData(attendancePolicyListQueryOptions().queryKey, {
      items: [
        {
          revision: "attendance-v1",
          effective_from: "2026-08-16T00:00:00Z",
          created_at: "2026-08-16T00:00:00Z",
          settings: {
            enabled: true,
            day_boundary_timezone: "Asia/Shanghai",
            fixed_enabled: true,
            fixed_reward: "5",
            random_enabled: true,
            random_min: "1",
            random_max: "20",
            streak_enabled: true,
            streak_milestones: [
              { days: 7, reward: "5" },
              { days: 14, reward: "10" },
              { days: 30, reward: "20" },
            ],
            experience_reward: "5",
          },
          snapshot_sha256: "a".repeat(64),
          reason: "PeerGo 首版签到兼容基线",
        },
      ],
      total: "1",
      limit: 30,
      offset: 0,
      minimum_effective_from: "2026-08-17T16:00:00Z",
    })

    render(
      <MemoryRouter initialEntries={["/staff/settings/activity-rewards"]}>
        <QueryClientProvider client={queryClient}>
          <StaffActivityRewardsSettingsPage />
        </QueryClientProvider>
      </MemoryRouter>
    )

    expect(
      await screen.findByRole("heading", { name: "签到与活动奖励" })
    ).toBeVisible()
    expect(screen.getByText("签到设置历史")).toBeVisible()
    expect(screen.getByText("已接通")).toBeVisible()
    expect(screen.getByText("12")).toBeVisible()
    expect(screen.getAllByRole("switch").length).toBeGreaterThan(3)
    expect(screen.getByRole("button", { name: "保存并按期生效" })).toBeVisible()
  })
})

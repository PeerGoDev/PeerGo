import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter } from "react-router"
import { describe, expect, it } from "vitest"

import { sessionKeys } from "~/features/auth/api/session.mutations"
import { attendanceOverviewQueryOptions } from "~/features/economy/api/attendance.queries"
import {
  economyKeys,
  type EconomyOverview,
} from "~/features/economy/api/economy.queries"
import { EconomyPage } from "~/features/economy/pages/economy-page"

const userId = "0198f20a-6da8-7e51-9c64-111111111111"

describe("EconomyPage", () => {
  it("restores the compact level dashboard and separates the legacy-style sections", async () => {
    const user = userEvent.setup()
    const queryClient = economyQueryClient()

    render(
      <MemoryRouter initialEntries={["/account/economy"]}>
        <QueryClientProvider client={queryClient}>
          <EconomyPage />
        </QueryClientProvider>
      </MemoryRouter>
    )

    expect(await screen.findByText("当前等级")).toBeVisible()
    expect(screen.getAllByText("Lv.2").length).toBeGreaterThan(0)
    expect(screen.getByText("经验值 1,319.84")).toBeVisible()
    expect(screen.getByText("下一级 5,000")).toBeVisible()
    expect(screen.getByText("魔力值 +2%")).toBeVisible()
    expect(screen.getByText("数量奖励计数 60 个")).toBeVisible()
    expect(screen.getByText("连续 2 天")).toBeVisible()
    expect(screen.getByText("做种奖励公式")).toBeVisible()

    await user.click(screen.getByRole("button", { name: "消费与交易" }))

    expect(screen.getByText("现行站点统一使用魔力值")).toBeVisible()
    expect(screen.getByRole("link", { name: "查看勋章" })).toHaveAttribute(
      "href",
      "/medals"
    )
    expect(screen.getByRole("link", { name: "查看购买" })).toHaveAttribute(
      "href",
      "/account/purchases"
    )

    await user.click(screen.getByRole("button", { name: "记录" }))

    expect(screen.getByText("魔力值明细")).toBeVisible()
    expect(screen.getByText("种子促销")).toBeVisible()
    expect(screen.getByText("经验记录")).toBeVisible()
  })
})

function economyQueryClient() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  })
  queryClient.setQueryData(sessionKeys.current(), {
    user: {
      id: userId,
      username: "admin",
      display_name: "管理员",
      email_verified: true,
    },
    expires_at: "2026-09-19T01:30:00Z",
    csrf_token: "c".repeat(43),
  })
  queryClient.setQueryData(economyKeys.current(userId), economyOverview)
  queryClient.setQueryData(
    attendanceOverviewQueryOptions(userId).queryKey,
    attendanceOverview
  )
  return queryClient
}

const attendanceOverview = {
  policy: {
    revision: "attendance-v1",
    effective_from: "2026-08-18T00:00:00Z",
    created_at: "2026-08-17T00:00:00Z",
    settings: {
      enabled: true,
      day_boundary_timezone: "Asia/Shanghai",
      fixed_enabled: true,
      fixed_reward: "100",
      random_enabled: true,
      random_min: "1",
      random_max: "300",
      streak_enabled: true,
      streak_milestones: [{ days: 7, reward: "200" }],
      experience_reward: "1",
    },
    snapshot_sha256: "a".repeat(64),
    reason: "测试签到规则",
  },
  claimed_today: true,
  today: "2026-08-25",
  current_streak: 2,
  total_days: 193,
  longest_streak: 181,
  today_record: {
    attendance_date: "2026-08-25",
    day_boundary_timezone: "Asia/Shanghai",
    mode: "fixed" as const,
    base_reward: "100",
    streak_reward: "0",
    total_reward: "100",
    experience_reward: "1",
    current_streak: 2,
    total_days: 193,
    longest_streak: 181,
    policy_revision: "attendance-v1",
    occurred_at: "2026-08-25T08:00:00Z",
  },
  history: [],
}

const economyOverview: EconomyOverview = {
  magic_balance: "10444852097",
  magic_updated_at: "2026-08-25T08:00:00Z",
  magic_entries: [
    {
      sequence: "8",
      transaction_type: "promotion_product_purchase",
      entry_type: "spend",
      amount: "-88888",
      balance_after: "10444852097",
      source_reference: "promotion:8",
      policy_revision: "promotion-v1",
      occurred_at: "2026-08-25T08:00:00Z",
    },
  ],
  experience_entries: [
    {
      sequence: "9",
      entry_type: "earn",
      amount: "1",
      balance_after: "1319.84",
      source_kind: "activity",
      policy_revision: "attendance-v1",
      level_after: 2,
      occurred_at: "2026-08-25T08:00:00Z",
    },
  ],
  progress: {
    experience: "1319.84",
    level: 2,
    policy_version: "rousi-compatible-v1",
    current_minimum_experience: "1000",
    next: { level: 3, minimum_experience: "5000" },
    updated_at: "2026-08-25T08:00:00Z",
  },
  rules: {
    level_policy_version: "rousi-compatible-v1",
    contribution_experience: {
      revision: "rousi-contribution-v1",
      effective_from: "2026-08-18T00:00:00Z",
      experience_per_upload_gib_milli: 100,
      experience_per_torrent_milli: 2_000,
      experience_per_account_day_milli: 1_000,
    },
    seeding_reward: {
      revision: "seeding-v1",
      formula_version: "nexus-atan-active-v1",
      effective_from: "2026-08-18T00:00:00Z",
      curve_hourly_cap_milli: 100_000,
      age_saturation_seconds: 2_419_200,
      seeder_decay: 7,
      curve_scale_milli: 300_000,
      size_multiplier_bps: 10_000,
      official_bonus_bps: 10_000,
      upload_contribution_bonus_bps: 5_000,
      per_torrent_hourly_milli: 500,
      base_linear_torrent_limit: 60,
      maximum_level_torrent_bonus: 55,
      minimum_torrent_bytes: 52_428_800,
      minimum_active_seconds: 300,
      maximum_snapshot_age_seconds: 600,
      vip_bonus_bps: 2_000,
      maximum_medal_bonus_bps: 10_000,
      maximum_level_bonus_bps: 2_000,
      maximum_hourly_reward: 500,
      experience_per_magic_bps: 200,
    },
    levels: [
      {
        level: 1,
        minimum_experience: "0",
        karma_bonus_bps: 0,
        seeding_count_bonus: 0,
      },
      {
        level: 2,
        minimum_experience: "1000",
        karma_bonus_bps: 200,
        seeding_count_bonus: 0,
      },
      {
        level: 3,
        minimum_experience: "5000",
        karma_bonus_bps: 400,
        seeding_count_bonus: 5,
      },
    ],
  },
  latest_seeding_reward: {
    window_start: "2026-08-25T07:00:00Z",
    window_end: "2026-08-25T08:00:00Z",
    policy_revision: "seeding-v1",
    eligible_torrent_count: 8,
    curve_reward_milli: "12500",
    linear_reward_milli: "4000",
    base_reward_milli: "16500",
    vip_bonus_milli: "1650",
    medal_bonus_milli: "825",
    level_bonus_milli: "330",
    uncapped_reward: "19",
    reward: "19",
    experience_amount: "0.38",
    capped: false,
    calculated_at: "2026-08-25T08:02:00Z",
  },
}

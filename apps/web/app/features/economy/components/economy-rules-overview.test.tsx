import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"

import { attendanceOverviewQueryOptions } from "~/features/economy/api/attendance.queries"
import type { EconomyOverview } from "~/features/economy/api/economy.queries"
import { EconomyRulesOverview } from "~/features/economy/components/economy-rules-overview"

const overview: EconomyOverview = {
  magic_balance: "10444852097",
  magic_updated_at: "2026-08-18T12:00:00Z",
  magic_entries: [],
  experience_entries: [],
  progress: {
    experience: "1319.84",
    level: 2,
    policy_version: "rousi-compatible-v1",
    current_minimum_experience: "1000",
    next: { level: 3, minimum_experience: "5000" },
    updated_at: "2026-08-18T12:00:00Z",
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
      maximum_medal_bonus_bps: 2_000,
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
        karma_bonus_bps: 100,
        seeding_count_bonus: 5,
      },
      {
        level: 3,
        minimum_experience: "5000",
        karma_bonus_bps: 200,
        seeding_count_bonus: 10,
      },
    ],
  },
  latest_seeding_reward: {
    window_start: "2026-08-18T11:00:00Z",
    window_end: "2026-08-18T12:00:00Z",
    policy_revision: "seeding-v1",
    eligible_torrent_count: 8,
    curve_reward_milli: "12500",
    linear_reward_milli: "4000",
    base_reward_milli: "16500",
    vip_bonus_milli: "1650",
    medal_bonus_milli: "825",
    level_bonus_milli: "165",
    uncapped_reward: "19",
    reward: "19",
    experience_amount: "0.38",
    capped: false,
    calculated_at: "2026-08-18T12:02:00Z",
  },
}

describe("EconomyRulesOverview", () => {
  it("renders the effective acquisition, formula, calculation, and level rules", async () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false, staleTime: Infinity } },
    })
    queryClient.setQueryData(
      attendanceOverviewQueryOptions("member-1").queryKey,
      {
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
          reason: "验证用户侧签到规则展示完整",
        },
        claimed_today: false,
        today: "2026-08-18",
        current_streak: 2,
        total_days: 10,
        longest_streak: 4,
        history: [],
      }
    )

    render(
      <QueryClientProvider client={queryClient}>
        <EconomyRulesOverview overview={overview} userId="member-1" />
      </QueryClientProvider>
    )

    expect(await screen.findByText("魔力值获取方式")).toBeVisible()
    expect(screen.getByText("做种奖励公式")).toBeVisible()
    expect(screen.getByText("经验获取方式")).toBeVisible()
    expect(screen.getByText("每 1 GiB +0.1")).toBeVisible()
    expect(screen.getByText("每个 +2")).toBeVisible()
    expect(screen.getByText("每天 +1")).toBeVisible()
    expect(screen.getByText("等级一览")).toBeVisible()
    expect(screen.getByText("最高 500 / 小时")).toBeVisible()
    expect(screen.getByText("100 或 1–300 魔力值")).toBeVisible()
    expect(screen.getByText("100 / 小时")).toBeVisible()
    expect(screen.getByText("0.5 / 小时")).toBeVisible()
    expect(screen.getAllByText("65 个").length).toBeGreaterThan(0)
    expect(screen.getByText("12.5")).toBeVisible()
    expect(screen.getByText("4")).toBeVisible()
    expect(screen.getByText("19")).toBeVisible()
    expect(screen.getByText("当前")).toBeVisible()
  })
})

import { describe, expect, it } from "vitest"

import {
  recommendedSeedingRewardPolicy,
  seedingRewardPolicyDraft,
} from "~/features/staff/model/seeding-reward-policy-preset"

describe("recommended seeding reward policy", () => {
  it("matches Rousi's live curve and PeerGo's bounded evidence defaults", () => {
    expect(
      recommendedSeedingRewardPolicy(
        "peergo-seeding-2026081900",
        "2026-08-19T00:00:00Z"
      )
    ).toEqual({
      revision: "peergo-seeding-2026081900",
      formula_version: "nexus-atan-active-v1",
      effective_from: "2026-08-19T00:00:00Z",
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
    })
  })

  it("uses the recommended policy for a site's first draft", () => {
    const draft = seedingRewardPolicyDraft(
      undefined,
      "2026-08-19T03:00:00+00:00"
    )

    expect(draft.revision).toBe("peergo-seeding-2026081903")
    expect(draft.base_linear_torrent_limit).toBe(60)
    expect(draft.vip_bonus_bps).toBe(2_000)
    expect(draft.experience_per_magic_bps).toBe(200)
  })
})

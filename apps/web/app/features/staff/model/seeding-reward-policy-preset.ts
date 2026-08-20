import type { components } from "~/generated/api"

type SeedingRewardPolicy = components["schemas"]["SeedingRewardPolicy"]
type SeedingRewardPolicyInput =
  components["schemas"]["SeedingRewardPolicyInput"]
type SeedingRewardPolicyDefaults = Omit<
  SeedingRewardPolicyInput,
  "revision" | "effective_from"
>

/**
 * PeerGo's first production baseline follows the values currently used by
 * Rousi for the Nexus-style curve. PeerGo-specific evidence limits and the
 * final hourly ceiling stay explicit so a stale Tracker snapshot or stacked
 * benefits cannot silently inflate the ledger.
 */
export const RECOMMENDED_SEEDING_REWARD_DEFAULTS = {
  formula_version: "nexus-atan-active-v1",
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
  // Rousi displays 0.05 GB; store the exact 50 MiB eligibility boundary.
  minimum_torrent_bytes: 52_428_800,
  minimum_active_seconds: 300,
  maximum_snapshot_age_seconds: 600,
  vip_bonus_bps: 2_000,
  // Rousi's live medal setting caps the combined magic bonus at 100%.
  maximum_medal_bonus_bps: 10_000,
  maximum_level_bonus_bps: 2_000,
  maximum_hourly_reward: 500,
  experience_per_magic_bps: 200,
} as const satisfies SeedingRewardPolicyDefaults

export function recommendedSeedingRewardPolicy(
  revision: string,
  effectiveFrom: string
): SeedingRewardPolicyInput {
  return {
    revision,
    effective_from: effectiveFrom,
    ...RECOMMENDED_SEEDING_REWARD_DEFAULTS,
  }
}

export function seedingRewardPolicyDraft(
  latest: SeedingRewardPolicy | undefined,
  minimumEffectiveFrom: string
): SeedingRewardPolicyInput {
  const revision = recommendedRevision(minimumEffectiveFrom)
  if (!latest) {
    return recommendedSeedingRewardPolicy(revision, minimumEffectiveFrom)
  }

  const {
    snapshot_sha256: _snapshot,
    issued_by: _issuer,
    reason: _reason,
    created_at: _created,
    ...input
  } = latest
  return {
    ...input,
    revision,
    effective_from: minimumEffectiveFrom,
  }
}

function recommendedRevision(effectiveFrom: string) {
  const timestamp = new Date(effectiveFrom).toISOString().slice(0, 13)
  return `peergo-seeding-${timestamp.replaceAll(/[-T]/g, "")}`
}

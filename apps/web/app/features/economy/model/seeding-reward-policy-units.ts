export type SeedingRewardDisplayUnit =
  | "integer"
  | "milli"
  | "percent"
  | "gibibytes"
  | "weeks"
  | "minutes"
  | "ratio"

const POLICY_UNITS_PER_DISPLAY_UNIT: Record<SeedingRewardDisplayUnit, number> =
  {
    integer: 1,
    milli: 1_000,
    percent: 100,
    gibibytes: 1_073_741_824,
    weeks: 604_800,
    minutes: 60,
    ratio: 10_000,
  }

/**
 * Core stores every policy value as an integer. Both the member-facing rules
 * page and staff editor use this boundary so their units cannot drift apart.
 */
export function fromSeedingRewardPolicyUnit(
  value: number,
  unit: SeedingRewardDisplayUnit
) {
  return Number((value / POLICY_UNITS_PER_DISPLAY_UNIT[unit]).toFixed(6))
}

export function toSeedingRewardPolicyUnit(
  value: number,
  unit: SeedingRewardDisplayUnit
) {
  return Math.round(value * POLICY_UNITS_PER_DISPLAY_UNIT[unit])
}

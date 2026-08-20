import { describe, expect, it } from "vitest"

import {
  fromSeedingRewardPolicyUnit,
  toSeedingRewardPolicyUnit,
  type SeedingRewardDisplayUnit,
} from "~/features/economy/model/seeding-reward-policy-units"

describe("seeding reward policy display units", () => {
  it.each<[SeedingRewardDisplayUnit, number, number]>([
    ["integer", 50, 50],
    ["milli", 100_000, 100],
    ["percent", 2_000, 20],
    ["gibibytes", 53_687_091, 0.05],
    ["weeks", 2_419_200, 4],
    ["minutes", 900, 15],
    ["ratio", 1_000, 0.1],
  ])("converts %s policy values into business units", (unit, stored, shown) => {
    expect(fromSeedingRewardPolicyUnit(stored, unit)).toBeCloseTo(shown)
  })

  it.each<[SeedingRewardDisplayUnit, number, number]>([
    ["milli", 0.5, 500],
    ["percent", 20, 2_000],
    ["gibibytes", 0.05, 53_687_091],
    ["weeks", 4, 2_419_200],
    ["minutes", 5, 300],
    ["ratio", 0.1, 1_000],
  ])("rounds %s inputs back to the integer contract", (unit, shown, stored) => {
    expect(toSeedingRewardPolicyUnit(shown, unit)).toBe(stored)
  })
})

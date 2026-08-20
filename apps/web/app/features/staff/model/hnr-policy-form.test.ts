import { describe, expect, it } from "vitest"

import {
  hnrPolicyDraftFromCurrent,
  hnrPolicyFromDraft,
} from "~/features/staff/model/hnr-policy-form"

describe("H&R policy form units", () => {
  it("converts familiar operator units into exact settlement units", () => {
    const policy = hnrPolicyFromDraft({
      mode: "enforced",
      seedHours: "72",
      ratio: "1.25",
      assessmentDays: "7",
      graceHours: "24",
      intervalMinutes: "60",
    })

    expect(policy).toEqual({
      mode: "enforced",
      required_seed_seconds: 259_200,
      required_ratio_basis_points: 12_500,
      assessment_window_seconds: 604_800,
      grace_period_seconds: 86_400,
      max_interval_credit_seconds: 3600,
    })
  })

  it("zeroes thresholds when H&R is disabled or globally exempted", () => {
    expect(
      hnrPolicyFromDraft({
        mode: "exempt",
        seedHours: "72",
        ratio: "1",
        assessmentDays: "7",
        graceHours: "24",
        intervalMinutes: "60",
      })
    ).toMatchObject({
      mode: "exempt",
      required_seed_seconds: 0,
      required_ratio_basis_points: 0,
    })
  })

  it("rejects an assessment period shorter than the seeding requirement", () => {
    expect(
      hnrPolicyFromDraft({
        mode: "enforced",
        seedHours: "240",
        ratio: "1",
        assessmentDays: "7",
        graceHours: "24",
        intervalMinutes: "60",
      })
    ).toBeUndefined()
  })

  it("keeps an existing policy editable without changing its units", () => {
    expect(
      hnrPolicyDraftFromCurrent({
        configured: true,
        revision_id: "revision",
        effective_at: "2026-08-16T08:00:00Z",
        rule_id: "global-default",
        rule_version: 2,
        mode: "enforced",
        required_seed_seconds: 259_200,
        required_ratio_basis_points: 10_000,
        assessment_window_seconds: 604_800,
        grace_period_seconds: 86_400,
        max_interval_credit_seconds: 3600,
      })
    ).toEqual({
      mode: "enforced",
      seedHours: "72",
      ratio: "1",
      assessmentDays: "7",
      graceHours: "24",
      intervalMinutes: "60",
    })
  })
})

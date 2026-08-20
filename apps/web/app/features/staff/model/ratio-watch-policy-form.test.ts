import { describe, expect, it } from "vitest"

import {
  ratioWatchDraftFromCurrent,
  ratioWatchPolicyFromDraft,
} from "~/features/staff/model/ratio-watch-policy-form"

describe("ratio watch policy form", () => {
  it("maps the familiar PtYes defaults to exact persisted units", () => {
    expect(ratioWatchPolicyFromDraft(ratioWatchDraftFromCurrent(null))).toEqual(
      {
        enabled: true,
        download_threshold_bytes: "53687091200",
        minimum_ratio_basis_points: 4000,
        watch_period_seconds: 1_209_600,
        restriction_ratio_basis_points: 3000,
      }
    )
  })

  it("rejects a restriction ratio above the recovery target", () => {
    expect(
      ratioWatchPolicyFromDraft({
        enabled: true,
        thresholdGiB: "50",
        minimumRatio: "0.4",
        watchDays: "14",
        restrictionRatio: "0.5",
      })
    ).toBeUndefined()
  })

  it("disables the policy without carrying hidden values", () => {
    expect(
      ratioWatchPolicyFromDraft({
        enabled: false,
        thresholdGiB: "50",
        minimumRatio: "0.4",
        watchDays: "14",
        restrictionRatio: "0.3",
      })
    ).toEqual({
      enabled: false,
      download_threshold_bytes: "0",
      minimum_ratio_basis_points: 0,
      watch_period_seconds: 0,
      restriction_ratio_basis_points: 0,
    })
  })
})

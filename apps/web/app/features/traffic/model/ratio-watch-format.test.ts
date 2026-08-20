import { describe, expect, it } from "vitest"

import {
  formatDeadlineRemaining,
  formatRatioBasisPoints,
  formatWatchPeriod,
} from "~/features/traffic/model/ratio-watch-format"

describe("ratio watch formatting", () => {
  it("renders familiar share ratios and whole-day observation periods", () => {
    expect(formatRatioBasisPoints(4_000)).toBe("0.40")
    expect(formatRatioBasisPoints(12_345)).toBe("1.23")
    expect(formatWatchPeriod(14 * 86_400)).toBe("14 天")
  })

  it("derives a stable remaining duration from server timestamps", () => {
    expect(
      formatDeadlineRemaining("2026-08-18T15:00:00Z", "2026-08-16T12:00:00Z")
    ).toBe("还剩 2 天 3 小时")
    expect(
      formatDeadlineRemaining("2026-08-16T11:59:59Z", "2026-08-16T12:00:00Z")
    ).toBe("观察期已结束")
  })
})

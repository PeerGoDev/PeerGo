import { describe, expect, it } from "vitest"

import {
  formatHNRCount,
  formatHNRDuration,
  formatHNRRatio,
  hnrProgressPercent,
} from "~/features/traffic/model/hnr-format"

describe("H&R formatters", () => {
  it("formats duration, ratio and counts with bigint arithmetic", () => {
    expect(formatHNRDuration("176400")).toBe("2 天 1 小时")
    expect(formatHNRRatio("12500")).toBe("1.25")
    expect(formatHNRCount("9007199254740993", "7")).toBe(
      "9,007,199,254,741,000"
    )
  })

  it("caps progress without converting the source counters to number", () => {
    expect(hnrProgressPercent("7200", "14400")).toBe(50)
    expect(hnrProgressPercent("18014398509481986", "9007199254740993")).toBe(
      100
    )
    expect(hnrProgressPercent("1", "0")).toBe(100)
  })
})

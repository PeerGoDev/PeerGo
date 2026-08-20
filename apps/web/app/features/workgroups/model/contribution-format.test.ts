import { describe, expect, it } from "vitest"

import {
  contributionMetricLabel,
  contributionPercent,
  formatContributionValue,
} from "~/features/workgroups/model/contribution-format"

describe("workgroup contribution formatting", () => {
  it("keeps fixed business metric labels", () => {
    expect(contributionMetricLabel("trusted_torrents_published")).toBe(
      "本月可信发布"
    )
    expect(contributionMetricLabel("torrent_review_votes")).toBe("本月有效审核")
  })

  it("formats aggregate seeding time without decimals", () => {
    expect(formatContributionValue("seeding_active_seconds", 176400)).toBe(
      "2 天 1 小时"
    )
  })

  it("caps visual progress while preserving the raw value elsewhere", () => {
    expect(contributionPercent(25, 20)).toBe(100)
    expect(contributionPercent(5, 20)).toBe(25)
  })
})

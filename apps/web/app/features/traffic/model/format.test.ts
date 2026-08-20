import { describe, expect, it } from "vitest"

import type { components } from "~/generated/api"
import {
  formatShareRatio,
  trafficAdjustmentLabels,
} from "~/features/traffic/model/format"

type TrafficEntry = components["schemas"]["TrafficEntry"]

const baseEntry: TrafficEntry = {
  id: "0198f20a-6da8-7e51-9c64-111111111111",
  torrent: {
    id: 42,
    title: "Release",
  },
  interval_started_at: "2026-08-09T11:00:00Z",
  interval_ended_at: "2026-08-09T11:30:00Z",
  raw_uploaded_bytes: "500",
  raw_downloaded_bytes: "300",
  credited_uploaded_bytes: "1000",
  charged_downloaded_bytes: "0",
  explanation: {
    status: "not_available",
    segment_count: "0",
    segments: [],
  },
  settled_at: "2026-08-09T11:31:00Z",
}

describe("traffic formatters", () => {
  it("calculates the credited-to-charged ratio with bigint arithmetic", () => {
    expect(formatShareRatio("9007199254740993", "3")).toBe(
      "3,002,399,751,580,331.00"
    )
    expect(formatShareRatio("1", "3", 3)).toBe("0.333")
    expect(formatShareRatio("1000", "0")).toBe("∞")
    expect(formatShareRatio("0", "0")).toBe("—")
  })

  it("derives display labels from final values without re-evaluating policy", () => {
    expect(trafficAdjustmentLabels(baseEntry)).toEqual(["上传 2×", "免费下载"])
  })
})

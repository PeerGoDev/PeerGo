import { describe, expect, it } from "vitest"

import {
  formatRelativeTime,
  formatTorrentSize,
  formatTorrentSizeParts,
  getTorrentSwarmFreshness,
} from "~/features/torrent/model/format"

describe("torrent formatters", () => {
  it("formats relative times against an injected current time", () => {
    const now = Date.parse("2026-08-05T12:00:00Z")
    expect(formatRelativeTime("2026-08-05T11:30:00Z", now)).toBe("30分钟前")
  })

  it("distinguishes an unobserved import from stale and fresh Tracker data", () => {
    expect(
      getTorrentSwarmFreshness({
        swarm_observed_at: "1970-01-01T00:00:00Z",
        swarm_stale: true,
      })
    ).toBe("unavailable")
    expect(
      getTorrentSwarmFreshness({
        swarm_observed_at: "2026-08-05T09:00:00Z",
        swarm_stale: true,
      })
    ).toBe("stale")
    expect(
      getTorrentSwarmFreshness({
        swarm_observed_at: "2026-08-05T09:00:00Z",
        swarm_stale: false,
      })
    ).toBe("fresh")
  })

  it("formats torrent sizes with up to two decimals and semantic unit tones", () => {
    expect(formatTorrentSizeParts(1_073_741_824)).toEqual({
      value: "1",
      unit: "GB",
      tone: "green",
    })
    expect(formatTorrentSizeParts(850 * 1024 * 1024)).toEqual({
      value: "850",
      unit: "MB",
      tone: "blue",
    })
    expect(formatTorrentSize(34_273_555_333)).toBe("31.92 GB")
    expect(formatTorrentSizeParts(-1)).toBeUndefined()
  })
})

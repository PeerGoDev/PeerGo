import { describe, expect, it } from "vitest"

import { formatBytes } from "~/shared/formatters/bytes"

describe("formatBytes", () => {
  it("formats existing numeric torrent sizes", () => {
    expect(formatBytes(18_742_348_800)).toBe("17.5 GB")
    expect(formatBytes(-1)).toBe("—")
  })

  it("keeps decimal ledger strings exact beyond JavaScript safe integers", () => {
    expect(formatBytes("9007199254740993")).toBe("8 PB")
    expect(formatBytes("18014398509481986")).toBe("16 PB")
    expect(formatBytes("not-a-counter")).toBe("—")
  })
})

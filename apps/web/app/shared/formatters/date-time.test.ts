import { describe, expect, it } from "vitest"

import {
  formatCompactDate,
  formatCompactDateTime,
  formatDateTime,
  formatLongDate,
} from "~/shared/formatters/date-time"

describe("formatDateTime", () => {
  it("uses one safe fallback for invalid timestamps", () => {
    expect(formatDateTime("not-a-date")).toBe("时间未知")
    expect(formatLongDate("not-a-date")).toBe("时间未知")
    expect(formatCompactDateTime("not-a-date")).toBe("时间未知")
    expect(formatCompactDate("not-a-date")).toBe("时间未知")
  })

  it("formats a Chinese long date without adding time", () => {
    expect(formatLongDate("2026-08-09T12:00:00Z")).toBe("2026年8月9日")
  })
})

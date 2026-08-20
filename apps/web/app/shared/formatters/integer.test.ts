import { describe, expect, it } from "vitest"

import {
  formatCompactInteger,
  formatInteger,
} from "~/shared/formatters/integer"

describe("formatInteger", () => {
  it("formats exact signed integer strings without losing precision", () => {
    expect(formatInteger("31328711552")).toBe("31,328,711,552")
    expect(formatInteger("-10701")).toBe("-10,701")
    expect(formatInteger("1.5")).toBe("—")
  })

  it("compacts narrow-surface values without losing integer precision", () => {
    expect(formatCompactInteger("999")).toBe("999")
    expect(formatCompactInteger("20403550")).toBe("20.4M")
    expect(formatCompactInteger("10444000000")).toBe("10.4B")
    expect(formatCompactInteger("999999")).toBe("1.0M")
    expect(formatCompactInteger("9007199254740993")).toBe("9007.2T")
    expect(formatCompactInteger("-10701")).toBe("-10.7K")
    expect(formatCompactInteger("1.5")).toBe("—")
  })
})

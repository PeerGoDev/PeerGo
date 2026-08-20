import { describe, expect, it } from "vitest"

import { opaqueTokenPattern } from "~/features/auth/model/opaque-token"

describe("opaqueTokenPattern", () => {
  it("accepts only an unpadded 32-byte base64url credential", () => {
    expect(opaqueTokenPattern.test("a".repeat(43))).toBe(true)
    expect(opaqueTokenPattern.test("a".repeat(42))).toBe(false)
    expect(opaqueTokenPattern.test(`${"a".repeat(42)}+`)).toBe(false)
  })
})

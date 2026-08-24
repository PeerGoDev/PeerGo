import { describe, expect, it } from "vitest"

import { invitationTokenPattern } from "~/features/auth/model/invitation-token"

describe("invitationTokenPattern", () => {
  it("accepts PeerGo and still-valid Rousi invitation credentials", () => {
    expect(invitationTokenPattern.test("a".repeat(43))).toBe(true)
    expect(invitationTokenPattern.test("a".repeat(64))).toBe(true)
    expect(invitationTokenPattern.test("A".repeat(64))).toBe(false)
    expect(invitationTokenPattern.test("a".repeat(63))).toBe(false)
  })
})

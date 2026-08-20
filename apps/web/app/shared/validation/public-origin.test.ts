import { describe, expect, it } from "vitest"

import { isSecurePublicOrigin } from "~/shared/validation/public-origin"

describe("isSecurePublicOrigin", () => {
  it.each([
    ["https://peergo.example", true],
    ["https://peergo.example/", true],
    ["http://peergo.example", false],
    ["https://user@peergo.example", false],
    ["https://peergo.example/reset-password", false],
    ["https://peergo.example?next=reset", false],
    ["not-an-origin", false],
  ])("validates %s", (origin, expected) => {
    expect(isSecurePublicOrigin(origin)).toBe(expected)
  })
})

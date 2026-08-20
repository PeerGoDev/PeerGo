import { describe, expect, it } from "vitest"

import { passwordRecoveryFormSchema } from "~/features/auth/model/password-recovery-form"

describe("password recovery form", () => {
  it("shares the 12-character password boundary and requires confirmation", () => {
    expect(
      passwordRecoveryFormSchema.safeParse({
        password: "short",
        confirmPassword: "different",
      }).success
    ).toBe(false)
    expect(
      passwordRecoveryFormSchema.safeParse({
        password: "PeerGo-new-password-2026!",
        confirmPassword: "PeerGo-new-password-2026!",
      }).success
    ).toBe(true)
  })
})

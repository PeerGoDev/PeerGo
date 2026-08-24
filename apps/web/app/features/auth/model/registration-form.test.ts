import { describe, expect, it } from "vitest"

import { registrationFormSchema } from "~/features/auth/model/registration-form"

const validInput = {
  username: "new_member",
  displayName: "新成员",
  email: "member@example.com",
  password: "PeerGo-member-2026!",
  confirmPassword: "PeerGo-member-2026!",
  invitationToken: "cGVlcmdvLWRldmVsb3BtZW50LWludml0ZS12MSEhISE",
}

describe("registrationFormSchema", () => {
  it("requires an invitation only in invite mode", () => {
    expect(registrationFormSchema("invite").safeParse(validInput).success).toBe(
      true
    )
    expect(
      registrationFormSchema("invite").safeParse({
        ...validInput,
        invitationToken: "",
      }).success
    ).toBe(false)
    expect(
      registrationFormSchema("open").safeParse({
        ...validInput,
        invitationToken: "",
      }).success
    ).toBe(true)
    expect(registrationFormSchema("open").safeParse(validInput).success).toBe(
      true
    )
    expect(
      registrationFormSchema("open").safeParse({
        ...validInput,
        invitationToken: "invalid",
      }).success
    ).toBe(false)
  })

  it("accepts a migrated Rousi invitation credential", () => {
    expect(
      registrationFormSchema("invite").safeParse({
        ...validInput,
        invitationToken: "a".repeat(64),
      }).success
    ).toBe(true)
  })

  it("rejects uppercase usernames and mismatched passwords", () => {
    const result = registrationFormSchema("open").safeParse({
      ...validInput,
      username: "NewMember",
      confirmPassword: "different-password",
    })
    expect(result.success).toBe(false)
    if (!result.success) {
      expect(result.error.flatten().fieldErrors.username).toBeDefined()
      expect(result.error.flatten().fieldErrors.confirmPassword).toBeDefined()
    }
  })

  it("uses the public username bounds from the active registration policy", () => {
    const schema = registrationFormSchema({
      mode: "open",
      usernameMinCharacters: 5,
      usernameMaxCharacters: 8,
    })
    expect(
      schema.safeParse({
        ...validInput,
        username: "four",
        invitationToken: "",
      }).success
    ).toBe(false)
    expect(
      schema.safeParse({
        ...validInput,
        username: "member9",
        invitationToken: "",
      }).success
    ).toBe(true)
  })
})

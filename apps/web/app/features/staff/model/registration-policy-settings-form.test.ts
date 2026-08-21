import { describe, expect, it } from "vitest"

import { registrationPolicySettingsFormSchema } from "~/features/staff/model/registration-policy-settings-form"

const validInput = {
  mode: "invite" as const,
  memberInvitesEnabled: true,
  inviteValidDays: 7,
  maxInvitesPerMember: 5,
  minimumInviteAccountAgeDays: 30,
  minimumInviteLevel: 2,
  usernameMinCharacters: 3,
  usernameMaxCharacters: 20,
  reservedUsernames: " Root, admin\nroot ",
  emailDomainMode: "blocklist" as const,
  emailDomains: " Trash.Example\ntrash.example ",
  sessionValidHours: 168,
  rememberSessionValidHours: 720,
  humanVerificationProvider: "disabled" as const,
  humanVerificationSiteKey: "",
  humanVerificationRegistrationEnabled: false,
  humanVerificationLoginEnabled: false,
  humanVerificationPasswordRecoveryEnabled: false,
  humanVerificationSecretConfigured: false,
  reason: "调整新账户准入规则以匹配当前运营要求。",
}

describe("registrationPolicySettingsFormSchema", () => {
  it("normalizes policy lists before sending the versioned update", () => {
    const result = registrationPolicySettingsFormSchema.safeParse(validInput)
    expect(result.success).toBe(true)
    if (result.success) {
      expect(result.data.reservedUsernames).toEqual(["admin", "root"])
      expect(result.data.emailDomains).toEqual(["trash.example"])
    }
  })

  it("requires a domain list and ordered session durations", () => {
    expect(
      registrationPolicySettingsFormSchema.safeParse({
        ...validInput,
        emailDomainMode: "allowlist",
        emailDomains: "",
      }).success
    ).toBe(false)
    expect(
      registrationPolicySettingsFormSchema.safeParse({
        ...validInput,
        sessionValidHours: 720,
        rememberSessionValidHours: 24,
      }).success
    ).toBe(false)
  })

  it("accepts a five-character reason and rejects a shorter one", () => {
    expect(
      registrationPolicySettingsFormSchema.safeParse({
        ...validInput,
        reason: "调整注册制",
      }).success
    ).toBe(true)
    expect(
      registrationPolicySettingsFormSchema.safeParse({
        ...validInput,
        reason: "调整注册",
      }).success
    ).toBe(false)
  })

  it("requires a deployed secret, public site key, and at least one protected flow", () => {
    expect(
      registrationPolicySettingsFormSchema.safeParse({
        ...validInput,
        humanVerificationProvider: "turnstile",
        humanVerificationSiteKey: "public-site-key",
        humanVerificationRegistrationEnabled: true,
      }).success
    ).toBe(false)

    expect(
      registrationPolicySettingsFormSchema.safeParse({
        ...validInput,
        humanVerificationProvider: "turnstile",
        humanVerificationSiteKey: "public-site-key",
        humanVerificationRegistrationEnabled: true,
        humanVerificationSecretConfigured: true,
      }).success
    ).toBe(true)

    expect(
      registrationPolicySettingsFormSchema.safeParse({
        ...validInput,
        humanVerificationProvider: "disabled",
        humanVerificationSiteKey: "stale-site-key",
      }).success
    ).toBe(false)
  })
})

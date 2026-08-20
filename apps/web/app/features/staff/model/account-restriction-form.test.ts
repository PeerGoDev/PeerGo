import { describe, expect, it } from "vitest"

import {
  accountRestrictionDurationLabel,
  accountRestrictionReasonLabel,
  accountRestrictionRevocationReasonLabel,
  createAccountRestrictionFormSchema,
  revokeAccountRestrictionFormSchema,
} from "~/features/staff/model/account-restriction-form"

describe("account restriction forms", () => {
  it("normalizes a bounded create command before review", () => {
    const result = createAccountRestrictionFormSchema.parse({
      reasonCode: "manual_review",
      durationHours: "72",
      reason: " 核对账户异常状态并等待人工复核结论。 ",
    })

    expect(result).toEqual({
      reasonCode: "manual_review",
      durationHours: 72,
      reason: "核对账户异常状态并等待人工复核结论。",
    })
    expect(accountRestrictionReasonLabel(result.reasonCode)).toBe("人工复核")
    expect(accountRestrictionDurationLabel(result.durationHours)).toBe("3 天")
  })

  it.each([
    ["unknown reason code", { reasonCode: "punishment" }],
    ["unbounded duration", { durationHours: "720" }],
    ["short reason", { reason: "理由过短" }],
  ])("rejects %s", (_name, override) => {
    const result = createAccountRestrictionFormSchema.safeParse({
      reasonCode: "security_incident",
      durationHours: "24",
      reason: "处置已确认的账户安全事件并保留复核窗口。",
      ...override,
    })

    expect(result.success).toBe(false)
  })

  it("normalizes an explicit revocation command", () => {
    const result = revokeAccountRestrictionFormSchema.parse({
      reasonCode: "review_completed",
      reason: " 复核工作已经完成，确认可以恢复账户访问。 ",
    })

    expect(result).toEqual({
      reasonCode: "review_completed",
      reason: "复核工作已经完成，确认可以恢复账户访问。",
    })
    expect(accountRestrictionRevocationReasonLabel(result.reasonCode)).toBe(
      "复核已完成"
    )
  })
})

import { z } from "zod"

import type { components } from "~/generated/api"

export type AccountRestrictionReasonCode =
  components["schemas"]["AccountRestrictionReasonCode"]
export type AccountRestrictionRevocationReasonCode =
  components["schemas"]["AccountRestrictionRevocationReasonCode"]

const humanReasonSchema = z
  .string()
  .trim()
  .refine((value) => runeLength(value) >= 10, "请填写至少 10 个字符的人工理由")
  .refine((value) => runeLength(value) <= 500, "人工理由不能超过 500 个字符")

export const createAccountRestrictionFormSchema = z.object({
  reasonCode: z.enum(["manual_review", "security_incident"]),
  durationHours: z
    .enum(["24", "72", "168"])
    .transform((value) => Number(value)),
  reason: humanReasonSchema,
})

export const revokeAccountRestrictionFormSchema = z.object({
  reasonCode: z.enum(["review_completed", "restriction_no_longer_needed"]),
  reason: humanReasonSchema,
})

export type CreateAccountRestrictionFormValues = z.output<
  typeof createAccountRestrictionFormSchema
>
export type CreateAccountRestrictionFormField = keyof z.input<
  typeof createAccountRestrictionFormSchema
>
export type RevokeAccountRestrictionFormValues = z.output<
  typeof revokeAccountRestrictionFormSchema
>
export type RevokeAccountRestrictionFormField = keyof z.input<
  typeof revokeAccountRestrictionFormSchema
>

export function accountRestrictionReasonLabel(
  code: AccountRestrictionReasonCode
) {
  return code === "manual_review" ? "人工复核" : "安全事件处置"
}

export function accountRestrictionRevocationReasonLabel(
  code: AccountRestrictionRevocationReasonCode
) {
  return code === "review_completed" ? "复核已完成" : "限制已无必要"
}

export function accountRestrictionDurationLabel(hours: number) {
  switch (hours) {
    case 24:
      return "24 小时"
    case 72:
      return "3 天"
    case 168:
      return "7 天"
    default:
      return `${hours} 小时`
  }
}

function runeLength(value: string) {
  return Array.from(value).length
}

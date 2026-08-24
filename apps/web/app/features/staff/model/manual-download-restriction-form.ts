import { z } from "zod"

import type { components } from "~/generated/api"

export type ManualDownloadRestrictionReasonCode =
  components["schemas"]["ManualDownloadRestrictionReasonCode"]
export type ManualDownloadRestrictionRevocationReasonCode =
  components["schemas"]["ManualDownloadRestrictionRevocationReasonCode"]

const administrationReasonSchema = z
  .string()
  .trim()
  .refine(
    (value) => Array.from(value).length <= 500,
    "人工理由不能超过 500 个字符"
  )

export const manualDownloadRestrictionFormSchema = z.object({
  reasonCode: z.enum(["manual_review", "policy_violation", "abuse_prevention"]),
  reason: administrationReasonSchema,
})

export const revokeManualDownloadRestrictionFormSchema = z.object({
  reasonCode: z.enum(["review_completed", "restriction_no_longer_needed"]),
  reason: administrationReasonSchema,
})

export type ManualDownloadRestrictionFormValues = z.output<
  typeof manualDownloadRestrictionFormSchema
>
export type RevokeManualDownloadRestrictionFormValues = z.output<
  typeof revokeManualDownloadRestrictionFormSchema
>

export function manualDownloadRestrictionReasonLabel(code: string) {
  switch (code) {
    case "manual_review":
      return "人工复核"
    case "policy_violation":
      return "站点规则处置"
    case "abuse_prevention":
      return "滥用风险控制"
    case "legacy_download_restriction":
      return "旧站迁入"
    case "appeal_approved":
      return "申诉批准"
    case "review_completed":
      return "复核已完成"
    case "restriction_no_longer_needed":
      return "限制已无必要"
    default:
      return "其他管理原因"
  }
}

export function manualDownloadRestrictionTransitionLabel(
  transition: "restricted" | "updated" | "revoked"
) {
  switch (transition) {
    case "restricted":
      return "签发"
    case "updated":
      return "修改"
    case "revoked":
      return "解除"
  }
}

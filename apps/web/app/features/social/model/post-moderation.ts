import type { components } from "~/generated/api"

type SocialPostReportReasonCode =
  components["schemas"]["SocialPostReportReasonCode"]
type SocialPostModerationDecisionReasonCode =
  components["schemas"]["SocialPostModerationDecisionReasonCode"]

export const postReportReasonOptions: Array<{
  value: SocialPostReportReasonCode
  label: string
}> = [
  { value: "spam", label: "广告或刷屏" },
  { value: "harassment", label: "骚扰或人身攻击" },
  { value: "personal_information", label: "泄露个人信息" },
  { value: "off_topic", label: "与板块主题无关" },
  { value: "other", label: "其他问题" },
]

export const postViolationReasonOptions: Array<{
  value: Exclude<SocialPostModerationDecisionReasonCode, "no_violation">
  label: string
}> = postReportReasonOptions

export function postReportReasonLabel(reason: SocialPostReportReasonCode) {
  return (
    postReportReasonOptions.find((option) => option.value === reason)?.label ??
    reason
  )
}

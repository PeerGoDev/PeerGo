import type { components } from "~/generated/api"

export type CommentReportReasonCode =
  components["schemas"]["CommentReportReasonCode"]
export type CommentModerationDecisionReasonCode =
  components["schemas"]["CommentModerationDecisionReasonCode"]

export const commentReportReasonOptions: Array<{
  value: CommentReportReasonCode
  label: string
}> = [
  { value: "spam", label: "广告或刷屏" },
  { value: "harassment", label: "骚扰或人身攻击" },
  { value: "personal_information", label: "泄露个人信息" },
  { value: "off_topic", label: "与资源讨论无关" },
  { value: "other", label: "其他问题" },
]

export const commentViolationReasonOptions: Array<{
  value: Exclude<CommentModerationDecisionReasonCode, "no_violation">
  label: string
}> = commentReportReasonOptions

export function commentReportReasonLabel(reason: CommentReportReasonCode) {
  return (
    commentReportReasonOptions.find((option) => option.value === reason)
      ?.label ?? reason
  )
}

import { z } from "zod"

import type {
  AnnouncementPublicationAction,
  ManagedAnnouncement,
} from "~/features/staff/api/announcement-administration.queries"

const stableAnnouncementIdPattern = /^[A-Za-z0-9][A-Za-z0-9._-]{0,119}$/

const changeReasonSchema = z
  .string()
  .trim()
  .refine((value) => runeLength(value) <= 500, "变更理由不能超过 500 个字符")

export const announcementDraftFormSchema = z.object({
  id: z
    .string()
    .trim()
    .regex(
      stableAnnouncementIdPattern,
      "仅可使用字母、数字、点、下划线和连字符，最长 120 位"
    ),
  title: boundedText("请输入公告标题", "公告标题不能超过 160 个字符", 160),
  summary: boundedText("请输入公告摘要", "公告摘要不能超过 500 个字符", 500),
  body: boundedText("请输入公告正文", "公告正文不能超过 20000 个字符", 20_000),
  bodyFormat: z.enum(["plain_text", "legacy_bbcode"]),
  reason: changeReasonSchema,
})

export const announcementPublicationFormSchema = z
  .object({
    action: z.enum(["publish_now", "schedule", "cancel_schedule", "withdraw"]),
    scheduledFor: z.string(),
    reason: changeReasonSchema,
  })
  .superRefine((value, context) => {
    if (value.action !== "schedule") {
      return
    }
    if (!value.scheduledFor) {
      context.addIssue({
        code: "custom",
        path: ["scheduledFor"],
        message: "请选择预约发布时间",
      })
      return
    }
    const timestamp = Date.parse(value.scheduledFor)
    if (!Number.isFinite(timestamp)) {
      context.addIssue({
        code: "custom",
        path: ["scheduledFor"],
        message: "预约发布时间无效",
      })
      return
    }
    const lead = timestamp - Date.now()
    if (lead < 60_000 || lead > 365 * 24 * 60 * 60 * 1000) {
      context.addIssue({
        code: "custom",
        path: ["scheduledFor"],
        message: "预约时间需在 1 分钟至 365 天之间",
      })
    }
  })

export type AnnouncementDraftFormValues = z.output<
  typeof announcementDraftFormSchema
>
export type AnnouncementDraftFormField = keyof z.input<
  typeof announcementDraftFormSchema
>
export type AnnouncementPublicationFormValues = z.output<
  typeof announcementPublicationFormSchema
>
export type AnnouncementPublicationFormField = keyof z.input<
  typeof announcementPublicationFormSchema
>

export function hasAnnouncementContentChanges(
  announcement: ManagedAnnouncement,
  values: AnnouncementDraftFormValues
) {
  return (
    announcement.title !== values.title ||
    announcement.summary !== values.summary ||
    announcement.body !== values.body ||
    announcement.body_format !== values.bodyFormat
  )
}

export function publicationActionLabel(action: AnnouncementPublicationAction) {
  switch (action) {
    case "publish_now":
      return "立即发布"
    case "schedule":
      return "预约发布"
    case "cancel_schedule":
      return "取消排期"
    case "withdraw":
      return "撤回公告"
  }
}

function boundedText(required: string, tooLong: string, maxRunes: number) {
  return z
    .string()
    .trim()
    .refine((value) => runeLength(value) >= 1, required)
    .refine((value) => runeLength(value) <= maxRunes, tooLong)
}

function runeLength(value: string) {
  return Array.from(value).length
}

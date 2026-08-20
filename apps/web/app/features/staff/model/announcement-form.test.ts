import { afterEach, describe, expect, it, vi } from "vitest"

import type { ManagedAnnouncement } from "~/features/staff/api/announcement-administration.queries"
import {
  announcementDraftFormSchema,
  announcementPublicationFormSchema,
  hasAnnouncementContentChanges,
} from "~/features/staff/model/announcement-form"

describe("announcement draft form", () => {
  it("normalizes bounded draft content", () => {
    const result = announcementDraftFormSchema.safeParse({
      id: " maintenance-2026-08 ",
      title: " 维护通知 ",
      summary: " 维护窗口说明 ",
      body: " 服务将在窗口内短暂停止。 ",
      bodyFormat: "plain_text",
      reason: " 建立草稿并等待值班人员复核。 ",
    })

    expect(result.success).toBe(true)
    if (result.success) {
      expect(result.data).toMatchObject({
        id: "maintenance-2026-08",
        title: "维护通知",
        summary: "维护窗口说明",
        body: "服务将在窗口内短暂停止。",
      })
    }
  })

  it("compares only immutable revision content", () => {
    const announcement = {
      id: "maintenance-2026-08",
      title: "维护通知",
      summary: "维护窗口说明",
      body: "服务将在窗口内短暂停止。",
      body_format: "plain_text",
    } as ManagedAnnouncement

    expect(
      hasAnnouncementContentChanges(announcement, {
        id: announcement.id,
        title: announcement.title,
        summary: announcement.summary,
        body: announcement.body,
        bodyFormat: announcement.body_format,
        reason: "重新保存但正文内容没有发生任何变化。",
      })
    ).toBe(false)
  })
})

describe("announcement publication form", () => {
  afterEach(() => vi.useRealTimers())

  it("requires a bounded future time only for scheduling", () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date("2026-08-10T10:00:00Z"))

    expect(
      announcementPublicationFormSchema.safeParse({
        action: "schedule",
        scheduledFor: "2026-08-10T10:00:30Z",
        reason: "排期时间过近，应当在前端先拒绝。",
      }).success
    ).toBe(false)
    expect(
      announcementPublicationFormSchema.safeParse({
        action: "schedule",
        scheduledFor: "2026-08-10T11:00:00Z",
        reason: "已完成内容复核并按照维护窗口排期。",
      }).success
    ).toBe(true)
    expect(
      announcementPublicationFormSchema.safeParse({
        action: "publish_now",
        scheduledFor: "",
        reason: "已完成内容复核并决定立即公开公告。",
      }).success
    ).toBe(true)
  })
})

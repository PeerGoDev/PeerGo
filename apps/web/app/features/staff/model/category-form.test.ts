import { describe, expect, it } from "vitest"

import {
  categoryFormSchema,
  hasCategoryBusinessChanges,
} from "~/features/staff/model/category-form"
import type { ManagedCategory } from "~/features/staff/api/category-administration.queries"

const category: ManagedCategory = {
  id: "documentary",
  name: "纪录片",
  display_order: 30,
  enabled: true,
  version: 4,
  torrent_count: 12,
  created_at: "2026-08-05T08:00:00Z",
  updated_at: "2026-08-05T09:00:00Z",
  facets: [],
}

describe("categoryFormSchema", () => {
  it("normalizes reviewed values into the generated API shape", () => {
    const result = categoryFormSchema.parse({
      id: " documentary ",
      name: " 纪录片 ",
      displayOrder: "30",
      enabled: true,
      reason: " 调整分类名称并确认公开内容影响。 ",
    })

    expect(result).toEqual({
      id: "documentary",
      name: "纪录片",
      displayOrder: 30,
      enabled: true,
      reason: "调整分类名称并确认公开内容影响。",
    })
    expect(hasCategoryBusinessChanges(category, result)).toBe(false)
  })

  it.each([
    ["uppercase id", { id: "Documentary" }],
    ["fractional order", { displayOrder: "3.5" }],
    ["short reason", { reason: "理由太短" }],
  ])("rejects %s before a mutation is opened", (_name, override) => {
    const result = categoryFormSchema.safeParse({
      id: "documentary",
      name: "纪录片",
      displayOrder: "30",
      enabled: true,
      reason: "调整分类名称并确认公开内容影响。",
      ...override,
    })

    expect(result.success).toBe(false)
  })

  it("detects only business-field changes, not the audit reason", () => {
    const result = categoryFormSchema.parse({
      id: category.id,
      name: category.name,
      displayOrder: String(category.display_order),
      enabled: false,
      reason: "停用当前分类并确认已有种子的公开影响。",
    })

    expect(hasCategoryBusinessChanges(category, result)).toBe(true)
  })
})

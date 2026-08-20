import { describe, expect, it } from "vitest"

import {
  torrentMetadataChanged,
  torrentResubmissionFormSchema,
} from "~/features/torrent/model/torrent-resubmission-form"

describe("torrent resubmission form", () => {
  it("normalizes the shared editable metadata and requires a useful note", () => {
    const result = torrentResubmissionFormSchema.parse({
      categoryId: " movies ",
      title: " Corrected release ",
      subtitle: " New subtitle ",
      correctionNote: " 已经按反馈补全发布资料。 ",
    })

    expect(result).toEqual({
      categoryId: "movies",
      title: "Corrected release",
      subtitle: "New subtitle",
      correctionNote: "已经按反馈补全发布资料。",
    })
  })

  it("distinguishes a correction note from an actual metadata change", () => {
    const before = {
      categoryId: "movies",
      title: "Release",
      subtitle: "First edition",
    }
    const parsed = torrentResubmissionFormSchema.parse({
      ...before,
      correctionNote: "只是填写说明但并未修改发布资料。",
    })

    expect(torrentMetadataChanged(before, parsed)).toBe(false)
    expect(
      torrentMetadataChanged(before, { ...parsed, subtitle: "Corrected" })
    ).toBe(true)
  })
})

import { describe, expect, it } from "vitest"

import { extractWikiHeadings } from "./wiki-markdown"

describe("extractWikiHeadings", () => {
  it("builds stable Chinese anchors and disambiguates duplicates", () => {
    expect(
      extractWikiHeadings(
        [
          "# 用户规范",
          "正文",
          "## 发种要求",
          "### 发种要求",
          "## 发种要求",
        ].join("\n")
      )
    ).toEqual([
      { id: "用户规范", text: "用户规范", level: 1 },
      { id: "发种要求", text: "发种要求", level: 2 },
      { id: "发种要求-1", text: "发种要求", level: 3 },
      { id: "发种要求-2", text: "发种要求", level: 2 },
    ])
  })

  it("ignores headings deeper than the visible table of contents", () => {
    expect(extractWikiHeadings("#### Hidden\nplain text")).toEqual([])
  })
})

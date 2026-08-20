import { describe, expect, it } from "vitest"

import { convertLegacyDescription } from "./legacy-description"

describe("convertLegacyDescription", () => {
  it("returns ordinary Markdown without changing it", () => {
    expect(convertLegacyDescription("## 已经是 Markdown")).toEqual({
      markdown: "## 已经是 Markdown",
      converted: false,
      removedImages: false,
    })
  })

  it("does not rewrite whitespace while a legacy pair is incomplete", () => {
    expect(convertLegacyDescription("  [b]正在输入")).toEqual({
      markdown: "  [b]正在输入",
      converted: false,
      removedImages: false,
    })
  })

  it("converts the text-oriented legacy tags", () => {
    const result = convertLegacyDescription(
      "[b]标题[/b][br][quote=Alice]第一行\n第二行[/quote][br][list][*]甲[*]乙[/list]"
    )

    expect(result).toEqual({
      markdown: "**标题**\n> **Alice：**\n> 第一行\n> 第二行\n- 甲\n- 乙",
      converted: true,
      removedImages: false,
    })
  })

  it("removes legacy image embeds and keeps safe links only", () => {
    const result = convertLegacyDescription(
      "[url=https://example.com/post][img]https://example.com/a.jpg[/img][/url][url=https://example.com]官网[/url] [url=javascript:alert(1)]危险[/url]"
    )

    expect(result.markdown).toBe("[官网](https://example.com) 危险")
    expect(result.converted).toBe(true)
    expect(result.removedImages).toBe(true)
  })
})

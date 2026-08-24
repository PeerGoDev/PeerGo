import { describe, expect, it } from "vitest"

import { formatSiteDocumentTitle } from "./site-document-title"

describe("formatSiteDocumentTitle", () => {
  it("uses the configured site name while preserving the page label", () => {
    expect(formatSiteDocumentTitle("邀请 · PeerGo", "RousiPro")).toBe(
      "邀请 · RousiPro"
    )
    expect(formatSiteDocumentTitle("PeerGo", "RousiPro")).toBe("RousiPro")
  })

  it("preserves staff title contexts", () => {
    expect(
      formatSiteDocumentTitle("用户 · 账户 · PeerGo Staff", "RousiPro")
    ).toBe("用户 · 账户 · RousiPro Staff")
    expect(
      formatSiteDocumentTitle("购买记录 · PeerGo 管理后台", "RousiPro")
    ).toBe("购买记录 · RousiPro 管理后台")
  })

  it("updates a title after the configured site name changes", () => {
    expect(
      formatSiteDocumentTitle("邀请 · RousiPro", "Rousi", "RousiPro")
    ).toBe("邀请 · Rousi")
  })

  it("does not rewrite unrelated document titles", () => {
    expect(formatSiteDocumentTitle("外部页面", "RousiPro")).toBe("外部页面")
  })
})

import { describe, expect, it } from "vitest"

import { resolveAppBreadcrumbs } from "~/features/shell/model/breadcrumbs"

describe("resolveAppBreadcrumbs", () => {
  it("shows only the current label on the home page", () => {
    expect(resolveAppBreadcrumbs("/")).toEqual([{ label: "首页" }])
  })

  it("links back home from top-level pages and ignores trailing slashes", () => {
    expect(resolveAppBreadcrumbs("/torrents/")).toEqual([
      { label: "首页", to: "/" },
      { label: "种子" },
    ])
  })

  it("names the sidebar group for collapsible account sections", () => {
    expect(resolveAppBreadcrumbs("/account/hnr")).toEqual([
      { label: "首页", to: "/" },
      { label: "账户与成长" },
      { label: "H&R" },
    ])
    expect(resolveAppBreadcrumbs("/account/security")).toEqual([
      { label: "首页", to: "/" },
      { label: "账户设置", to: "/account" },
      { label: "安全设置" },
    ])
  })

  it("links nested detail pages to their parent list", () => {
    expect(resolveAppBreadcrumbs("/torrents/42")).toEqual([
      { label: "首页", to: "/" },
      { label: "种子", to: "/torrents" },
      { label: "种子详情" },
    ])
    expect(resolveAppBreadcrumbs("/wiki/rules/edit")).toEqual([
      { label: "首页", to: "/" },
      { label: "Wiki", to: "/wiki" },
      { label: "编辑页面" },
    ])
  })

  it("falls back to the home crumb for unknown routes", () => {
    expect(resolveAppBreadcrumbs("/unknown/route")).toEqual([
      { label: "首页", to: "/" },
    ])
  })
})

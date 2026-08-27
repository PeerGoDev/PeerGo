import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { describe, expect, it, vi } from "vitest"

import { CategoryFacetManager } from "~/features/staff/components/category-facet-manager-sheet"
import { AppProviders } from "~/shared/providers/app-providers"

describe("CategoryFacetManager", () => {
  it("allows a category without attributes to create its first upload attribute", async () => {
    const user = userEvent.setup()
    renderManager([])

    expect(screen.getByText("这个分类还没有属性")).toBeVisible()
    await user.click(screen.getByRole("button", { name: "添加属性" }))

    expect(screen.getByRole("heading", { name: "添加分类属性" })).toBeVisible()
    expect(screen.getByLabelText("稳定标识")).toBeEnabled()
    expect(screen.getByLabelText("显示名称")).toBeEnabled()

    await user.click(screen.getByRole("button", { name: "保存" }))
    expect(
      screen.getByText(
        "属性稳定标识只能使用小写字母、数字与连字符，最长 64 位。"
      )
    ).toBeVisible()
  })

  it("shows disabled attributes with their historical option usage", () => {
    renderManager([
      {
        id: "resolution",
        name: "分辨率",
        selection_mode: "single_option",
        required: true,
        display_order: 10,
        enabled: false,
        version: 3,
        torrent_count: 12,
        created_at: "2026-08-01T00:00:00Z",
        updated_at: "2026-08-27T00:00:00Z",
        options: [
          {
            key: "2160p",
            label: "4K / 2160p",
            canonical_label: "4K / 2160p",
            display_order: 10,
            enabled: true,
            version: 2,
            torrent_count: 8,
            created_at: "2026-08-01T00:00:00Z",
            updated_at: "2026-08-27T00:00:00Z",
          },
        ],
      },
    ])

    expect(screen.getByText("分辨率")).toBeVisible()
    expect(screen.getByText("4K / 2160p")).toBeVisible()
    expect(
      screen.getByRole("button", { name: "编辑类型选项 4K / 2160p" })
    ).toHaveAttribute("title", "稳定值 2160p · 顺序 10 · 引用 8 个种子")
    expect(screen.getAllByText("停用")).toHaveLength(1)
    expect(screen.getByRole("button", { name: "编辑属性" })).toBeEnabled()
  })
})

function renderManager(
  facets: Parameters<typeof CategoryFacetManager>[0]["category"]["facets"]
) {
  return render(
    <AppProviders>
      <CategoryFacetManager
        category={{
          id: "movies",
          name: "电影",
          display_order: 10,
          enabled: true,
          version: 1,
          torrent_count: 100,
          created_at: "2026-08-01T00:00:00Z",
          updated_at: "2026-08-27T00:00:00Z",
          facets,
        }}
        csrfToken="test-csrf"
        canUpdate
        onSaved={vi.fn()}
      />
    </AppProviders>
  )
}

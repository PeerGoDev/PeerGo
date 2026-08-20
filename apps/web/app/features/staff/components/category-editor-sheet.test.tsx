import { render, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { describe, expect, it, vi } from "vitest"

import { CategoryEditorSheet } from "~/features/staff/components/category-editor-sheet"
import { AppProviders } from "~/shared/providers/app-providers"

describe("CategoryEditorSheet", () => {
  it("keeps invalid create input in the sheet and focuses the first field", async () => {
    const user = userEvent.setup()
    render(
      <AppProviders>
        <CategoryEditorSheet
          csrfToken="test-csrf"
          onOpenChange={vi.fn()}
          onSaved={vi.fn()}
        />
      </AppProviders>
    )

    await user.click(screen.getByRole("button", { name: "审阅变更" }))

    expect(
      screen.getByText("仅可使用小写字母、数字和连字符，最长 64 位")
    ).toBeInTheDocument()
    expect(screen.getByText("请输入分类名称")).toBeInTheDocument()
    expect(
      screen.getByText("请填写至少 10 个字符的变更理由")
    ).toBeInTheDocument()
    await waitFor(() => expect(screen.getByLabelText("分类标识")).toHaveFocus())
  })

  it("shows the reviewed create values before any API mutation", async () => {
    const user = userEvent.setup()
    render(
      <AppProviders>
        <CategoryEditorSheet
          csrfToken="test-csrf"
          onOpenChange={vi.fn()}
          onSaved={vi.fn()}
        />
      </AppProviders>
    )

    await user.type(screen.getByLabelText("分类标识"), "software")
    await user.type(screen.getByLabelText("显示名称"), "软件")
    await user.type(
      screen.getByLabelText("变更理由"),
      "增加软件分类并确认它的公开展示范围。"
    )
    await user.click(screen.getByRole("button", { name: "审阅变更" }))

    expect(
      screen.getByRole("heading", { name: "确认创建分类" })
    ).toBeInTheDocument()
    expect(screen.getByText("software")).toBeInTheDocument()
    expect(screen.getByText("软件")).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "确认创建" })).toBeEnabled()
  })
})

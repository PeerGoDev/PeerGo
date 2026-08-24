import { render, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { describe, expect, it, vi } from "vitest"

import { AnnouncementEditorSheet } from "~/features/staff/components/announcement-editor-sheet"
import { AppProviders } from "~/shared/providers/app-providers"

describe("AnnouncementEditorSheet", () => {
  it("keeps an invalid create draft local and focuses the route key", async () => {
    const user = userEvent.setup()
    renderEditor()

    await user.click(screen.getByRole("button", { name: "创建公告草稿" }))

    expect(
      screen.getByText("仅可使用字母、数字、点、下划线和连字符，最长 120 位")
    ).toBeInTheDocument()
    expect(screen.getByText("请输入公告标题")).toBeInTheDocument()
    expect(screen.queryByText(/变更理由.*至少 10/)).not.toBeInTheDocument()
    await waitFor(() =>
      expect(screen.getByLabelText("公开路由键")).toHaveFocus()
    )
  })

  it("previews legacy text without interpreting markup", async () => {
    const user = userEvent.setup()
    renderEditor()

    await user.type(screen.getByLabelText("标题"), "维护通知")
    await user.type(screen.getByLabelText("摘要"), "维护窗口说明")
    await user.type(
      screen.getByLabelText("正文"),
      "<strong>这段文本不能被执行</strong>"
    )
    await user.click(screen.getByRole("button", { name: "旧版 BBCode 文本" }))
    await user.click(screen.getByRole("button", { name: "上线预览" }))

    expect(
      screen.getByText("<strong>这段文本不能被执行</strong>")
    ).toBeInTheDocument()
    expect(screen.getByText("旧版格式")).toBeInTheDocument()
  })
})

function renderEditor() {
  return render(
    <AppProviders>
      <AnnouncementEditorSheet
        csrfToken="test-csrf"
        canUpdate
        onOpenChange={vi.fn()}
        onSaved={vi.fn()}
      />
    </AppProviders>
  )
}

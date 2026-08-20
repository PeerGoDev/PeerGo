import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"

import { Button } from "~/components/ui/button"
import { StaffPageHeader } from "~/features/staff/components/staff-page-header"

describe("StaffPageHeader", () => {
  it("keeps the compact heading, supporting copy, metadata, and actions together", () => {
    render(
      <StaffPageHeader
        title="分类管理"
        description="管理分类顺序与状态。"
        meta="10 条记录"
        actions={<Button size="sm">新建分类</Button>}
      />
    )

    expect(screen.getByRole("heading", { name: "分类管理" })).toHaveClass(
      "text-xl",
      "font-semibold"
    )
    expect(screen.getByText("管理分类顺序与状态。")).toBeVisible()
    expect(screen.getByText("10 条记录")).toBeVisible()
    expect(screen.getByRole("button", { name: "新建分类" })).toBeVisible()
  })
})

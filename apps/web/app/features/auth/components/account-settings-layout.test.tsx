import { render, screen } from "@testing-library/react"
import { MemoryRouter } from "react-router"
import { describe, expect, it } from "vitest"

import { AccountSettingsLayout } from "~/features/auth/components/account-settings-layout"

describe("AccountSettingsLayout", () => {
  it("keeps the PtYes settings rail and stacks it above content on narrow screens", () => {
    render(
      <MemoryRouter>
        <AccountSettingsLayout
          active="permissions"
          title="我的权限"
          description="当前权限"
        >
          <div>权限内容</div>
        </AccountSettingsLayout>
      </MemoryRouter>
    )

    const navigation = screen.getByRole("navigation", { name: "账户设置" })
    expect(screen.getByRole("main")).toHaveClass(
      "flex-row",
      "items-start",
      "gap-6",
      "max-md:flex-col"
    )
    expect(navigation).toHaveClass(
      "w-48",
      "shrink-0",
      "flex-col",
      "gap-1",
      "max-md:w-full"
    )
    expect(navigation).not.toHaveClass("max-sm:flex-row")
    expect(screen.getByRole("link", { name: "我的权限" })).toHaveClass(
      "w-full",
      "font-normal"
    )
  })
})

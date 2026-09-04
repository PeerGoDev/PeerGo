import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"

import { PageHeader, PageLayout } from "~/shared/components/page-layout"

describe("PageLayout", () => {
  it("reserves the same 1200px content rail plus desktop page padding as Rousi", () => {
    render(<PageLayout>页面内容</PageLayout>)

    expect(screen.getByRole("main")).toHaveClass(
      "min-w-0",
      "max-w-[1248px]",
      "p-4",
      "lg:p-6"
    )
  })

  it("renders the title in the glass subheader strip", () => {
    render(<PageHeader title="页面标题" />)

    expect(screen.getByRole("heading", { name: "页面标题" })).toHaveClass(
      "font-heading",
      "text-[15px]",
      "font-semibold"
    )
  })
})

import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"

import { PageHeader, PageLayout } from "~/shared/components/page-layout"

describe("PageLayout", () => {
  it("centres a 1120px content rail with responsive page gutters", () => {
    render(<PageLayout>页面内容</PageLayout>)

    expect(screen.getByRole("main")).toHaveClass(
      "min-w-0",
      "max-w-[1120px]",
      "px-4",
      "sm:px-6",
      "lg:px-8"
    )
  })

  it("renders a plain Direction H page title with supporting copy", () => {
    render(<PageHeader title="页面标题" description="页面说明" />)

    expect(screen.getByRole("heading", { name: "页面标题" })).toHaveClass(
      "font-heading",
      "text-lg",
      "font-semibold",
      "tracking-title"
    )
    expect(screen.getByText("页面说明")).toHaveClass(
      "text-xs",
      "text-muted-foreground"
    )
  })
})

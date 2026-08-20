import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"

import { PageLayout } from "~/shared/components/page-layout"

describe("PageLayout", () => {
  it("reserves the same 1200px content rail plus desktop page padding as Rousi", () => {
    render(<PageLayout>页面内容</PageLayout>)

    expect(screen.getByRole("main")).toHaveClass("max-w-[1248px]", "lg:p-6")
  })
})

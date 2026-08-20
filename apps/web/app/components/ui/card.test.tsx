import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"

import { Card } from "~/components/ui/card"

describe("Card", () => {
  it("keeps the PtYes reading size while preserving the compact variant", () => {
    const { rerender } = render(<Card>默认卡片</Card>)

    expect(screen.getByText("默认卡片")).toHaveClass("text-base")

    rerender(<Card size="sm">紧凑卡片</Card>)

    expect(screen.getByText("紧凑卡片")).toHaveClass(
      "text-base",
      "data-[size=sm]:text-sm"
    )
  })
})

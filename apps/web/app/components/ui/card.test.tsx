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

  it("uses the Direction I borderless soft-shadow shell", () => {
    render(<Card>方向 I 卡片</Card>)

    expect(screen.getByText("方向 I 卡片")).toHaveClass(
      "rounded-3xl",
      "border-0",
      "shadow-soft"
    )
    expect(screen.getByText("方向 I 卡片")).not.toHaveClass("shadow-sm")
  })
})

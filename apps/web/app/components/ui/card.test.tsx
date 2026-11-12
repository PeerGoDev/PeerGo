import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"

import { Card } from "~/components/ui/card"

describe("Card", () => {
  it("reads at the compact Direction H size and tightens spacing on phones", () => {
    const { rerender } = render(<Card>默认卡片</Card>)

    expect(screen.getByText("默认卡片")).toHaveClass(
      "text-sm",
      "[--card-spacing:--spacing(4)]",
      "sm:[--card-spacing:--spacing(6)]"
    )

    rerender(<Card size="sm">紧凑卡片</Card>)

    expect(screen.getByText("紧凑卡片")).toHaveClass(
      "data-[size=sm]:[--card-spacing:--spacing(4)]"
    )
  })

  it("uses the Direction H bordered, near-flat shell", () => {
    render(<Card>方向 H 卡片</Card>)

    expect(screen.getByText("方向 H 卡片")).toHaveClass(
      "rounded-xl",
      "border",
      "bg-card",
      "shadow-card"
    )
    expect(screen.getByText("方向 H 卡片")).not.toHaveClass("shadow-soft")
  })
})

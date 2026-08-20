import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"

import { HydrateFallback } from "~/root"

describe("HydrateFallback", () => {
  it("keeps route-module loading visible instead of showing a blank page", () => {
    render(<HydrateFallback />)

    expect(screen.getByRole("status", { name: "加载中" })).toBeVisible()
    expect(screen.getByText("页面加载中")).toBeVisible()
  })
})

import { render, screen } from "@testing-library/react"
import { MemoryRouter, Route, Routes } from "react-router"
import { describe, expect, it } from "vitest"

import CatchAllRoute from "~/routes/catch-all"

describe("catch-all route", () => {
  it("returns visitors to the site home instead of dropping the app shell", async () => {
    render(
      <MemoryRouter initialEntries={["/missing-page"]}>
        <Routes>
          <Route path="/" element={<h1>首页</h1>} />
          <Route path="*" element={<CatchAllRoute />} />
        </Routes>
      </MemoryRouter>
    )

    expect(await screen.findByRole("heading", { name: "首页" })).toBeVisible()
  })
})

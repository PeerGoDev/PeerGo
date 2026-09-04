import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { afterEach, describe, expect, it } from "vitest"

import { AuthShell } from "~/features/auth/components/auth-shell"

afterEach(() => {
  window.localStorage.clear()
  document.documentElement.classList.remove("dark")
  document.documentElement.style.colorScheme = ""
})

describe("AuthShell", () => {
  it("renders children without the app sidebar or header chrome", () => {
    window.localStorage.setItem("peergo-theme", "light")
    render(
      <AuthShell>
        <main>auth content</main>
      </AuthShell>
    )

    expect(screen.getByText("auth content")).toBeVisible()
    expect(screen.queryByRole("navigation")).not.toBeInTheDocument()
    expect(screen.queryByRole("banner")).not.toBeInTheDocument()
  })

  it("toggles the theme from the corner button", async () => {
    window.localStorage.setItem("peergo-theme", "light")
    const user = userEvent.setup()
    render(<AuthShell>content</AuthShell>)

    await user.click(screen.getByRole("button", { name: "切换到深色模式" }))
    expect(document.documentElement).toHaveClass("dark")
    expect(window.localStorage.getItem("peergo-theme")).toBe("dark")

    await user.click(screen.getByRole("button", { name: "切换到浅色模式" }))
    expect(document.documentElement).not.toHaveClass("dark")
    expect(window.localStorage.getItem("peergo-theme")).toBe("light")
  })
})

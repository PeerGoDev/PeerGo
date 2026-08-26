import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter } from "react-router"
import { afterEach, describe, expect, it, vi } from "vitest"

import { ResetPasswordPage } from "~/features/auth/pages/reset-password-page"

function renderPage(entry: string) {
  const queryClient = new QueryClient({
    defaultOptions: { mutations: { retry: false } },
  })
  return render(
    <MemoryRouter initialEntries={[entry]}>
      <QueryClientProvider client={queryClient}>
        <ResetPasswordPage />
      </QueryClientProvider>
    </MemoryRouter>
  )
}

afterEach(() => vi.restoreAllMocks())

describe("ResetPasswordPage", () => {
  it("reads a fragment credential and removes it from the visible URL", async () => {
    const historyState = globalThis.history.state
    const replaceState = vi.spyOn(globalThis.history, "replaceState")

    renderPage(`/reset-password#token=${"A".repeat(43)}`)

    expect(screen.getByLabelText("新密码")).toBeVisible()
    await waitFor(() =>
      expect(replaceState).toHaveBeenCalledWith(
        historyState,
        "",
        "/reset-password"
      )
    )
  })

  it("does not accept a recovery credential from the query string", () => {
    renderPage(`/reset-password?token=${"A".repeat(43)}`)

    const title = screen.getByText("链接无效或已过期")
    expect(title).toBeVisible()
    expect(title.closest("main")).toHaveClass(
      "flex",
      "min-h-svh",
      "items-center",
      "justify-center"
    )
    expect(title.closest("[data-slot='card']")).toHaveClass(
      "rounded-3xl",
      "border-0",
      "shadow-soft"
    )
    expect(title.closest("[data-slot='card-content']")).toHaveClass("px-6")
    expect(
      screen.queryByRole("button", { name: "重置密码" })
    ).not.toBeInTheDocument()
  })

  it("keeps mismatched passwords local and focuses confirmation", async () => {
    const user = userEvent.setup()
    renderPage(`/reset-password#token=${"A".repeat(43)}`)

    await user.type(
      screen.getByLabelText("新密码"),
      "PeerGo-new-password-2026!"
    )
    await user.type(
      screen.getByLabelText("确认密码"),
      "PeerGo-other-password-2026!"
    )
    await user.click(screen.getByRole("button", { name: "重置密码" }))

    expect(screen.getByText("两次输入的密码不一致")).toBeVisible()
    expect(screen.getByLabelText("确认密码")).toHaveFocus()
  })
})

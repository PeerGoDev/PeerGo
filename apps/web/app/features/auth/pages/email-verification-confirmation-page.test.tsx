import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen, waitFor } from "@testing-library/react"
import { MemoryRouter } from "react-router"
import { afterEach, describe, expect, it, vi } from "vitest"

import { EmailVerificationConfirmationPage } from "~/features/auth/pages/email-verification-confirmation-page"

function renderPage(entry: string) {
  const queryClient = new QueryClient({
    defaultOptions: { mutations: { retry: false } },
  })
  return render(
    <MemoryRouter initialEntries={[entry]}>
      <QueryClientProvider client={queryClient}>
        <EmailVerificationConfirmationPage />
      </QueryClientProvider>
    </MemoryRouter>
  )
}

describe("EmailVerificationConfirmationPage", () => {
  afterEach(() => vi.restoreAllMocks())

  it("requires confirmation and removes the fragment from the visible URL", async () => {
    const historyState = globalThis.history.state
    const replaceState = vi.spyOn(globalThis.history, "replaceState")
    renderPage(`/verify-email#token=${"A".repeat(43)}`)

    expect(screen.getByRole("heading", { name: "确认邮箱" })).toBeVisible()
    expect(screen.getByRole("button", { name: "确认验证" })).toBeVisible()
    await waitFor(() =>
      expect(replaceState).toHaveBeenCalledWith(
        historyState,
        "",
        "/verify-email"
      )
    )
  })

  it("does not accept a token from the request query string", () => {
    renderPage(`/verify-email?token=${"A".repeat(43)}`)

    const message = screen.getByText("缺少验证令牌")
    expect(message).toBeVisible()
    expect(message.closest("main")).toHaveClass("mt-4", "min-h-svh", "lg:mt-6")
    const card = message.closest('[data-slot="card"]')
    expect(card).toHaveClass("gap-0", "py-0")
    expect(card?.querySelector('[data-slot="card-header"]')).toHaveClass("p-6")
    expect(card?.querySelector('[data-slot="card-content"]')).toHaveClass(
      "pb-6"
    )
    expect(
      screen.queryByRole("button", { name: "确认验证" })
    ).not.toBeInTheDocument()
  })
})

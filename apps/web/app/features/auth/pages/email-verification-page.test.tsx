import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter } from "react-router"
import { describe, expect, it } from "vitest"

import {
  sessionKeys,
  type WebSession,
} from "~/features/auth/api/session.mutations"
import { EmailVerificationPage } from "~/features/auth/pages/email-verification-page"

const session: WebSession = {
  user: {
    id: "0198f20a-6da8-7e51-9c64-111111111111",
    username: "member",
    display_name: "测试成员",
    email_verified: false,
  },
  expires_at: "2026-08-07T00:00:00Z",
  csrf_token: "c".repeat(43),
}

function renderPage(activeSession: WebSession | null) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  })
  queryClient.setQueryData(sessionKeys.current(), activeSession)

  return render(
    <MemoryRouter>
      <QueryClientProvider client={queryClient}>
        <EmailVerificationPage />
      </QueryClientProvider>
    </MemoryRouter>
  )
}

describe("EmailVerificationPage", () => {
  it("validates and focuses the address locally before making a request", async () => {
    const user = userEvent.setup()
    renderPage(session)

    expect(screen.getByRole("link", { name: "邮箱验证" })).toHaveAttribute(
      "aria-current",
      "page"
    )

    await user.click(screen.getByRole("button", { name: "发送验证邮件" }))

    expect(screen.getByText("请输入有效的邮箱地址")).toBeVisible()
    expect(screen.getByLabelText("注册邮箱")).toHaveFocus()
  })

  it("does not offer another verification request to a verified account", () => {
    renderPage({
      ...session,
      user: { ...session.user, email_verified: true },
    })

    expect(screen.getByText("邮箱已验证")).toBeVisible()
    expect(
      screen.getByText("邮箱已验证").closest("[data-slot='card']")
    ).toHaveClass("gap-0", "py-0", "border-success/30")
    expect(
      screen.queryByRole("button", { name: "发送验证邮件" })
    ).not.toBeInTheDocument()
  })
})

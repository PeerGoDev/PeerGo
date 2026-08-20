import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter } from "react-router"
import { describe, expect, it } from "vitest"

import { ForgotPasswordPage } from "~/features/auth/pages/forgot-password-page"
import { siteKeys } from "~/features/site/api/site.queries"

function renderPage() {
  const queryClient = new QueryClient({
    defaultOptions: { mutations: { retry: false } },
  })
  queryClient.setQueryData(siteKeys.info(), siteInfo)
  return render(
    <MemoryRouter>
      <QueryClientProvider client={queryClient}>
        <ForgotPasswordPage />
      </QueryClientProvider>
    </MemoryRouter>
  )
}

describe("ForgotPasswordPage", () => {
  it("validates and focuses the email before making a request", async () => {
    const user = userEvent.setup()
    renderPage()

    await user.click(screen.getByRole("button", { name: "发送重置邮件" }))

    expect(screen.getByText("请输入有效的邮箱地址")).toBeVisible()
    expect(screen.getByLabelText("邮箱地址")).toHaveFocus()
  })
})

const siteInfo = {
  name: "PeerGo",
  description: "测试站点",
  registration_mode: "open" as const,
  registration_username_min_characters: 3,
  registration_username_max_characters: 20,
  registration_email_domain_mode: "any" as const,
  human_verification: {
    provider: "disabled" as const,
    site_key: "",
    registration_enabled: false,
    login_enabled: false,
    password_recovery_enabled: false,
  },
  online_users: 1,
  default_torrent_view: "list" as const,
  show_latest_announcement: true,
}

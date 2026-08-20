import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter, Route, Routes } from "react-router"
import { afterEach, describe, expect, it, vi } from "vitest"

import {
  sessionKeys,
  type WebSession,
} from "~/features/auth/api/session.mutations"
import { LoginPage } from "~/features/auth/pages/login-page"
import { siteKeys } from "~/features/site/api/site.queries"

describe("LoginPage", () => {
  afterEach(() => vi.unstubAllGlobals())

  it("shows accessible field errors without submitting credentials", async () => {
    const user = userEvent.setup()
    renderLogin()

    expect(screen.getByRole("checkbox", { name: /记住登录/ })).toHaveClass(
      "border-foreground/50",
      "bg-native-control"
    )
    await user.click(screen.getByRole("button", { name: "登录" }))

    expect(screen.getByText("请输入用户名或邮箱")).toBeInTheDocument()
    expect(screen.getByText("请输入密码")).toBeInTheDocument()
    expect(screen.getByLabelText("用户名 / 邮箱")).toHaveFocus()
  })

  it("keeps the optional second-factor field visible and focuses it after a challenge", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(
          JSON.stringify({
            type: "about:blank",
            title: "需要两步验证码",
            status: 428,
            code: "second_factor_required",
            request_id: "test-request",
          }),
          {
            status: 428,
            headers: { "Content-Type": "application/problem+json" },
          }
        )
      )
    )
    const user = userEvent.setup()
    renderLogin()

    expect(screen.getByLabelText("两步验证码（可选）")).toBeVisible()
    await user.type(screen.getByLabelText("用户名 / 邮箱"), "demo")
    await user.type(screen.getByLabelText("密码"), "correct-password")
    await user.click(screen.getByRole("button", { name: "登录" }))

    expect(await screen.findByText("还需要第二因素")).toBeVisible()
    expect(screen.getByLabelText("两步验证码（可选）")).toHaveFocus()
  })

  it("returns an authenticated member to the content home", async () => {
    renderLogin(activeSession)

    expect(
      await screen.findByRole("heading", { name: "内容首页" })
    ).toBeVisible()
    expect(
      screen.queryByRole("heading", { name: "登录" })
    ).not.toBeInTheDocument()
  })
})

const activeSession: WebSession = {
  user: {
    id: "0198f20a-6da8-7e51-9c64-111111111111",
    username: "demo",
    display_name: "PeerGo 演示用户",
    email_verified: true,
  },
  expires_at: "2026-09-05T12:00:00Z",
  csrf_token: "c".repeat(43),
}

function renderLogin(session: WebSession | null = null) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  })
  queryClient.setQueryData(sessionKeys.current(), session)
  queryClient.setQueryData(siteKeys.info(), siteInfo)

  return render(
    <MemoryRouter initialEntries={["/login"]}>
      <QueryClientProvider client={queryClient}>
        <Routes>
          <Route path="/login" element={<LoginPage />} />
          <Route path="/" element={<h1>内容首页</h1>} />
        </Routes>
      </QueryClientProvider>
    </MemoryRouter>
  )
}

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

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter, Route, Routes, useLocation } from "react-router"
import { afterEach, describe, expect, it, vi } from "vitest"

import {
  sessionKeys,
  type WebSession,
} from "~/features/auth/api/session.mutations"
import { RegistrationPage } from "~/features/auth/pages/registration-page"
import { siteKeys } from "~/features/site/api/site.queries"

function renderWithMode(
  mode: "open" | "invite" | "closed",
  session: WebSession | null = null,
  initialEntry = "/register"
) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  })
  queryClient.setQueryData(siteKeys.info(), {
    name: "PeerGo",
    description: "测试站点",
    registration_mode: mode,
    registration_username_min_characters: 3,
    registration_username_max_characters: 20,
    registration_email_domain_mode: "any",
    human_verification: disabledHumanVerification,
    online_users: 1,
    default_torrent_view: "list",
    show_latest_announcement: true,
  })
  queryClient.setQueryData(sessionKeys.current(), session)
  return render(
    <MemoryRouter initialEntries={[initialEntry]}>
      <QueryClientProvider client={queryClient}>
        <Routes>
          <Route
            path="/register"
            element={
              <>
                <RegistrationPage />
                <LocationProbe />
              </>
            }
          />
          <Route path="/" element={<h1>内容首页</h1>} />
        </Routes>
      </QueryClientProvider>
    </MemoryRouter>
  )
}

const disabledHumanVerification = {
  provider: "disabled" as const,
  site_key: "",
  registration_enabled: false,
  login_enabled: false,
  password_recovery_enabled: false,
}

describe("RegistrationPage", () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it("shows the invite-only field and focuses the first invalid control", async () => {
    const user = userEvent.setup()
    renderWithMode("invite")

    expect(screen.getByLabelText("邀请凭证")).toBeInTheDocument()
    expect(screen.getByRole("heading", { name: "邀请注册" })).toBeVisible()
    expect(document.querySelector('[data-slot="card-header"]')).toHaveClass(
      "justify-items-center",
      "gap-1.5",
      "pt-2",
      "text-center"
    )
    expect(document.querySelector(".lucide-gift")?.parentElement).toHaveClass(
      "mx-auto",
      "mb-4"
    )
    expect(document.querySelector('[data-slot="card-content"]')).toHaveClass(
      "pt-2"
    )
    expect(document.querySelector('[data-slot="card-footer"]')).toHaveClass(
      "-mt-4",
      "pb-6"
    )
    expect(
      screen.queryByRole("button", { name: "注册" })
    ).not.toBeInTheDocument()
    await user.type(screen.getByLabelText("邀请凭证"), "invalid")
    await user.click(screen.getByRole("button", { name: "验证邀请凭证" }))

    expect(screen.getByText("请输入有效的邀请码")).toBeVisible()
    expect(screen.getByLabelText("邀请凭证")).toHaveFocus()

    await user.clear(screen.getByLabelText("邀请凭证"))
    await user.type(screen.getByLabelText("邀请凭证"), "i".repeat(43))
    await user.click(screen.getByRole("button", { name: "验证邀请凭证" }))

    expect(screen.getByText("邀请凭证已填写")).toBeVisible()
    expect(screen.getByRole("button", { name: "更换邀请凭证" })).toBeVisible()
    await user.click(screen.getByRole("button", { name: "注册" }))

    expect(screen.getByText("用户名至少需要 3 个字符")).toBeInTheDocument()
    expect(screen.getByLabelText("用户名")).toHaveFocus()
  })

  it("fails closed when registration is disabled", () => {
    renderWithMode("closed")
    expect(screen.getByRole("heading", { name: "注册已关闭" })).toBeVisible()
    expect(screen.getByText("PeerGo 目前暂不开放注册")).toBeVisible()
    expect(screen.getByRole("link", { name: "登录" })).toHaveAttribute(
      "href",
      "/login"
    )
    expect(
      screen.queryByRole("button", { name: "注册" })
    ).not.toBeInTheDocument()
  })

  it("keeps an optional invitation relationship during open registration", async () => {
    const user = userEvent.setup()
    const token = "i".repeat(43)
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(
        JSON.stringify({
          user: {
            id: "0198f20a-6da8-7e51-9c64-222222222222",
            username: "open_member",
            display_name: "开放注册成员",
            email_verified: false,
          },
          admission_mode: "invite",
          email_verification_required: true,
          completed_at: "2026-08-17T13:00:00Z",
        }),
        { status: 201, headers: { "Content-Type": "application/json" } }
      )
    )
    vi.stubGlobal("fetch", fetchMock)

    renderWithMode("open", null, `/register?invite=${token}`)

    expect(await screen.findByText("已识别邀请链接")).toBeVisible()
    await waitFor(() => {
      expect(screen.getByTestId("registration-location")).toHaveTextContent(
        /^\/register$/
      )
    })
    await user.type(screen.getByLabelText("用户名"), "open_member")
    await user.type(screen.getByLabelText("显示名称"), "开放注册成员")
    await user.type(screen.getByLabelText("邮箱"), "open@example.com")
    await user.type(
      screen.getByLabelText("密码", { exact: true }),
      "PeerGo-open-2026!"
    )
    await user.type(screen.getByLabelText("确认密码"), "PeerGo-open-2026!")
    await user.click(screen.getByRole("button", { name: "注册" }))

    expect(await screen.findByText("账户已经创建")).toBeVisible()
    const request = fetchMock.mock.calls[0]?.[0] as Request
    await expect(request.clone().json()).resolves.toMatchObject({
      username: "open_member",
      invitation_token: token,
    })
  })

  it("returns an authenticated member to the content home", async () => {
    renderWithMode("invite", activeSession)

    expect(
      await screen.findByRole("heading", { name: "内容首页" })
    ).toBeVisible()
    expect(
      screen.queryByRole("heading", { name: "注册" })
    ).not.toBeInTheDocument()
  })
})

function LocationProbe() {
  const location = useLocation()
  return (
    <span data-testid="registration-location">
      {location.pathname}
      {location.search}
    </span>
  )
}

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

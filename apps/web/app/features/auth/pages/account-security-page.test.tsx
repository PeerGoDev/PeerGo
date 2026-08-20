import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter } from "react-router"
import { afterEach, describe, expect, it, vi } from "vitest"

import {
  sessionSecurityKeys,
  type AccountSecurityOverview,
  type UserWebSessionList,
} from "~/features/auth/api/session-security.queries"
import { sessionKeys } from "~/features/auth/api/session.mutations"
import { AccountSecurityPage } from "~/features/auth/pages/account-security-page"

const userId = "0198f20a-6da8-7e51-9c64-111111111111"

describe("AccountSecurityPage", () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it("renders only the redacted account and session projections", async () => {
    const user = userEvent.setup()
    const queryClient = accountSecurityQueryClient()

    render(
      <MemoryRouter initialEntries={["/account/security"]}>
        <QueryClientProvider client={queryClient}>
          <AccountSecurityPage />
        </QueryClientProvider>
      </MemoryRouter>
    )

    expect(screen.getByRole("heading", { name: "账户安全" })).toBeVisible()
    expect(screen.getByRole("link", { name: "我的权限" })).toHaveAttribute(
      "href",
      "/account/permissions"
    )
    expect(screen.getByRole("link", { name: "安全设置" })).toHaveAttribute(
      "aria-current",
      "page"
    )
    expect(screen.getByRole("main").children[1]).toHaveClass("gap-6")
    expect(screen.getByRole("link", { name: "修改密码" })).toHaveAttribute(
      "href",
      "/forgot-password"
    )
    expect(screen.getByRole("link", { name: "修改密码" })).toHaveClass(
      "w-[88px]",
      "self-start"
    )
    expect(screen.getByText("两步验证")).toBeVisible()
    expect(screen.getByRole("button", { name: "启用两步验证" })).toBeVisible()
    expect(screen.getByText("当前浏览器")).toBeVisible()
    expect(screen.getByText("浏览器会话")).toBeVisible()
    expect(screen.getByRole("columnheader", { name: "最近活动" })).toHaveClass(
      "hidden",
      "lg:table-cell"
    )
    expect(screen.getByRole("columnheader", { name: "有效至" })).toHaveClass(
      "hidden",
      "xl:table-cell"
    )
    expect(screen.queryByText("203.0.113.7")).not.toBeInTheDocument()
    expect(screen.queryByText("Mozilla/5.0")).not.toBeInTheDocument()
    expect(
      screen.queryByText("0198f20a-6da8-7e51-9c64-222222222222")
    ).not.toBeInTheDocument()
    expect(document.body.textContent).not.toMatch(/Web 会话|后台子会话/)

    await user.click(screen.getByRole("button", { name: "撤销其他会话" }))
    expect(
      screen.getByRole("heading", { name: "撤销其他 1 个会话？" })
    ).toBeVisible()
    expect(screen.getByRole("button", { name: "全部撤销" })).toBeVisible()
  })

  it("starts TOTP setup with a password reauthentication dialog", async () => {
    const user = userEvent.setup()
    const queryClient = accountSecurityQueryClient()
    render(
      <MemoryRouter initialEntries={["/account/security"]}>
        <QueryClientProvider client={queryClient}>
          <AccountSecurityPage />
        </QueryClientProvider>
      </MemoryRouter>
    )

    await user.click(screen.getByRole("button", { name: "启用两步验证" }))
    expect(
      screen.getByRole("heading", { name: "启用验证器动态码" })
    ).toBeVisible()
    expect(screen.getByLabelText("当前密码")).toHaveAttribute(
      "autocomplete",
      "current-password"
    )
  })

  it("keeps one-time recovery codes visible until the user acknowledges them", async () => {
    const user = userEvent.setup()
    const queryClient = accountSecurityQueryClient()
    queryClient.setDefaultOptions({
      queries: { retry: false, staleTime: Number.POSITIVE_INFINITY },
    })
    const recoveryCodes = Array.from(
      { length: 10 },
      (_, index) => `TEST-CODE-${String(index).padStart(4, "0")}`
    )
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        jsonResponse({
          enrollment_id: "0198f20a-6da8-7e51-9c64-444444444444",
          secret: "JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXP",
          provisioning_uri:
            "otpauth://totp/PeerGo%3Ademo?secret=JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXP&issuer=PeerGo",
          expires_at: "2026-08-07T14:30:00Z",
        })
      )
      .mockResolvedValueOnce(
        jsonResponse({
          change_id: "0198f20a-6da8-7e51-9c64-555555555555",
          enabled_at: "2026-08-07T14:00:00Z",
          recovery_codes: recoveryCodes,
        })
      )
      // This response reproduces the status flip that used to unmount the
      // dialog when confirmation invalidated the overview immediately.
      .mockResolvedValueOnce(
        jsonResponse({
          email_verified: true,
          password_changed_at: "2026-08-06T08:00:00Z",
          two_factor: {
            enabled: true,
            enabled_at: "2026-08-07T14:00:00Z",
            recovery_codes_remaining: 10,
          },
        })
      )
    vi.stubGlobal("fetch", fetchMock)

    render(
      <MemoryRouter initialEntries={["/account/security"]}>
        <QueryClientProvider client={queryClient}>
          <AccountSecurityPage />
        </QueryClientProvider>
      </MemoryRouter>
    )

    await user.click(screen.getByRole("button", { name: "启用两步验证" }))
    await user.type(screen.getByLabelText("当前密码"), "current-password")
    await user.click(screen.getByRole("button", { name: "继续" }))
    await user.type(await screen.findByLabelText("验证器动态码"), "123456")
    await user.click(screen.getByRole("button", { name: "验证并启用" }))

    expect(await screen.findByText("立即保存这组恢复码")).toBeVisible()
    expect(screen.getByLabelText("一次性恢复码")).toHaveTextContent(
      recoveryCodes[0]
    )
    expect(screen.getByRole("button", { name: "我已安全保存" })).toBeVisible()
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })

  it("does not request or render security data without a Web session", () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    queryClient.setQueryData(sessionKeys.current(), null)

    render(
      <MemoryRouter initialEntries={["/account/security"]}>
        <QueryClientProvider client={queryClient}>
          <AccountSecurityPage />
        </QueryClientProvider>
      </MemoryRouter>
    )

    expect(screen.getByText("需要登录")).toBeVisible()
    expect(screen.getByRole("link", { name: "前往登录" })).toHaveAttribute(
      "href",
      "/login"
    )
  })
})

function accountSecurityQueryClient() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  queryClient.setQueryData(sessionKeys.current(), {
    user: {
      id: userId,
      username: "demo",
      display_name: "PeerGo 演示用户",
      email_verified: true,
    },
    expires_at: "2026-09-05T12:00:00Z",
    csrf_token: "c".repeat(43),
  })
  const overview: AccountSecurityOverview = {
    email_verified: true,
    password_changed_at: "2026-08-06T08:00:00Z",
    two_factor: {
      enabled: false,
      enabled_at: null,
      recovery_codes_remaining: 0,
    },
  }
  queryClient.setQueryData(sessionSecurityKeys.overview(userId), overview)
  const sessions: UserWebSessionList = {
    items: [
      {
        id: "0198f20a-6da8-7e51-9c64-222222222222",
        current: true,
        created_at: "2026-08-06T08:00:00Z",
        last_seen_at: "2026-08-06T09:00:00Z",
        expires_at: "2026-09-05T08:00:00Z",
      },
      {
        id: "0198f20a-6da8-7e51-9c64-333333333333",
        current: false,
        created_at: "2026-08-05T08:00:00Z",
        last_seen_at: "2026-08-05T09:00:00Z",
        expires_at: "2026-08-17T08:00:00Z",
      },
    ],
  }
  queryClient.setQueryData(sessionSecurityKeys.sessions(userId), sessions)
  return queryClient
}

function jsonResponse(body: unknown) {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { "Content-Type": "application/json" },
  })
}

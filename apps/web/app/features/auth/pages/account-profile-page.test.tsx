import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter } from "react-router"
import { afterEach, describe, expect, it, vi } from "vitest"

import { sessionKeys } from "~/features/auth/api/session.mutations"
import { AccountProfilePage } from "~/features/auth/pages/account-profile-page"

describe("AccountProfilePage", () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it("renders editable public profile data with a new PeerGo avatar upload", () => {
    const queryClient = new QueryClient()
    queryClient.setQueryData(sessionKeys.current(), {
      user: {
        id: "0198f20a-6da8-7e51-9c64-111111111111",
        username: "legacy-user",
        display_name: "迁移用户",
        email_verified: true,
      },
      expires_at: "2026-09-05T12:00:00Z",
      csrf_token: "c".repeat(43),
    })

    render(
      <MemoryRouter initialEntries={["/account"]}>
        <QueryClientProvider client={queryClient}>
          <AccountProfilePage />
        </QueryClientProvider>
      </MemoryRouter>
    )

    expect(screen.getByRole("link", { name: "个人资料" })).toHaveAttribute(
      "aria-current",
      "page"
    )
    expect(screen.getByLabelText("昵称")).toHaveValue("迁移用户")
    expect(screen.getByLabelText("昵称")).toBeEnabled()
    expect(screen.getByLabelText("用户名")).toHaveValue("legacy-user")
    expect(screen.getByText("邮箱已验证")).toBeVisible()
    expect(screen.queryByLabelText("邮箱")).not.toBeInTheDocument()
    expect(screen.getByRole("button", { name: "上传头像" })).toBeEnabled()
    expect(screen.getByRole("button", { name: "上传头像" })).toHaveClass(
      "font-medium",
      "gap-1"
    )
    expect(
      screen.getByRole("button", { name: "上传头像" }).querySelector("svg")
    ).toHaveClass("lucide-upload")
    expect(screen.getByRole("button", { name: "点击头像更换" })).toBeEnabled()
    expect(screen.getByText(/支持 JPG、PNG、WebP 格式，最大 5MB/)).toBeVisible()
    expect(screen.getByText(/昵称将优先显示在评论和公共内容中/)).toHaveClass(
      "text-xs",
      "leading-4"
    )
    expect(
      screen.getByText(/昵称将优先显示在评论和公共内容中/)
    ).not.toHaveClass("min-h-8")
    expect(screen.getByText("暂不支持修改用户名和邮箱。")).toBeVisible()
    expect(screen.getByRole("button", { name: "保存资料" })).toHaveClass(
      "w-[88px]"
    )
    expect(
      screen.getByLabelText("昵称").closest("[data-slot='card-content']")
    ).toHaveClass("px-6", "pb-6")
    expect(
      screen.getByText("邮箱已验证").closest("[data-slot='card']")
    ).toHaveClass("border-success/30")
    expect(screen.queryByText(/功能开放后|暂不可用/)).not.toBeInTheDocument()
  })

  it("saves a normalized nickname through the generated profile endpoint", async () => {
    const user = userEvent.setup()
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(
        JSON.stringify({
          id: "0198f20a-6da8-7e51-9c64-111111111111",
          username: "legacy-user",
          display_name: "新昵称",
          email_verified: true,
        }),
        { status: 200, headers: { "Content-Type": "application/json" } }
      )
    )
    vi.stubGlobal("fetch", fetchMock)
    const queryClient = new QueryClient()
    queryClient.setQueryData(sessionKeys.current(), {
      user: {
        id: "0198f20a-6da8-7e51-9c64-111111111111",
        username: "legacy-user",
        display_name: "迁移用户",
        email_verified: true,
      },
      expires_at: "2026-09-05T12:00:00Z",
      csrf_token: "c".repeat(43),
    })
    render(
      <MemoryRouter initialEntries={["/account"]}>
        <QueryClientProvider client={queryClient}>
          <AccountProfilePage />
        </QueryClientProvider>
      </MemoryRouter>
    )

    await user.clear(screen.getByLabelText("昵称"))
    await user.type(screen.getByLabelText("昵称"), "  新昵称  ")
    await user.click(screen.getByRole("button", { name: "保存资料" }))

    expect(await screen.findByText("个人资料已保存。")).toBeVisible()
    const request = fetchMock.mock.calls[0]?.[0] as Request
    expect(request.url).toContain("/api/v1/me/profile")
    expect(request.method).toBe("PATCH")
    expect(request.headers.get("X-CSRF-Token")).toBe("c".repeat(43))
    await expect(request.clone().json()).resolves.toEqual({
      display_name: "新昵称",
    })
    expect(
      queryClient.getQueryData<{ user: { display_name: string } }>(
        sessionKeys.current()
      )?.user.display_name
    ).toBe("新昵称")
  })
})

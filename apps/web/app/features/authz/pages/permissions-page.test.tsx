import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { fireEvent, render, screen } from "@testing-library/react"
import { MemoryRouter } from "react-router"
import { describe, expect, it } from "vitest"

import { sessionKeys } from "~/features/auth/api/session.mutations"
import {
  capabilityKeys,
  type CapabilityList,
} from "~/features/authz/api/capabilities.queries"
import { PermissionsPage } from "~/features/authz/pages/permissions-page"

const userId = "0198f20a-6da8-7e51-9c64-111111111111"

describe("PermissionsPage", () => {
  it("renders only the current user's generated capability projection", () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    queryClient.setQueryData(sessionKeys.current(), {
      user: {
        id: userId,
        username: "demo",
        display_name: "PeerGo 演示用户",
      },
      expires_at: "2026-08-06T00:00:00Z",
      csrf_token: "c".repeat(43),
    })
    const capabilities: CapabilityList = {
      policy_version: "2026-08-05.1",
      items: [
        {
          action: "authz.capability.read.self",
          description: "查看自己的当前有效权限",
          scope: { type: "site", id: "peergo" },
          expires_at: "2027-08-05T12:00:00Z",
        },
      ],
    }
    queryClient.setQueryData(capabilityKeys.current(userId), capabilities)

    render(
      <MemoryRouter>
        <QueryClientProvider client={queryClient}>
          <PermissionsPage />
        </QueryClientProvider>
      </MemoryRouter>
    )

    expect(screen.getByRole("heading", { name: "我的权限" })).toBeVisible()
    expect(screen.getByRole("heading", { name: "可用功能" })).toHaveClass(
      "text-2xl",
      "leading-none",
      "font-semibold"
    )
    expect(
      screen
        .getByRole("heading", { name: "可用功能" })
        .closest("[data-slot='card']")
    ).toHaveClass("gap-0", "py-0", "ring-0")
    expect(screen.getByRole("link", { name: "安全设置" })).toHaveAttribute(
      "href",
      "/account/security"
    )
    expect(screen.getByLabelText("功能概览")).toBeVisible()
    expect(screen.getByText("账户与安全")).toBeVisible()
    expect(
      screen.getAllByText("1 项", { selector: "[data-slot='badge']" })
    ).toHaveLength(2)
    expect(screen.queryByText("查看自己的当前有效权限")).not.toBeInTheDocument()
    expect(
      screen.getByRole("button", { name: "查看全部 1 项功能" })
    ).toBeVisible()
    fireEvent.click(screen.getByRole("button", { name: "查看全部 1 项功能" }))
    expect(screen.getByRole("table", { name: "全部可用功能" })).toBeVisible()
    expect(screen.getByLabelText("移动端全部可用功能")).toHaveClass("sm:hidden")
    expect(screen.getAllByText("查看功能权限")).toHaveLength(2)
    expect(
      screen.queryByText("authz.capability.read.self")
    ).not.toBeInTheDocument()
    expect(screen.getAllByText("全站")).toHaveLength(2)
    expect(screen.queryByText(/grant/i)).not.toBeInTheDocument()
    expect(screen.queryByText(/mandate/i)).not.toBeInTheDocument()
  })

  it("does not reuse capability cache without an active session", () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    queryClient.setQueryData(sessionKeys.current(), null)

    render(
      <MemoryRouter>
        <QueryClientProvider client={queryClient}>
          <PermissionsPage />
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

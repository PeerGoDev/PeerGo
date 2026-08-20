import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import { MemoryRouter } from "react-router"
import { describe, expect, it } from "vitest"

import { sessionKeys } from "~/features/auth/api/session.mutations"
import { capabilityKeys } from "~/features/authz/api/capabilities.queries"
import { staffSessionKeys } from "~/features/staff/api/staff-session.mutations"
import { StaffAccessGate } from "~/features/staff/components/staff-access-gate"

const userId = "0198f20a-6da8-7e51-9c64-333333333333"

describe("StaffAccessGate", () => {
  it("centres the account-session retry prompt without weakening the admin gate", () => {
    const queryClient = createQueryClient()

    render(
      <MemoryRouter>
        <QueryClientProvider client={queryClient}>
          <StaffAccessGate requiredAction="torrent.review">
            {() => <p>审核队列</p>}
          </StaffAccessGate>
        </QueryClientProvider>
      </MemoryRouter>
    )

    const prompt = screen
      .getByRole("heading", { name: "正在进入管理后台" })
      .closest('[data-slot="card"]')
    const frame = prompt?.parentElement

    expect(prompt).toHaveClass("max-w-xl")
    expect(frame).toHaveClass("items-center", "[&>*]:w-full")
    expect(screen.queryByText("审核队列")).not.toBeInTheDocument()
    expect(screen.getByRole("button", { name: "重新读取" })).toBeVisible()
  })

  it("keeps the destination visible while admin state synchronizes without rendering protected data", () => {
    const queryClient = createQueryClient()

    render(
      <MemoryRouter>
        <QueryClientProvider client={queryClient}>
          <StaffAccessGate
            requiredAction="torrent.review"
            pageHeader={{
              title: "分类管理",
              description: "每个分类拥有独立名称和排序。",
            }}
          >
            {() => <p>受保护的分类数据</p>}
          </StaffAccessGate>
        </QueryClientProvider>
      </MemoryRouter>
    )

    expect(
      screen.getByRole("heading", { level: 1, name: "分类管理" })
    ).toBeVisible()
    expect(screen.getByText("每个分类拥有独立名称和排序。")).toBeVisible()
    expect(
      screen.getByRole("heading", { level: 2, name: "正在进入管理后台" })
    ).toBeVisible()
    expect(screen.queryByText("受保护的分类数据")).not.toBeInTheDocument()
  })
})

function createQueryClient() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  })
  queryClient.setQueryData(sessionKeys.current(), {
    user: {
      id: userId,
      username: "admin",
      display_name: "管理员",
      email_verified: true,
    },
    expires_at: "2026-08-13T00:00:00Z",
    csrf_token: "c".repeat(43),
  })
  queryClient.setQueryData(capabilityKeys.current(userId), {
    policy_version: "2026-08-12",
    items: [
      {
        action: "staff.session.create.self",
        description: "进入员工后台",
        scope: { type: "site", id: "peergo" },
        expires_at: "2026-08-13T00:00:00Z",
      },
    ],
  })
  queryClient.setQueryData(staffSessionKeys.current(), null)
  return queryClient
}

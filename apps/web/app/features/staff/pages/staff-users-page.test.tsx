import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import { MemoryRouter } from "react-router"
import { describe, expect, it } from "vitest"

import { sessionKeys } from "~/features/auth/api/session.mutations"
import { capabilityKeys } from "~/features/authz/api/capabilities.queries"
import { staffSessionKeys } from "~/features/staff/api/staff-session.mutations"
import { managedUserListQueryOptions } from "~/features/staff/api/user-administration.queries"
import { StaffUsersPage } from "~/features/staff/pages/staff-users-page"

const administratorId = "0198f20a-6da8-7e51-9c64-111111111111"

describe("StaffUsersPage", () => {
  it("keeps the Rousi directory density while exposing only PeerGo account data", async () => {
    const queryClient = createQueryClient()

    render(
      <MemoryRouter initialEntries={["/staff/users"]}>
        <QueryClientProvider client={queryClient}>
          <StaffUsersPage />
        </QueryClientProvider>
      </MemoryRouter>
    )

    expect(
      await screen.findByRole("heading", { name: "用户管理" })
    ).toBeVisible()
    expect(
      screen.getByLabelText("搜索用户 ID、UUID、用户名或显示名")
    ).toHaveAttribute("placeholder", "搜索 ID / UUID / 用户名...")
    expect(
      screen.getByRole("combobox", { name: "按账户状态筛选" })
    ).toBeVisible()
    expect(screen.getAllByText(/12,327 条记录/)).toHaveLength(2)
    expect(screen.getAllByText("demo-user")).toHaveLength(2)
    expect(screen.getAllByText("demo@example.com")).toHaveLength(2)
    expect(screen.getAllByText("12327").length).toBeGreaterThan(0)
    expect(
      screen.getAllByText("0198f20a-6da8-7e51-9c64-222222222222").length
    ).toBeGreaterThan(0)
    expect(screen.queryByText(administratorId)).toBeNull()
    expect(screen.queryByRole("button", { name: "增加新用户" })).toBeNull()

    expect(
      screen.getByRole("navigation", { name: "用户列表分页" })
    ).toHaveTextContent("共 12,327 条记录")
    expect(screen.getByRole("button", { name: "上一页" })).toBeDisabled()
    expect(screen.getByRole("button", { name: "下一页" })).toBeEnabled()
    expect(screen.getByText("1 / 617")).toBeVisible()
  })
})

function createQueryClient() {
  const filters = { query: "", status: "all" as const, page: 1, pageSize: 20 }
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  })
  queryClient.setQueryData(sessionKeys.current(), {
    user: {
      id: administratorId,
      username: "admin",
      display_name: "管理员",
      email_verified: true,
    },
    expires_at: "2026-08-15T00:00:00Z",
    csrf_token: "c".repeat(43),
  })
  queryClient.setQueryData(capabilityKeys.current(administratorId), {
    policy_version: "policy-2026-08-14",
    items: [],
  })
  queryClient.setQueryData(staffSessionKeys.current(), {
    user: {
      id: administratorId,
      username: "admin",
      display_name: "管理员",
      email_verified: true,
    },
    expires_at: "2026-08-15T00:00:00Z",
    webauthn_authenticated_at: "2026-08-14T08:00:00Z",
    csrf_token: "s".repeat(43),
  })
  queryClient.setQueryData(staffSessionKeys.capabilities(administratorId), {
    policy_version: "policy-2026-08-14",
    items: [
      {
        action: "user.account.read",
        description: "读取用户目录",
        scope: { type: "site", id: "peergo" },
        expires_at: "2026-08-15T00:00:00Z",
      },
    ],
  })
  queryClient.setQueryData(managedUserListQueryOptions(filters).queryKey, {
    items: [
      {
        id: "0198f20a-6da8-7e51-9c64-222222222222",
        numeric_id: 12_327,
        username: "demo-user",
        display_name: "演示用户",
        email: "demo@example.com",
        email_verified: true,
        banned: false,
        download_restricted: false,
        vip_enabled: true,
        vip_active: true,
        status: "active",
        version: 2,
        active_restriction_count: 0,
        uploaded_bytes: "18742348800",
        downloaded_bytes: "1073741824",
        magic_balance: "31328711552",
        level: 7,
        role_names: ["普通成员"],
        last_active_at: "2026-08-14T07:30:00Z",
        created_at: "2026-08-01T00:00:00Z",
        updated_at: "2026-08-14T08:00:00Z",
      },
    ],
    total: 12_327,
    page: 1,
    page_size: 20,
    summary: {
      total: 12_327,
      active: 11_263,
      banned: 1_064,
      vip: 43,
      download_restricted: 11,
      unverified: 108,
    },
  })
  return queryClient
}

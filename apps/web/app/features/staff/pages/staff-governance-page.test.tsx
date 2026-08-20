import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter } from "react-router"
import { describe, expect, it } from "vitest"

import { sessionKeys } from "~/features/auth/api/session.mutations"
import { capabilityKeys } from "~/features/authz/api/capabilities.queries"
import { governanceKeys } from "~/features/staff/api/governance.queries"
import { staffSessionKeys } from "~/features/staff/api/staff-session.mutations"
import { StaffGovernancePage } from "~/features/staff/pages/staff-governance-page"

const administratorId = "0198f20a-6da8-7e51-9c64-111111111111"
const reviewerId = "0198f20a-6da8-7e51-9c64-222222222222"
const administratorGrantId = "0198f20a-6da8-7e51-9c64-333333333333"
const reviewerGrantId = "0198f20a-6da8-7e51-9c64-444444444444"

describe("StaffGovernancePage", () => {
  it("uses the compact workgroup layout without weakening governance rules", async () => {
    const user = userEvent.setup()
    const queryClient = createQueryClient()

    render(
      <MemoryRouter initialEntries={["/staff/governance"]}>
        <QueryClientProvider client={queryClient}>
          <StaffGovernancePage />
        </QueryClientProvider>
      </MemoryRouter>
    )

    expect(
      await screen.findByRole("heading", { name: "权限与任期" })
    ).toHaveClass("text-2xl", "font-bold")
    expect(screen.getByRole("button", { name: "刷新" })).toHaveClass(
      "min-w-[78px]"
    )
    expect(
      screen.getByText("授权记录").closest('[data-slot="card"]')
    ).toHaveClass("h-[102px]")
    expect(screen.getByText("有效任期")).toBeVisible()
    expect(screen.getByText("待复核撤权")).toBeVisible()
    expect(screen.getByText("已加载")).toBeVisible()

    expect(screen.getByRole("button", { name: "角色授权" })).toHaveAttribute(
      "aria-pressed",
      "true"
    )
    expect(
      screen.getByRole("heading", { name: "角色授权与任期" })
    ).toBeVisible()
    expect(screen.getByText("不可对自己操作")).toBeVisible()
    expect(screen.getByText("撤权待复核")).toBeVisible()
    expect(screen.queryByRole("button", { name: "新增授权" })).toBeNull()

    await user.click(screen.getByRole("button", { name: /撤权复核/ }))
    expect(screen.getByRole("heading", { name: "撤权复核" })).toBeVisible()
    expect(screen.getAllByText("待处理", { exact: true })).toHaveLength(2)
    expect(screen.getAllByText("尚未作出决定")).toHaveLength(2)
  })
})

function createQueryClient() {
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
        action: "authz.grant.read",
        description: "读取权限授权",
        scope: { type: "site", id: "peergo" },
        expires_at: "2026-08-15T00:00:00Z",
      },
      {
        action: "authz.grant.revoke.propose",
        description: "发起撤权",
        scope: { type: "site", id: "peergo" },
        expires_at: "2026-08-15T00:00:00Z",
      },
      {
        action: "authz.grant.revoke.approve.governance",
        description: "治理复核",
        scope: { type: "site", id: "peergo" },
        expires_at: "2026-08-15T00:00:00Z",
      },
    ],
  })
  queryClient.setQueryData(governanceKeys.overview(), {
    policy_version: "policy-2026-08-14",
    grants: [
      {
        id: administratorGrantId,
        subject_id: administratorId,
        subject_username: "admin",
        subject_display_name: "管理员",
        role_id: "site-administrator",
        role_name: "站点管理员",
        mandate_id: "0198f20a-6da8-7e51-9c64-555555555555",
        mandate_status: "active",
        scope: { type: "site", id: "peergo" },
        valid_from: "2026-08-01T00:00:00Z",
        valid_until: "2026-09-01T00:00:00Z",
        version: 3,
      },
      {
        id: reviewerGrantId,
        subject_id: reviewerId,
        subject_username: "reviewer",
        subject_display_name: "审核员",
        role_id: "torrent-reviewer",
        role_name: "种子审核员",
        mandate_id: "0198f20a-6da8-7e51-9c64-666666666666",
        mandate_status: "active",
        scope: { type: "site", id: "peergo" },
        valid_from: "2026-08-01T00:00:00Z",
        valid_until: "2026-09-01T00:00:00Z",
        version: 2,
      },
    ],
    requests: [
      {
        id: "0198f20a-6da8-7e51-9c64-777777777777",
        grant_id: reviewerGrantId,
        expected_grant_version: 2,
        target_subject_id: reviewerId,
        proposer_id: "0198f20a-6da8-7e51-9c64-888888888888",
        reason: "该审核员的临时任期已经结束，需要按流程撤销权限。",
        status: "pending",
        created_at: "2026-08-14T08:00:00Z",
        expires_at: "2026-08-15T08:00:00Z",
        reviews: [],
      },
    ],
  })
  return queryClient
}

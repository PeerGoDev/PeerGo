import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter } from "react-router"
import { describe, expect, it, vi } from "vitest"

import {
  managedUserDetailQueryOptions,
  type ManagedUserDetail,
} from "~/features/staff/api/user-administration.queries"
import { ManagedUserDialog } from "~/features/staff/components/managed-user-detail-sheet"
import { managedTrackerActivityQueryOptions } from "~/features/user/api/tracker-activity.queries"

const userId = "0198f20a-6da8-7e51-9c64-777777777777"

const detail: ManagedUserDetail = {
  id: userId,
  numeric_id: 45,
  username: "dialog-target",
  display_name: "弹窗目标",
  email: "dialog@example.com",
  email_verified: true,
  banned: false,
  download_restricted: false,
  vip_enabled: false,
  vip_active: false,
  status: "active",
  version: 3,
  active_restriction_count: 0,
  uploaded_bytes: "10737418240",
  downloaded_bytes: "5368709120",
  magic_balance: "5000",
  level: 2,
  role_names: ["普通成员"],
  experience: "1200",
  donation_amount: "88.50",
  remaining_invites: 3,
  submitted_torrent_count: 4,
  published_torrent_count: 3,
  pending_review_torrent_count: 1,
  direct_invite_count: 2,
  inviter_numeric_id: null,
  inviter_username: null,
  registration_mode: "invite",
  registration_state: "completed",
  active_restrictions: [],
  manual_download_restriction: { active: false, version: 2 },
  manual_download_restriction_history: [],
  vip_state: {
    enabled: false,
    active: false,
    version: 2,
  },
  vip_history: [],
  created_at: "2026-08-01T09:00:00Z",
  updated_at: "2026-08-15T09:00:00Z",
}

describe("ManagedUserDialog", () => {
  it("keeps user administration in one modal and reveals one section at a time", async () => {
    const user = userEvent.setup()
    const onOpenChange = vi.fn()
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false, staleTime: Infinity } },
    })
    queryClient.setQueryData(
      managedUserDetailQueryOptions(userId).queryKey,
      detail
    )
    queryClient.setQueryData(
      managedTrackerActivityQueryOptions(userId).queryKey,
      {
        total_connections: 0,
        truncated: false,
        generated_at: "2026-08-28T22:42:00Z",
        items: [],
      }
    )

    render(
      <MemoryRouter>
        <QueryClientProvider client={queryClient}>
          <ManagedUserDialog
            open
            onOpenChange={onOpenChange}
            userId={userId}
            csrfToken="test-csrf"
            currentStaffUserId="0198f20a-6da8-7e51-9c64-999999999999"
            canRestrict
            canRevoke
            canDownloadRestrict
            canDownloadRevoke
            canManageVIP
            canAssignAssessment
            canAdjustData
            canReadNetworkHistory={false}
          />
        </QueryClientProvider>
      </MemoryRouter>
    )

    expect(screen.getByRole("dialog", { name: "用户设置" })).toBeInTheDocument()
    expect(screen.getByText("弹窗目标")).toBeVisible()
    expect(screen.getByRole("tab", { name: "基础信息" })).toHaveAttribute(
      "aria-selected",
      "true"
    )
    expect(screen.getByText("账户资料")).toBeVisible()
    expect(screen.getByRole("tab", { name: "数据设置" })).toBeVisible()
    expect(screen.getByRole("tab", { name: "BT 在线" })).toBeVisible()
    expect(screen.getByRole("tab", { name: "VIP 与考核" })).toBeVisible()
    expect(screen.getByRole("tab", { name: "访问限制" })).toBeVisible()

    await user.click(screen.getByRole("tab", { name: "数据设置" }))
    expect(screen.getByText("当前账户数据")).toBeVisible()
    expect(screen.getByRole("button", { name: "增减数据" })).toBeVisible()
    expect(screen.getByText("¥88.50")).toBeVisible()

    await user.click(screen.getByRole("tab", { name: "VIP 与考核" }))
    expect(screen.getByText("VIP 身份")).toBeVisible()
    expect(screen.getByText("账户状态与新人考核")).toBeVisible()

    await user.click(screen.getByRole("button", { name: "关闭" }))
    expect(onOpenChange.mock.calls[0]?.[0]).toBe(false)
  })
})

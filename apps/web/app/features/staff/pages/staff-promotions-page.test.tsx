import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter } from "react-router"
import { describe, expect, it } from "vitest"

import { sessionKeys } from "~/features/auth/api/session.mutations"
import { capabilityKeys } from "~/features/authz/api/capabilities.queries"
import { promotionCampaignListQueryOptions } from "~/features/staff/api/promotion-administration.queries"
import { staffSessionKeys } from "~/features/staff/api/staff-session.mutations"
import { StaffPromotionsPage } from "~/features/staff/pages/staff-promotions-page"

const administratorId = "0198f20a-6da8-7e51-9c64-111111111111"

describe("StaffPromotionsPage", () => {
  it("renders the immutable timeline and scheduling entry point", async () => {
    const user = userEvent.setup()
    const queryClient = createQueryClient()

    render(
      <MemoryRouter initialEntries={["/staff/settings/promotions"]}>
        <QueryClientProvider client={queryClient}>
          <StaffPromotionsPage />
        </QueryClientProvider>
      </MemoryRouter>
    )

    expect(
      await screen.findByRole("heading", { name: "优惠规则" })
    ).toBeVisible()
    expect(screen.getByText("按 announce 所属时刻结算")).toBeVisible()
    expect(screen.getByText("全站活动")).toBeVisible()
    expect(screen.getByText("2× / 免费")).toBeVisible()
    expect(screen.getByText("已入账本")).toBeVisible()
    expect(screen.getByText("生效中")).toBeVisible()

    await user.click(screen.getByRole("button", { name: "签发优惠" }))
    expect(
      await screen.findByRole("heading", { name: "签发优惠政策" })
    ).toBeVisible()
    expect(screen.getByLabelText("作用范围")).toBeVisible()
    expect(screen.getByLabelText("优惠类型")).toBeVisible()
    const submitButton = screen.getByRole("button", { name: "确认签发" })
    const reason = screen.getByLabelText("签发原因")
    expect(submitButton).toBeDisabled()
    expect(
      screen.getByText("已输入 0/1000 个字符；至少填写 10 个字符后才可签发。")
    ).toBeVisible()

    await user.type(reason, "周末活动")
    expect(screen.getByText("签发原因至少需要 10 个字符。")).toBeVisible()
    expect(reason).toHaveAttribute("aria-invalid", "true")
    expect(submitButton).toBeDisabled()

    await user.type(reason, "用于鼓励成员保种")
    expect(
      screen.queryByText("签发原因至少需要 10 个字符。")
    ).not.toBeInTheDocument()
    expect(reason).not.toHaveAttribute("aria-invalid", "true")
    expect(submitButton).toBeEnabled()
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
    expires_at: "2026-08-16T00:00:00Z",
    csrf_token: "c".repeat(43),
  })
  queryClient.setQueryData(capabilityKeys.current(administratorId), {
    policy_version: "policy-2026-08-15",
    items: [],
  })
  queryClient.setQueryData(staffSessionKeys.current(), {
    user: {
      id: administratorId,
      username: "admin",
      display_name: "管理员",
      email_verified: true,
    },
    expires_at: "2026-08-16T00:00:00Z",
    webauthn_authenticated_at: "2026-08-15T08:00:00Z",
    csrf_token: "s".repeat(43),
  })
  queryClient.setQueryData(staffSessionKeys.capabilities(administratorId), {
    policy_version: "policy-2026-08-15",
    items: ["promotion.manage.read", "promotion.schedule"].map((action) => ({
      action,
      description: action,
      scope: { type: "site", id: "peergo" },
      expires_at: "2026-08-16T00:00:00Z",
    })),
  })
  queryClient.setQueryData(promotionCampaignListQueryOptions().queryKey, {
    items: [
      {
        id: "0198f20a-6da8-7e51-9c64-515151515151",
        source: "staff_schedule",
        scope: "global",
        torrent_id: null,
        torrent_title: "",
        promotion: "double_upload_free",
        starts_at: "2026-08-15T08:00:00Z",
        ends_at: "2026-08-16T08:00:00Z",
        override_lower_scopes: true,
        reason: "周末全站做种活动，鼓励成员补种。",
        created_at: "2026-08-15T07:00:00Z",
        delivery_state: "delivered",
        delivery_attempts: 1,
        last_delivery_error: "",
        delivered_at: "2026-08-15T07:00:01Z",
        timeline_state: "active",
      },
    ],
    total: 1,
    limit: 50,
    offset: 0,
  })
  return queryClient
}

import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { describe, expect, it } from "vitest"

import type { ManagedUserDetail } from "~/features/staff/api/user-administration.queries"
import { VIPControls } from "~/features/staff/components/vip-controls"
import { AppProviders } from "~/shared/providers/app-providers"

const detail: ManagedUserDetail = {
  id: "0198f20a-6da8-7e51-9c64-777777777777",
  numeric_id: 45,
  username: "vip-target",
  display_name: "VIP 目标",
  email: "vip@example.com",
  email_verified: true,
  banned: false,
  download_restricted: false,
  vip_enabled: true,
  vip_active: true,
  vip_until: "2026-12-31T00:00:00Z",
  status: "active",
  version: 3,
  active_restriction_count: 0,
  uploaded_bytes: "1024",
  downloaded_bytes: "512",
  magic_balance: "5000",
  level: 2,
  role_names: ["普通成员"],
  experience: "1200",
  remaining_invites: 3,
  submitted_torrent_count: 4,
  published_torrent_count: 3,
  pending_review_torrent_count: 1,
  direct_invite_count: 2,
  inviter_numeric_id: null,
  inviter_username: null,
  registration_mode: null,
  registration_state: null,
  active_restrictions: [],
  manual_download_restriction: { active: false, version: 2 },
  manual_download_restriction_history: [],
  vip_state: {
    enabled: true,
    active: true,
    until: "2026-12-31T00:00:00Z",
    version: 2,
  },
  vip_history: [
    {
      transition: "granted",
      origin: "legacy_migration",
      reason_summary: "该 VIP 身份从 Rousi 当前账户状态迁入。",
      enabled: true,
      until: "2026-12-31T00:00:00Z",
      state_version: 2,
      occurred_at: "2026-08-15T09:00:00Z",
    },
  ],
  created_at: "2026-08-01T09:00:00Z",
  updated_at: "2026-08-15T09:00:00Z",
}

describe("VIPControls", () => {
  it("shows the current entitlement and immutable history", () => {
    renderControls(detail)

    expect(screen.getByText("限期 VIP")).toBeInTheDocument()
    expect(screen.getByText("签发")).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "续期／改为永久" })).toBeEnabled()
    expect(screen.getByRole("button", { name: "撤销 VIP" })).toBeEnabled()
  })

  it("offers bounded durations and a required reason", async () => {
    const user = userEvent.setup()
    renderControls({
      ...detail,
      vip_enabled: false,
      vip_active: false,
      vip_until: undefined,
      vip_state: { enabled: false, active: false, version: 2 },
      vip_history: [],
    })

    await user.click(screen.getByRole("button", { name: "签发 VIP" }))
    expect(
      screen.getByRole("heading", { name: "签发或续期 VIP" })
    ).toBeVisible()
    expect(screen.getByText("30 天")).toBeVisible()
    expect(screen.getByText("永久")).toBeVisible()
    expect(screen.getByLabelText("人工理由")).toHaveAttribute(
      "maxlength",
      "500"
    )
  })

  it("does not allow a staff member to change their own VIP", () => {
    renderControls(detail, detail.id)

    expect(screen.getByText(/不能变更自己的 VIP/)).toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "撤销 VIP" })).toBeNull()
  })
})

function renderControls(
  selected: ManagedUserDetail,
  currentStaffUserId = "0198f20a-6da8-7e51-9c64-999999999999"
) {
  return render(
    <AppProviders>
      <VIPControls
        detail={selected}
        csrfToken="test-csrf"
        currentStaffUserId={currentStaffUserId}
        canManage
      />
    </AppProviders>
  )
}

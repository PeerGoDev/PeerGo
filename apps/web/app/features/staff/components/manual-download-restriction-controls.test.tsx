import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { describe, expect, it } from "vitest"

import type { ManagedUserDetail } from "~/features/staff/api/user-administration.queries"
import { ManualDownloadRestrictionControls } from "~/features/staff/components/manual-download-restriction-controls"
import { AppProviders } from "~/shared/providers/app-providers"

const detail: ManagedUserDetail = {
  id: "0198f20a-6da8-7e51-9c64-777777777777",
  numeric_id: 45,
  username: "download-target",
  display_name: "下载目标",
  email: "target@example.com",
  email_verified: true,
  banned: false,
  download_restricted: true,
  vip_enabled: false,
  vip_active: false,
  status: "active",
  version: 3,
  active_restriction_count: 0,
  uploaded_bytes: "1024",
  downloaded_bytes: "512",
  magic_balance: "5000",
  level: 2,
  role_names: ["普通成员"],
  active_restrictions: [],
  manual_download_restriction: {
    active: true,
    version: 1,
    origin: "legacy_migration",
    reason_code: "legacy_download_restriction",
    reason_summary:
      "该下载限制从旧站当前账户状态迁入，需要由用户管理员单独复核。",
    started_at: "2026-08-15T09:00:00Z",
  },
  manual_download_restriction_history: [
    {
      transition: "restricted",
      origin: "legacy_migration",
      reason_code: "legacy_download_restriction",
      reason_summary:
        "该下载限制从旧站当前账户状态迁入，需要由用户管理员单独复核。",
      state_version: 1,
      occurred_at: "2026-08-15T09:00:00Z",
    },
  ],
  vip_state: { enabled: false, active: false, version: 2 },
  vip_history: [],
  created_at: "2026-08-01T09:00:00Z",
  updated_at: "2026-08-15T09:00:00Z",
}

describe("ManualDownloadRestrictionControls", () => {
  it("shows legacy/manual evidence separately from ratio and H&R", () => {
    renderControls(detail)

    expect(screen.getAllByText("旧站迁入")).toHaveLength(2)
    expect(screen.getByText(/分享率与 H&R 保持各自独立/)).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "修改理由" })).toBeEnabled()
    expect(screen.getByRole("button", { name: "解除限制" })).toBeEnabled()
    expect(screen.getByText("系统迁入")).toBeInTheDocument()
  })

  it("opens bounded create and revoke surfaces", async () => {
    const user = userEvent.setup()
    const inactive = {
      ...detail,
      download_restricted: false,
      manual_download_restriction: { active: false, version: 2 },
    }
    renderControls(inactive)

    await user.click(screen.getByRole("button", { name: "签发限制" }))
    expect(
      screen.getByRole("heading", { name: "签发人工下载限制" })
    ).toBeInTheDocument()
    expect(screen.getByLabelText("人工理由")).toHaveAttribute(
      "maxlength",
      "500"
    )
  })

  it("never renders write controls for the current staff account", () => {
    renderControls(detail, detail.id)

    expect(screen.getByText(/不能处置自己的下载权限/)).toBeInTheDocument()
    expect(
      screen.queryByRole("button", { name: "解除限制" })
    ).not.toBeInTheDocument()
  })
})

function renderControls(
  selectedDetail: ManagedUserDetail,
  currentStaffUserId = "0198f20a-6da8-7e51-9c64-999999999999"
) {
  return render(
    <AppProviders>
      <ManualDownloadRestrictionControls
        detail={selectedDetail}
        csrfToken="test-csrf"
        currentStaffUserId={currentStaffUserId}
        canRestrict
        canRevoke
      />
    </AppProviders>
  )
}

import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { describe, expect, it } from "vitest"

import type { ManagedUserDetail } from "~/features/staff/api/user-administration.queries"
import { AccountRestrictionControls } from "~/features/staff/components/account-restriction-controls"
import { AppProviders } from "~/shared/providers/app-providers"

const detail: ManagedUserDetail = {
  id: "0198f20a-6da8-7e51-9c64-666666666666",
  numeric_id: 23,
  username: "demo-target",
  display_name: "演示目标",
  email: "demo@example.com",
  email_verified: true,
  banned: false,
  download_restricted: false,
  vip_enabled: false,
  vip_active: false,
  status: "active",
  version: 4,
  active_restriction_count: 0,
  uploaded_bytes: "1024",
  downloaded_bytes: "512",
  magic_balance: "5000",
  level: 2,
  role_names: ["普通成员"],
  last_active_at: "2026-08-06T08:00:00Z",
  active_restrictions: [],
  manual_download_restriction: { active: false, version: 0 },
  manual_download_restriction_history: [],
  vip_state: { enabled: false, active: false, version: 0 },
  vip_history: [],
  created_at: "2026-08-05T09:00:00Z",
  updated_at: "2026-08-06T09:00:00Z",
}

describe("AccountRestrictionControls", () => {
  it("blocks self-targeting before rendering any write form", () => {
    renderControls({ currentStaffUserId: detail.id })

    expect(screen.getByText("不能处置自己的账户")).toBeInTheDocument()
    expect(
      screen.queryByRole("button", { name: "审阅限制" })
    ).not.toBeInTheDocument()
  })

  it("reviews the bounded effect before creating a restriction", async () => {
    const user = userEvent.setup()
    renderControls()

    await user.type(
      screen.getByLabelText("人工理由"),
      "核对账户异常状态并等待人工复核结论。"
    )
    await user.click(screen.getByRole("button", { name: "审阅限制" }))

    expect(
      screen.getByRole("heading", { name: "确认临时限制账户访问" })
    ).toBeInTheDocument()
    expect(screen.getAllByText("24 小时")).toHaveLength(2)
    expect(screen.getByText("影响：立即阻断账户访问")).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "确认并立即限制" })).toBeEnabled()
  })

  it("rechecks account and restriction state before revocation", async () => {
    const user = userEvent.setup()
    renderControls({
      detail: {
        ...detail,
        active_restriction_count: 1,
        active_restrictions: [
          {
            id: "0198f20a-6da8-7e51-9c64-777777777777",
            kind: "account_access",
            reason_code: "manual_review",
            reason_summary: "等待人工复核",
            starts_at: "2026-08-06T09:00:00Z",
            expires_at: "2026-08-07T09:00:00Z",
            version: 2,
          },
        ],
      },
    })

    await user.type(
      screen.getByLabelText("人工理由"),
      "复核工作已经完成，确认可以恢复账户访问。"
    )
    await user.click(screen.getByRole("button", { name: "审阅解除" }))

    expect(
      screen.getByRole("heading", { name: "确认解除账户访问限制" })
    ).toBeInTheDocument()
    expect(
      screen.getByText(/保存前会再次确认账户和限制状态/)
    ).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "确认解除限制" })).toBeEnabled()
  })
})

function renderControls({
  detail: selectedDetail = detail,
  currentStaffUserId = "0198f20a-6da8-7e51-9c64-888888888888",
}: {
  detail?: ManagedUserDetail
  currentStaffUserId?: string
} = {}) {
  return render(
    <AppProviders>
      <AccountRestrictionControls
        detail={selectedDetail}
        csrfToken="test-csrf"
        currentStaffUserId={currentStaffUserId}
        canRestrict
        canRevoke
        onRefresh={() => undefined}
      />
    </AppProviders>
  )
}

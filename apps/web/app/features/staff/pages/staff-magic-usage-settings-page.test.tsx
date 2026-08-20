import { QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import { MemoryRouter } from "react-router"
import { describe, expect, it } from "vitest"

import { economySettingsQueryOptions } from "~/features/staff/api/operations.queries"
import { contentTipPolicyListQueryOptions } from "~/features/staff/api/content-tip-administration.queries"
import { memberGiftPolicyListQueryOptions } from "~/features/staff/api/member-gift-administration.queries"
import { StaffMagicUsageSettingsPage } from "~/features/staff/pages/staff-magic-usage-settings-page"
import {
  createStaffPageQueryClient,
  economySettingsFixture,
} from "~/features/staff/test-utils/create-staff-page-query-client"

describe("StaffMagicUsageSettingsPage", () => {
  it("shows unified integer magic rules and real ledger totals", async () => {
    const queryClient = createStaffPageQueryClient([
      "economy.seedingreward.policy.read",
    ])
    queryClient.setQueryData(
      economySettingsQueryOptions().queryKey,
      economySettingsFixture()
    )
    queryClient.setQueryData(memberGiftPolicyListQueryOptions().queryKey, {
      items: [
        {
          revision: "member-gift-policy-2026-08-17-v1",
          settings: {
            enabled: false,
            minimum_amount: "1",
            maximum_amount: "1000000",
            daily_gross_limit: "1000000",
            fee_bps: 0,
          },
          snapshot_sha256:
            "79cccf4c56fd285411837109cd62b9cbea43009a23a7db908b36932b396dd1b7",
          reason: "迁移基线：管理员确认规则后再开放成员赠送。",
          issued_by: "00000000-0000-0000-0000-000000000001",
          created_at: "2026-08-17T00:00:00Z",
        },
      ],
      limit: 30,
      offset: 0,
      total: "1",
    })
    queryClient.setQueryData(contentTipPolicyListQueryOptions().queryKey, {
      items: [
        {
          revision: "content-tip-disabled-v1",
          settings: {
            enabled: false,
            minimum_amount: "1",
            maximum_amount: "1000000",
            daily_gross_limit: "1000000",
            fee_bps: 0,
          },
          snapshot_sha256:
            "b487c91c7836926a3fd47db56e601ac1ca09c8e31faad38fa2e583aa09db663f",
          reason: "迁移基线：管理员确认规则后再开放内容打赏。",
          issued_by: "00000000-0000-0000-0000-000000000001",
          created_at: "2026-08-17T00:00:01Z",
        },
      ],
      limit: 30,
      offset: 0,
      total: "1",
    })

    render(
      <MemoryRouter initialEntries={["/staff/settings/magic-usage"]}>
        <QueryClientProvider client={queryClient}>
          <StaffMagicUsageSettingsPage />
        </QueryClientProvider>
      </MemoryRouter>
    )

    expect(
      await screen.findByRole("heading", { name: "魔力值使用规则" })
    ).toBeVisible()
    expect(screen.getAllByText("魔力值").length).toBeGreaterThan(0)
    expect(screen.getByText("整数")).toBeVisible()
    expect(screen.getByText("不再启用 PT 币")).toBeVisible()
    expect(screen.getAllByText("成员赠送").length).toBeGreaterThan(0)
    expect(screen.getAllByText("内容打赏").length).toBeGreaterThan(0)
    expect(screen.getAllByText("已关闭").length).toBeGreaterThanOrEqual(2)
    expect(screen.getByText("12,327")).toBeVisible()
    expect(
      screen.getAllByText("账务已准备，入口未开放").length
    ).toBeGreaterThanOrEqual(1)
  })
})

import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { describe, expect, it, vi } from "vitest"

import { ManagedUserTable } from "~/features/staff/components/managed-user-table"

describe("ManagedUserTable", () => {
  it("renders authorized operational state and opens detail by UUID", async () => {
    const user = userEvent.setup()
    const onSelect = vi.fn()
    render(
      <ManagedUserTable
        users={[
          {
            id: "0198f20a-6da8-7e51-9c64-666666666666",
            numeric_id: 12_327,
            username: "demo-target",
            display_name: "演示目标",
            email: "demo@example.com",
            email_verified: false,
            banned: false,
            download_restricted: true,
            vip_enabled: true,
            vip_active: true,
            status: "active",
            version: 3,
            active_restriction_count: 1,
            uploaded_bytes: "18742348800",
            downloaded_bytes: "1073741824",
            magic_balance: "31328711552",
            level: 7,
            role_names: ["普通成员"],
            last_active_at: "2026-08-06T08:00:00Z",
            created_at: "2026-08-05T09:00:00Z",
            updated_at: "2026-08-06T09:00:00Z",
          },
        ]}
        hasFilters={false}
        onSelect={onSelect}
      />
    )

    expect(screen.getAllByText("demo-target").length).toBeGreaterThan(0)
    expect(screen.getAllByText("demo@example.com").length).toBeGreaterThan(0)
    expect(screen.getAllByText("12327").length).toBeGreaterThan(0)
    expect(
      screen.getAllByText("0198f20a-6da8-7e51-9c64-666666666666").length
    ).toBeGreaterThan(0)
    expect(screen.getAllByText("17.5 GB").length).toBeGreaterThan(0)
    expect(screen.getAllByText("1 GB").length).toBeGreaterThan(0)
    expect(screen.getAllByText("31,328,711,552").length).toBeGreaterThan(0)
    expect(screen.getAllByText("1 项有效限制").length).toBeGreaterThan(0)
    expect(screen.getAllByText("下载受限").length).toBeGreaterThan(0)
    expect(screen.getAllByText("未验证").length).toBeGreaterThan(0)
    expect(screen.getByText(/表格可左右滑动/)).toBeVisible()
    expect(screen.getByRole("table")).toHaveClass(
      "min-w-[1120px]",
      "lg:min-w-[1480px]"
    )
    await user.click(
      screen.getByRole("button", { name: "管理账户 demo-target" })
    )
    expect(onSelect).toHaveBeenCalledWith(
      "0198f20a-6da8-7e51-9c64-666666666666"
    )
  })

  it("explains an empty filtered result", () => {
    render(<ManagedUserTable users={[]} hasFilters onSelect={vi.fn()} />)
    expect(screen.getByText("没有匹配账户")).toBeInTheDocument()
  })

  it("keeps a roleless pending registration visible for recovery", () => {
    render(
      <ManagedUserTable
        users={[
          {
            id: "0198f20a-6da8-7e51-9c64-777777777777",
            numeric_id: 12_328,
            username: "pending-member",
            display_name: "待恢复成员",
            email: "pending@example.com",
            email_verified: false,
            banned: false,
            download_restricted: false,
            vip_enabled: false,
            vip_active: false,
            status: "pending",
            version: 1,
            active_restriction_count: 0,
            uploaded_bytes: "0",
            downloaded_bytes: "0",
            magic_balance: "0",
            level: 1,
            role_names: [],
            created_at: "2026-08-27T15:32:22Z",
            updated_at: "2026-08-27T15:32:22Z",
          },
        ]}
        hasFilters={false}
        onSelect={vi.fn()}
      />
    )

    expect(screen.getAllByText("pending-member").length).toBeGreaterThan(0)
    expect(screen.getAllByText("未分配").length).toBeGreaterThan(0)
    expect(screen.getAllByText("待激活").length).toBeGreaterThan(0)
  })
})

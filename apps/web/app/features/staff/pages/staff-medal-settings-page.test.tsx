import { QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import { MemoryRouter } from "react-router"
import { describe, expect, it } from "vitest"

import {
  type MedalDefinition,
  medalDefinitionOverviewQueryOptions,
} from "~/features/staff/api/medal-administration.queries"
import { StaffMedalSettingsPage } from "~/features/staff/pages/staff-medal-settings-page"
import { createStaffPageQueryClient } from "~/features/staff/test-utils/create-staff-page-query-client"

describe("StaffMedalSettingsPage", () => {
  it("renders imported definitions, holdings and missing-image status", async () => {
    const queryClient = createStaffPageQueryClient([
      "economy.medal.manage.read",
      "economy.medal.create",
      "economy.medal.update",
      "workgroup.manage.read",
    ])
    queryClient.setQueryData(medalDefinitionOverviewQueryOptions().queryKey, {
      settings: {
        enabled: true,
        maximum_wear_count: 3,
        maximum_upload_bonus_bps: 20_000,
        maximum_download_discount_bps: 10_000,
        maximum_magic_bonus_bps: 10_000,
        maximum_invite_bonus: "10",
        condition_check_day: 1,
        condition_warning_days: 7,
        version: 1,
        updated_at: "2026-08-18T08:00:00Z",
      },
      total: "20",
      items: [
        medalFixture({
          id: "1",
          name: "种审组",
          acquisition_method: "workgroup",
          holder_count: "25",
          active_holder_count: "24",
          wearing_count: "24",
          magic_bonus_bps: 1500,
          conditions_count: "1",
          privileges_count: "2",
        }),
        medalFixture({
          id: "2",
          name: "站点贡献者",
          acquisition_method: "sponsor",
          holder_count: "40438",
          active_holder_count: "40000",
          wearing_count: "1024",
          image_small_path: "/uploads/medals/supporter.webp",
        }),
      ],
    })

    render(
      <MemoryRouter initialEntries={["/staff/settings/medals"]}>
        <QueryClientProvider client={queryClient}>
          <StaffMedalSettingsPage />
        </QueryClientProvider>
      </MemoryRouter>
    )

    expect(
      await screen.findByRole("heading", { name: "勋章管理" })
    ).toBeVisible()
    expect(screen.getByText("40,463")).toBeVisible()
    expect(screen.getByText("种审组")).toBeVisible()
    expect(screen.getByText("工作组")).toBeVisible()
    expect(screen.getByText("魔力 +15%")).toBeVisible()
    expect(screen.getByText("旧站勋章图片尚未全部绑定")).toBeVisible()
    expect(screen.getByText("旧站条件已归档，工作组目标独立管理")).toBeVisible()
    expect(
      screen.getByRole("link", { name: "前往工作组目标" })
    ).toHaveAttribute("href", "/staff/workgroups")
    expect(screen.getByRole("button", { name: "新增勋章" })).toBeEnabled()
    expect(screen.getByRole("button", { name: "编辑规则" })).toBeEnabled()
    expect(screen.getByRole("button", { name: "编辑 种审组" })).toBeEnabled()
  })
})

function medalFixture(override: Partial<MedalDefinition>): MedalDefinition {
  return {
    id: "1",
    name: "勋章",
    description: null,
    image_large_path: null,
    image_small_path: null,
    acquisition_method: "grant",
    price: "0",
    duration_days: 0,
    display_on_page: true,
    priority: 0,
    upload_bonus_bps: 0,
    download_discount_bps: 0,
    magic_bonus_bps: 0,
    invite_bonus: "0",
    is_workgroup: false,
    pool_eligible: false,
    periodic_reward_magic: "0",
    reward_cycle: null,
    sale_begin_at: null,
    sale_end_at: null,
    inventory: null,
    conditions_count: "0",
    privileges_count: "0",
    version: 1,
    holder_count: "0",
    active_holder_count: "0",
    wearing_count: "0",
    created_at: "2026-08-18T08:00:00Z",
    updated_at: "2026-08-18T08:00:00Z",
    ...override,
  }
}

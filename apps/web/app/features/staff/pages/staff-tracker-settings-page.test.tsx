import { QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import { MemoryRouter } from "react-router"
import { describe, expect, it } from "vitest"

import { trackerOperationsQueryOptions } from "~/features/staff/api/operations.queries"
import { StaffTrackerSettingsPage } from "~/features/staff/pages/staff-tracker-settings-page"
import {
  createStaffPageQueryClient,
  trackerOperationsFixture,
} from "~/features/staff/test-utils/create-staff-page-query-client"

describe("StaffTrackerSettingsPage", () => {
  it("renders control, swarm, settlement and member projection health", async () => {
    const queryClient = createStaffPageQueryClient(["operations.monitor.read"])
    queryClient.setQueryData(
      trackerOperationsQueryOptions().queryKey,
      trackerOperationsFixture()
    )

    render(
      <MemoryRouter initialEntries={["/staff/operations/tracker"]}>
        <QueryClientProvider client={queryClient}>
          <StaffTrackerSettingsPage />
        </QueryClientProvider>
      </MemoryRouter>
    )

    expect(
      await screen.findByRole("heading", { name: "Tracker 状态" })
    ).toBeVisible()
    expect(screen.getAllByText("8,722").length).toBeGreaterThan(0)
    expect(screen.getByText("tracker-primary")).toBeVisible()
    expect(screen.getByText("结算证据与用户投影")).toBeVisible()
    expect(screen.getByText("已完成")).toBeVisible()
  })
})

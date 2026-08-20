import { QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import { MemoryRouter } from "react-router"
import { describe, expect, it } from "vitest"

import { workerOperationsQueryOptions } from "~/features/staff/api/operations.queries"
import { StaffOperationIncidentsPage } from "~/features/staff/pages/staff-operation-incidents-page"
import {
  createStaffPageQueryClient,
  workerOperationsFixture,
} from "~/features/staff/test-utils/create-staff-page-query-client"

describe("StaffOperationIncidentsPage", () => {
  it("filters ordinary backlog from failures and keeps audit delivery visible", async () => {
    const queryClient = createStaffPageQueryClient(["operations.monitor.read"])
    queryClient.setQueryData(
      workerOperationsQueryOptions().queryKey,
      workerOperationsFixture()
    )

    render(
      <MemoryRouter initialEntries={["/staff/operations/incidents"]}>
        <QueryClientProvider client={queryClient}>
          <StaffOperationIncidentsPage />
        </QueryClientProvider>
      </MemoryRouter>
    )

    expect(
      await screen.findByRole("heading", { name: "任务异常与审计" })
    ).toBeVisible()
    expect(screen.getByText("settlement_unavailable")).toBeVisible()
    expect(screen.getByText("审计事件投递")).toBeVisible()
    expect(screen.queryByText("Tracker 控制投影")).toBeNull()
    expect(screen.getByText("本页只读，不提供跳过或清空任务")).toBeVisible()
  })
})

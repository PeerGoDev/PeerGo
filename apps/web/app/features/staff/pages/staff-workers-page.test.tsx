import { QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import { MemoryRouter } from "react-router"
import { describe, expect, it } from "vitest"

import { workerOperationsQueryOptions } from "~/features/staff/api/operations.queries"
import { StaffWorkersPage } from "~/features/staff/pages/staff-workers-page"
import {
  createStaffPageQueryClient,
  workerOperationsFixture,
} from "~/features/staff/test-utils/create-staff-page-query-client"

describe("StaffWorkersPage", () => {
  it("shows all worker queues in one administrator view", async () => {
    const queryClient = createStaffPageQueryClient(["operations.monitor.read"])
    queryClient.setQueryData(
      workerOperationsQueryOptions().queryKey,
      workerOperationsFixture()
    )

    render(
      <MemoryRouter initialEntries={["/staff/operations/workers"]}>
        <QueryClientProvider client={queryClient}>
          <StaffWorkersPage />
        </QueryClientProvider>
      </MemoryRouter>
    )

    expect(
      await screen.findByRole("heading", { name: "Worker 状态" })
    ).toBeVisible()
    expect(screen.getByText("统一任务队列")).toBeVisible()
    expect(screen.getByText("做种奖励结算")).toBeVisible()
    expect(screen.getByText("优惠政策投递")).toBeVisible()
    expect(screen.getByText("H&R 政策投递")).toBeVisible()
    expect(screen.getByText("Tracker 控制投影")).toBeVisible()
    expect(screen.getByText("审计事件投递")).toBeVisible()
    expect(screen.getByText("失败待重试")).toBeVisible()
  })
})

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter } from "react-router"
import { describe, expect, it } from "vitest"

import { sessionKeys } from "~/features/auth/api/session.mutations"
import {
  trafficKeys,
  type TrafficOverview,
} from "~/features/traffic/api/traffic.queries"
import { TrafficPage } from "~/features/traffic/pages/traffic-page"

const userId = "0198f20a-6da8-7e51-9c64-111111111111"

describe("TrafficPage", () => {
  it("renders exact totals and expands public-safe settlement segments", async () => {
    const user = userEvent.setup()
    const queryClient = trafficTestClient()
    queryClient.setQueryData(sessionKeys.current(), {
      user: {
        id: userId,
        username: "demo",
        display_name: "PeerGo 演示用户",
        email_verified: true,
      },
      expires_at: "2026-08-10T00:00:00Z",
      csrf_token: "c".repeat(43),
    })
    const overview: TrafficOverview = {
      totals: {
        raw_uploaded_bytes: "9007199254740993",
        raw_downloaded_bytes: "4096",
        credited_uploaded_bytes: "18014398509481986",
        charged_downloaded_bytes: "0",
        entry_count: "1",
        last_settled_at: "2026-08-09T12:01:00Z",
        projection_updated_at: "2026-08-09T12:01:02Z",
      },
      entries: [
        {
          id: "0198f20a-6da8-7e51-9c64-333333333333",
          torrent: {
            id: 42,
            title: "Final Traffic Release",
          },
          interval_started_at: "2026-08-09T11:30:00Z",
          interval_ended_at: "2026-08-09T12:00:00Z",
          raw_uploaded_bytes: "500",
          raw_downloaded_bytes: "300",
          credited_uploaded_bytes: "1000",
          charged_downloaded_bytes: "0",
          explanation: {
            status: "complete",
            segment_count: "2",
            segments: [
              {
                started_at: "2026-08-09T11:30:00Z",
                ended_at: "2026-08-09T11:45:00Z",
                raw_uploaded_bytes: "200",
                raw_downloaded_bytes: "100",
                credited_uploaded_bytes: "400",
                charged_downloaded_bytes: "0",
              },
              {
                started_at: "2026-08-09T11:45:00Z",
                ended_at: "2026-08-09T12:00:00Z",
                raw_uploaded_bytes: "300",
                raw_downloaded_bytes: "200",
                credited_uploaded_bytes: "600",
                charged_downloaded_bytes: "0",
              },
            ],
          },
          settled_at: "2026-08-09T12:01:00Z",
        },
      ],
    }
    queryClient.setQueryData(trafficKeys.current(userId), overview)

    renderTrafficPage(queryClient)

    expect(screen.getByRole("heading", { name: "流量统计" })).toBeVisible()
    expect(
      screen.getByRole("region", { name: "流量汇总" }).querySelector(".grid")
    ).toHaveClass("md:grid-cols-3")
    expect(screen.getByText("实际上传 8 PB")).toBeVisible()
    expect(screen.getByText("16 PB")).toBeVisible()
    expect(screen.getAllByText("Final Traffic Release").length).toBeGreaterThan(
      0
    )
    expect(screen.getAllByText("上传 2×").length).toBeGreaterThan(0)
    expect(screen.getAllByText("免费下载").length).toBeGreaterThan(0)
    expect(screen.queryByText("时段 1")).not.toBeInTheDocument()
    await user.click(screen.getAllByRole("button", { name: /2 个时段/ })[0])
    expect(screen.getByText("时段 1")).toBeVisible()
    expect(screen.getByText("时段 2")).toBeVisible()
    expect(screen.getByText(/合计即本条记录显示的有效流量/)).toBeVisible()
    expect(
      screen.queryByText(/入账上传|计费下载|结算条目|流量账本/)
    ).not.toBeInTheDocument()
    expect(screen.queryByText(/settlement_sha256/i)).not.toBeInTheDocument()
    expect(screen.queryByText(/policy_revision/i)).not.toBeInTheDocument()
  })

  it("shows the formal empty state when Core has no final entries", () => {
    const queryClient = trafficTestClient()
    queryClient.setQueryData(sessionKeys.current(), {
      user: {
        id: userId,
        username: "demo",
        display_name: "PeerGo 演示用户",
        email_verified: true,
      },
      expires_at: "2026-08-10T00:00:00Z",
      csrf_token: "c".repeat(43),
    })
    queryClient.setQueryData(trafficKeys.current(userId), {
      totals: {
        raw_uploaded_bytes: "0",
        raw_downloaded_bytes: "0",
        credited_uploaded_bytes: "0",
        charged_downloaded_bytes: "0",
        entry_count: "0",
        last_settled_at: null,
        projection_updated_at: null,
      },
      entries: [],
    } satisfies TrafficOverview)

    renderTrafficPage(queryClient)

    expect(screen.getByText("还没有流量记录")).toBeVisible()
    expect(screen.getByText("暂无流量记录")).toBeVisible()
  })
})

function trafficTestClient() {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } })
}

function renderTrafficPage(queryClient: QueryClient) {
  render(
    <MemoryRouter>
      <QueryClientProvider client={queryClient}>
        <TrafficPage />
      </QueryClientProvider>
    </MemoryRouter>
  )
}

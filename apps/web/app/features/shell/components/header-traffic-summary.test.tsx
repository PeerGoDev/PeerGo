import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"

import { economyKeys } from "~/features/economy/api/economy.queries"
import { HeaderTrafficSummary } from "~/features/shell/components/header-traffic-summary"
import { trafficKeys } from "~/features/traffic/api/traffic.queries"

describe("HeaderTrafficSummary", () => {
  it("uses the existing settled traffic projection for compact header values", () => {
    const userId = "0198f20a-6da8-7e51-9c64-222222222222"
    const queryClient = new QueryClient()
    queryClient.setQueryData(trafficKeys.current(userId), {
      totals: {
        raw_uploaded_bytes: "3221225472",
        raw_downloaded_bytes: "1073741824",
        credited_uploaded_bytes: "3221225472",
        charged_downloaded_bytes: "1073741824",
        entry_count: "2",
        last_settled_at: "2026-08-11T10:00:00Z",
        projection_updated_at: "2026-08-11T10:00:01Z",
      },
      entries: [],
    })
    queryClient.setQueryData(economyKeys.current(userId), {
      magic_balance: "20403550",
      magic_updated_at: "2026-08-11T10:00:01Z",
      magic_entries: [],
      progress: {
        experience: "12000.00000000000000000000",
        level: 7,
        policy_version: "rousi-v1",
        current_minimum_experience: "10000.00000000000000000000",
        next: {
          level: 8,
          minimum_experience: "15000.00000000000000000000",
        },
        updated_at: "2026-08-11T10:00:01Z",
      },
      experience_entries: [],
    })

    render(
      <QueryClientProvider client={queryClient}>
        <HeaderTrafficSummary userId={userId} trafficEnabled economyEnabled />
      </QueryClientProvider>
    )

    expect(screen.getByText("分享率")).toBeInTheDocument()
    expect(screen.getByText("3.000")).toHaveClass("text-success-foreground")
    expect(screen.getByText("上传")).toBeInTheDocument()
    expect(screen.getByText("3 GB")).toBeInTheDocument()
    expect(screen.getByText("下载")).toBeInTheDocument()
    expect(screen.getByText("1 GB")).toBeInTheDocument()
    expect(screen.getByText("等级")).toBeInTheDocument()
    expect(screen.getByText("Lv.7")).toBeInTheDocument()
    expect(screen.getByText("魔力值")).toBeInTheDocument()
    expect(screen.getByText("20.4M")).toBeInTheDocument()
    expect(screen.getByLabelText("20,403,550 魔力值")).toBeInTheDocument()
    expect(screen.queryByText("PT币")).not.toBeInTheDocument()
    expect(screen.queryByText("—")).not.toBeInTheDocument()
    expect(screen.queryByText("实际上传")).not.toBeInTheDocument()
    expect(screen.queryByText("结算数")).not.toBeInTheDocument()
    expect(
      document.querySelector("svg.text-success-foreground")
    ).toBeInTheDocument()
    expect(document.querySelector("svg.text-destructive")).toBeInTheDocument()
    expect(
      document.querySelector("svg.text-warning-foreground")
    ).toBeInTheDocument()
  })
})

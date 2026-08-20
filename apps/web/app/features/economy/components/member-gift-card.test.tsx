import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"

import { memberGiftOverviewQueryOptions } from "~/features/economy/api/member-gifts.queries"
import { MemberGiftCard } from "~/features/economy/components/member-gift-card"

describe("MemberGiftCard", () => {
  it("shows the numeric member id, disabled policy, quota, and exact history amounts", async () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false, staleTime: Infinity } },
    })
    queryClient.setQueryData(
      memberGiftOverviewQueryOptions("member-1").queryKey,
      {
        policy: {
          revision: "member-gift-disabled-v1",
          settings: {
            enabled: false,
            minimum_amount: "1",
            maximum_amount: "10000",
            daily_gross_limit: "20000",
            fee_bps: 0,
          },
          snapshot_sha256:
            "79cccf4c56fd285411837109cd62b9cbea43009a23a7db908b36932b396dd1b7",
          reason: "PeerGo 升级基线默认关闭成员赠送",
          created_at: "2026-08-17T00:00:00Z",
        },
        my_numeric_id: "12327",
        outgoing_today: "2000",
        remaining_today: "18000",
        history: [
          {
            id: "0198f20a-6da8-7e51-9c64-222222222222",
            direction: "received",
            counterparty: {
              numeric_id: "42",
              username: "member42",
              display_name: "四十二号成员",
            },
            gross_amount: "9007199254740993",
            fee_amount: "1",
            net_amount: "9007199254740992",
            message: "感谢保种",
            policy_revision: "member-gift-disabled-v1",
            occurred_at: "2026-08-17T08:00:00Z",
          },
        ],
      }
    )

    render(
      <QueryClientProvider client={queryClient}>
        <MemberGiftCard
          userId="member-1"
          csrfToken={"c".repeat(43)}
          magicBalance="9007199254740993"
        />
      </QueryClientProvider>
    )

    expect(await screen.findByText("我的 ID：12327")).toBeVisible()
    expect(screen.getByText("今日剩余 18,000")).toBeVisible()
    expect(screen.getByText("成员赠送暂未开放")).toBeVisible()
    expect(screen.getByText("四十二号成员")).toBeVisible()
    expect(screen.getByText("#42 · member42")).toBeVisible()
    expect(screen.getByText("+9,007,199,254,740,992")).toBeVisible()
    expect(
      screen.queryByRole("button", { name: "确认赠送" })
    ).not.toBeInTheDocument()
  })
})

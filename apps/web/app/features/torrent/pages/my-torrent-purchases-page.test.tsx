import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import { MemoryRouter } from "react-router"
import { describe, expect, it } from "vitest"

import { sessionKeys } from "~/features/auth/api/session.mutations"
import { capabilityKeys } from "~/features/authz/api/capabilities.queries"
import { myTorrentPurchasesQueryOptions } from "~/features/torrent/api/torrent-purchases.queries"
import { MyTorrentPurchasesPage } from "~/features/torrent/pages/my-torrent-purchases-page"

const userId = "0198f20a-6da8-7e51-9c64-333333333333"

describe("MyTorrentPurchasesPage", () => {
  it("renders immutable migrated purchase history with numeric torrent links", async () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false, staleTime: Infinity } },
    })
    queryClient.setQueryData(sessionKeys.current(), {
      user: {
        id: userId,
        username: "member",
        display_name: "普通用户",
        email_verified: true,
      },
      expires_at: "2026-08-17T00:00:00Z",
      csrf_token: "c".repeat(43),
    })
    queryClient.setQueryData(capabilityKeys.current(userId), {
      policy_version: "2026-08-16",
      items: [{ action: "torrent.purchase.read.self" }],
    })
    queryClient.setQueryData(
      myTorrentPurchasesQueryOptions(userId, 20, 0).queryKey,
      {
        items: [
          {
            torrent_id: 1234,
            title: "演示种子 2026 2160p WEB-DL",
            category_name: "电影",
            torrent_state: "published",
            price: "5000",
            purchased_at: "2026-08-14T08:00:00Z",
            legacy_import: true,
          },
        ],
        total: 1,
        limit: 20,
        offset: 0,
      }
    )

    render(
      <MemoryRouter initialEntries={["/account/purchases"]}>
        <QueryClientProvider client={queryClient}>
          <MyTorrentPurchasesPage />
        </QueryClientProvider>
      </MemoryRouter>
    )

    expect(
      await screen.findByRole("heading", { name: /已购种子/ })
    ).toBeVisible()
    expect(screen.getByText("(1)")).toBeVisible()
    expect(screen.getAllByText("旧站继承")).toHaveLength(2)
    expect(screen.getByText("5,000")).toBeVisible()
    expect(
      screen.getAllByRole("link", { name: "演示种子 2026 2160p WEB-DL" })[0]
    ).toHaveAttribute("href", "/torrents/1234")
  })
})

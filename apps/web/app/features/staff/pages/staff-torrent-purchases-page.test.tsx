import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter } from "react-router"
import { describe, expect, it } from "vitest"

import { sessionKeys } from "~/features/auth/api/session.mutations"
import { capabilityKeys } from "~/features/authz/api/capabilities.queries"
import { staffSessionKeys } from "~/features/staff/api/staff-session.mutations"
import { managedTorrentPurchaseListQueryOptions } from "~/features/staff/api/torrent-purchase-administration.queries"
import { StaffTorrentPurchasesPage } from "~/features/staff/pages/staff-torrent-purchases-page"

const administratorId = "0198f20a-6da8-7e51-9c64-444444444444"

describe("StaffTorrentPurchasesPage", () => {
  it("renders numeric purchase identities and opens the bounded refund review", async () => {
    const user = userEvent.setup()
    const queryClient = createQueryClient()

    render(
      <MemoryRouter initialEntries={["/staff/content/torrent-purchases"]}>
        <QueryClientProvider client={queryClient}>
          <StaffTorrentPurchasesPage />
        </QueryClientProvider>
      </MemoryRouter>
    )

    expect(
      await screen.findByRole("heading", { name: "购买记录" })
    ).toBeVisible()
    expect(screen.getAllByText(/#327/).length).toBeGreaterThan(0)
    expect(screen.getAllByText(/#1234/).length).toBeGreaterThan(0)
    expect(screen.getAllByText("旧站继承")).toHaveLength(2)
    expect(screen.getByText("5,000")).toBeVisible()

    await user.click(
      screen.getAllByRole("button", {
        name: "退款用户 327 的种子 1234",
      })[0]
    )
    expect(await screen.findByRole("alertdialog")).toBeVisible()
    expect(
      screen.getByRole("heading", { name: "确认退款并撤销购买权限？" })
    ).toBeVisible()
    expect(screen.getByText(/退款由站点承担/)).toBeVisible()
    expect(screen.getByRole("button", { name: "确认退款" })).toBeEnabled()
  })
})

function createQueryClient() {
  const filters = {
    query: "",
    status: "all" as const,
    source: "all" as const,
    page: 1,
    pageSize: 20,
  }
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  })
  queryClient.setQueryData(sessionKeys.current(), {
    user: {
      id: administratorId,
      username: "admin",
      display_name: "管理员",
      email_verified: true,
    },
    expires_at: "2026-08-17T00:00:00Z",
    csrf_token: "c".repeat(43),
  })
  queryClient.setQueryData(capabilityKeys.current(administratorId), {
    policy_version: "2026-08-16",
    items: [],
  })
  queryClient.setQueryData(staffSessionKeys.current(), {
    user: {
      id: administratorId,
      username: "admin",
      display_name: "管理员",
      email_verified: true,
    },
    expires_at: "2026-08-17T00:00:00Z",
    webauthn_authenticated_at: "2026-08-16T05:00:00Z",
    csrf_token: "s".repeat(43),
  })
  queryClient.setQueryData(staffSessionKeys.capabilities(administratorId), {
    policy_version: "2026-08-16",
    items: [
      "torrent.purchase.manage.read",
      "torrent.purchase.manage.refund",
    ].map((action) => ({
      action,
      description: action,
      scope: { type: "site", id: "peergo" },
      expires_at: "2026-08-17T00:00:00Z",
    })),
  })
  queryClient.setQueryData(
    managedTorrentPurchaseListQueryOptions(filters).queryKey,
    {
      items: [
        {
          buyer_numeric_id: 327,
          buyer_username: "member",
          buyer_display_name: "普通用户",
          seller_numeric_id: 1,
          seller_username: "uploader",
          torrent_id: 1234,
          torrent_title: "演示种子 2026 2160p WEB-DL",
          category_name: "电影",
          source: "legacy_import",
          status: "active",
          price: "5000",
          tax: "500",
          seller_income: "4500",
          purchased_at: "2026-08-14T08:00:00Z",
        },
      ],
      total: 1,
      limit: 20,
      offset: 0,
    }
  )
  return queryClient
}

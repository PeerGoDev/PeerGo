import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter } from "react-router"
import { describe, expect, it } from "vitest"

import { sessionKeys } from "~/features/auth/api/session.mutations"
import { capabilityKeys } from "~/features/authz/api/capabilities.queries"
import { staffSessionKeys } from "~/features/staff/api/staff-session.mutations"
import { managedTorrentListQueryOptions } from "~/features/staff/api/torrent-administration.queries"
import { StaffTorrentsPage } from "~/features/staff/pages/staff-torrents-page"

const administratorId = "0198f20a-6da8-7e51-9c64-111111111111"

describe("StaffTorrentsPage", () => {
  it("renders the compact lifecycle directory with numeric identities and safe actions", async () => {
    const user = userEvent.setup()
    const queryClient = createQueryClient()

    render(
      <MemoryRouter initialEntries={["/staff/content/torrents"]}>
        <QueryClientProvider client={queryClient}>
          <StaffTorrentsPage />
        </QueryClientProvider>
      </MemoryRouter>
    )

    expect(
      await screen.findByRole("heading", { name: "种子管理" })
    ).toBeVisible()
    expect(screen.getByLabelText("搜索种子 ID、名称或上传者")).toHaveAttribute(
      "placeholder",
      "搜索 ID / 名称 / 上传者..."
    )
    expect(
      screen.getByRole("combobox", { name: "按种子分类筛选" })
    ).toBeVisible()
    expect(screen.getAllByText("1234").length).toBeGreaterThan(0)
    expect(screen.getAllByText("用户 #327").length).toBeGreaterThan(0)
    expect(screen.getAllByText("2X免费").length).toBeGreaterThan(0)
    expect(screen.getAllByText("已发布").length).toBeGreaterThan(0)
    expect(
      screen.getAllByRole("link", { name: /演示种子/ })[0]
    ).toHaveAttribute("href", "/torrents/1234")

    await user.click(
      screen.getAllByRole("button", { name: "下架种子 1234" })[0]
    )
    expect(await screen.findByRole("alertdialog")).toBeVisible()
    expect(screen.getByRole("heading", { name: "下架种子" })).toBeVisible()
    expect(screen.getByText(/Tracker 准入会按同一版本关闭/)).toBeVisible()
    expect(screen.getByRole("button", { name: "确认下架" })).toBeEnabled()
  })
})

function createQueryClient() {
  const filters = {
    query: "",
    state: "all" as const,
    categoryId: "",
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
    expires_at: "2026-08-16T00:00:00Z",
    csrf_token: "c".repeat(43),
  })
  queryClient.setQueryData(capabilityKeys.current(administratorId), {
    policy_version: "policy-2026-08-15",
    items: [],
  })
  queryClient.setQueryData(staffSessionKeys.current(), {
    user: {
      id: administratorId,
      username: "admin",
      display_name: "管理员",
      email_verified: true,
    },
    expires_at: "2026-08-16T00:00:00Z",
    webauthn_authenticated_at: "2026-08-15T08:00:00Z",
    csrf_token: "s".repeat(43),
  })
  queryClient.setQueryData(staffSessionKeys.capabilities(administratorId), {
    policy_version: "policy-2026-08-15",
    items: [
      "torrent.manage.read",
      "torrent.lifecycle.update",
      "torrent.purchase.manage.update",
    ].map((action) => ({
      action,
      description: action,
      scope: { type: "site", id: "peergo" },
      expires_at: "2026-08-16T00:00:00Z",
    })),
  })
  queryClient.setQueryData(managedTorrentListQueryOptions(filters).queryKey, {
    items: [
      {
        id: 1234,
        uploader_numeric_id: 327,
        uploader_username: "uploader",
        uploader_display_name: "发布员",
        category_id: "movies",
        category_name: "电影",
        title: "演示种子 2026 2160p WEB-DL",
        subtitle: "用于验证后台种子管理工作台",
        total_size_bytes: 17_179_869_184,
        purchase_price: "120",
        state: "published",
        version: 7,
        promotion: "double_upload_free",
        promotion_ends_at: "2026-08-16T00:00:00Z",
        seeders: 28,
        leechers: 3,
        completed: 109,
        submitted_at: "2026-08-14T07:00:00Z",
        published_at: "2026-08-14T08:00:00Z",
        state_changed_at: "2026-08-14T08:00:00Z",
        updated_at: "2026-08-15T08:00:00Z",
      },
    ],
    categories: [{ id: "movies", name: "电影", enabled: true }],
    state_counts: {
      pending_review: 2,
      published: 8999,
      rejected: 3,
      disabled: 4,
      deleted: 5,
    },
    total: 8999,
    limit: 20,
    offset: 0,
  })
  return queryClient
}

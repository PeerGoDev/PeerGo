import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter } from "react-router"
import { describe, expect, it } from "vitest"

import { sessionKeys } from "~/features/auth/api/session.mutations"
import { capabilityKeys } from "~/features/authz/api/capabilities.queries"
import { categoryAdministrationKeys } from "~/features/staff/api/category-administration.queries"
import { staffSessionKeys } from "~/features/staff/api/staff-session.mutations"
import { StaffCategoriesPage } from "~/features/staff/pages/staff-categories-page"

const userId = "0198f20a-6da8-7e51-9c64-666666666666"

describe("StaffCategoriesPage", () => {
  it("keeps Rousi category density while exposing PeerGo category facts", async () => {
    const user = userEvent.setup()
    const queryClient = createQueryClient()

    render(
      <MemoryRouter initialEntries={["/staff/content/categories"]}>
        <QueryClientProvider client={queryClient}>
          <StaffCategoriesPage />
        </QueryClientProvider>
      </MemoryRouter>
    )

    expect(
      await screen.findByRole("heading", { name: "分类管理" })
    ).toHaveClass("text-xl", "font-semibold")
    expect(screen.getByRole("button", { name: "新建分类" })).toHaveClass("w-26")
    expect(screen.getByRole("button", { name: "刷新" })).toHaveClass("w-19")
    expect(screen.getByText(/点击分类可直接展开类型/)).toBeVisible()

    const expandMovie = screen.getByRole("button", {
      name: "展开 电影 的类型与属性",
    })
    const movieCard = expandMovie.closest('[data-slot="card"]') as HTMLElement
    expect(movieCard).toBeVisible()
    expect(movieCard.querySelector('[data-slot="card-header"]')).toHaveClass(
      "min-h-[62px]"
    )
    expect(screen.getByText("(movie)")).toBeVisible()
    expect(screen.getByText("1,337 个种子")).toBeVisible()
    expect(movieCard.querySelector('[data-category-icon="movie"]')).toHaveClass(
      "lucide-film"
    )
    expect(
      screen.getAllByRole("button", { name: "编辑分类 电影" })[0]
    ).toBeVisible()

    await user.click(expandMovie)

    expect(
      within(movieCard).getByRole("heading", {
        name: "电影 · 类型与属性",
      })
    ).toBeVisible()
    expect(within(movieCard).getByText("剧情")).toBeVisible()
    expect(
      screen.getByRole("button", { name: "收起 电影 的类型与属性" })
    ).toBeVisible()
  })
})

function createQueryClient() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  })
  queryClient.setQueryData(sessionKeys.current(), {
    user: {
      id: userId,
      username: "admin",
      display_name: "管理员",
      email_verified: true,
    },
    expires_at: "2026-08-15T00:00:00Z",
    csrf_token: "c".repeat(43),
  })
  queryClient.setQueryData(capabilityKeys.current(userId), {
    policy_version: "2026-08-14",
    items: [],
  })
  queryClient.setQueryData(staffSessionKeys.current(), {
    user: {
      id: userId,
      username: "admin",
      display_name: "管理员",
      email_verified: true,
    },
    expires_at: "2026-08-15T00:00:00Z",
    webauthn_authenticated_at: "2026-08-14T08:00:00Z",
    csrf_token: "s".repeat(43),
  })
  queryClient.setQueryData(staffSessionKeys.capabilities(userId), {
    policy_version: "2026-08-14",
    items: [
      {
        action: "category.manage.read",
        description: "读取分类",
        scope: { type: "site", id: "peergo" },
        expires_at: "2026-08-15T00:00:00Z",
      },
      {
        action: "category.create",
        description: "创建分类",
        scope: { type: "site", id: "peergo" },
        expires_at: "2026-08-15T00:00:00Z",
      },
      {
        action: "category.update",
        description: "更新分类",
        scope: { type: "site", id: "peergo" },
        expires_at: "2026-08-15T00:00:00Z",
      },
    ],
  })
  queryClient.setQueryData(categoryAdministrationKeys.list(), [
    {
      id: "movie",
      name: "电影",
      display_order: 10,
      enabled: true,
      torrent_count: 1337,
      version: 3,
      created_at: "2026-08-01T00:00:00Z",
      updated_at: "2026-08-14T00:00:00Z",
      facets: [
        {
          id: "genre",
          name: "类型",
          selection_mode: "multi_option",
          required: true,
          display_order: 10,
          enabled: true,
          version: 2,
          torrent_count: 1337,
          created_at: "2026-08-01T00:00:00Z",
          updated_at: "2026-08-14T00:00:00Z",
          options: [
            {
              key: "drama",
              label: "剧情",
              canonical_label: "剧情",
              display_order: 10,
              enabled: true,
              version: 1,
              torrent_count: 900,
              created_at: "2026-08-01T00:00:00Z",
              updated_at: "2026-08-14T00:00:00Z",
            },
          ],
        },
      ],
    },
  ])
  return queryClient
}

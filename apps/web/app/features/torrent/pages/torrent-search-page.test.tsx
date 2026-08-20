import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter, Route, Routes } from "react-router"
import { beforeEach, describe, expect, it } from "vitest"

import { TooltipProvider } from "~/components/ui/tooltip"
import { sessionKeys } from "~/features/auth/api/session.mutations"
import { capabilityKeys } from "~/features/authz/api/capabilities.queries"
import {
  torrentKeys,
  torrentListQueryOptions,
} from "~/features/torrent/api/torrent.queries"
import { TorrentSearchPage } from "~/features/torrent/pages/torrent-search-page"

const userId = "0198f20a-6da8-7e51-9c64-111111111111"

describe("TorrentSearchPage", () => {
  beforeEach(() => globalThis.localStorage.clear())

  it("matches the separate Rousi search flow and renders real results", async () => {
    const user = userEvent.setup()
    const queryClient = createQueryClient()
    queryClient.setQueryData(
      torrentListQueryOptions({
        query: "Orbit",
        searchScope: "title_subtitle",
        categoryId: "",
        sort: "published_desc",
        limit: 25,
        offset: 0,
      }).queryKey,
      {
        items: [
          {
            id: 42,
            name: "Orbit 2026 2160p WEB-DL",
            subtitle: "搜索结果副标题",
            category: { id: "movies", name: "电影" },
            size_bytes: 4096,
            seeders: 8,
            leechers: 2,
            completed: 18,
            promotion: "none",
            sticky_until: null,
            uploaded_at: "2026-08-11T10:00:00Z",
            swarm_observed_at: "2026-08-11T10:00:00Z",
            swarm_stale: false,
          },
        ],
        total: 1,
        limit: 25,
        offset: 0,
      }
    )

    renderSearchPage(queryClient, "/search?q=Orbit")

    expect(
      screen.getByRole("heading", { level: 1, name: "搜索种子" })
    ).toBeVisible()
    const resultSummary = screen.getByText(
      (_content, element) =>
        element?.tagName === "P" &&
        element.textContent?.includes("共找到 1 个结果") === true
    )
    expect(resultSummary).toBeVisible()
    expect(resultSummary.closest("section")).toHaveClass("gap-6")
    expect(
      screen.getAllByText("Orbit 2026 2160p WEB-DL").length
    ).toBeGreaterThan(0)

    await user.click(screen.getByRole("button", { name: "高级筛选" }))
    expect(screen.getByRole("combobox", { name: "搜索范围" })).toBeVisible()
    expect(screen.getByText("搜索范围")).toHaveClass("leading-none")
    expect(screen.getByRole("combobox", { name: "分类" })).toBeVisible()
    expect(screen.getByRole("combobox", { name: "排序" })).toBeVisible()
  })

  it("keeps the popular-search card truthful before aggregate data exists", () => {
    renderSearchPage(createQueryClient(), "/search")

    expect(
      screen.getByRole("textbox", { name: "搜索种子标题、副标题" })
    ).toHaveClass("text-base", "md:text-base")
    expect(screen.getByRole("button", { name: "搜索" })).toHaveClass("w-23")
    expect(screen.getByRole("button", { name: "高级筛选" })).toHaveClass("w-32")
    expect(screen.getByRole("heading", { name: "热门搜索" })).toBeVisible()
    expect(
      screen
        .getByRole("heading", { name: "热门搜索" })
        .closest('[data-slot="card"]')
    ).toHaveClass("md:min-h-[246px]")
    expect(screen.queryByText("暂无热门搜索")).not.toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "4K HDR 杜比视界" })).toBeNull()
    expect(screen.queryByLabelText("正在加载种子")).not.toBeInTheDocument()
  })

  it("keeps the former PtYes search history and clears the PeerGo view", async () => {
    const user = userEvent.setup()
    globalThis.localStorage.setItem(
      "pt_search_history",
      JSON.stringify(["旧站关键词", "保留关键词"])
    )

    renderSearchPage(createQueryClient(), "/search")

    expect(screen.getByRole("heading", { name: "搜索历史" })).toBeVisible()
    const oldTerm = screen.getByRole("button", { name: "旧站关键词" })
    expect(oldTerm).toBeVisible()
    expect(oldTerm.parentElement).toHaveClass(
      "bg-secondary",
      "text-secondary-foreground"
    )
    expect(
      screen.getByRole("button", { name: "删除搜索历史 旧站关键词" })
    ).toBeVisible()

    await user.click(screen.getByRole("button", { name: "清空" }))

    expect(
      screen.queryByRole("button", { name: "旧站关键词" })
    ).not.toBeInTheDocument()
    expect(
      JSON.parse(
        globalThis.localStorage.getItem("peergo.torrent-search.history.v1") ??
          "[]"
      )
    ).toEqual([])
    expect(globalThis.localStorage.getItem("pt_search_history")).not.toBeNull()
  })
})

function createQueryClient() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  })
  queryClient.setQueryData(sessionKeys.current(), {
    user: {
      id: userId,
      username: "demo",
      display_name: "PeerGo 演示用户",
      email_verified: true,
    },
    expires_at: "2026-08-12T00:00:00Z",
    csrf_token: "c".repeat(43),
  })
  queryClient.setQueryData(capabilityKeys.current(userId), {
    policy_version: "2026-08-12",
    items: [],
  })
  queryClient.setQueryData(torrentKeys.categories(), [
    { id: "movies", name: "电影", torrent_count: 12 },
    { id: "tv", name: "电视剧", torrent_count: 30 },
  ])
  return queryClient
}

function renderSearchPage(queryClient: QueryClient, entry: string) {
  return render(
    <MemoryRouter initialEntries={[entry]}>
      <QueryClientProvider client={queryClient}>
        <TooltipProvider>
          <Routes>
            <Route path="/search" element={<TorrentSearchPage />} />
          </Routes>
        </TooltipProvider>
      </QueryClientProvider>
    </MemoryRouter>
  )
}

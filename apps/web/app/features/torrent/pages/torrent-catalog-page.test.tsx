import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { fireEvent, render, screen, within } from "@testing-library/react"
import { MemoryRouter, Route, Routes } from "react-router"
import { describe, expect, it } from "vitest"

import { TooltipProvider } from "~/components/ui/tooltip"
import { sessionKeys } from "~/features/auth/api/session.mutations"
import { capabilityKeys } from "~/features/authz/api/capabilities.queries"
import { siteKeys } from "~/features/site/api/site.queries"
import {
  torrentKeys,
  torrentListQueryOptions,
} from "~/features/torrent/api/torrent.queries"
import { TorrentCatalogPage } from "~/features/torrent/pages/torrent-catalog-page"

const userId = "0198f20a-6da8-7e51-9c64-111111111111"

describe("TorrentCatalogPage", () => {
  it("renders real category counts, URL filters, results and pagination", () => {
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
      policy_version: "2026-08-11",
      items: [],
    })
    queryClient.setQueryData(siteKeys.info(), {
      name: "PeerGo",
      description: "私有分享社区",
      registration_mode: "invite",
      registration_username_min_characters: 3,
      registration_username_max_characters: 20,
      registration_email_domain_mode: "any",
      human_verification: disabledHumanVerification,
      online_users: 222,
      default_torrent_view: "list",
      show_latest_announcement: true,
    })
    queryClient.setQueryData(torrentKeys.categories(), [
      { id: "movies", name: "电影", torrent_count: 12 },
      { id: "tv", name: "电视剧", torrent_count: 30 },
      { id: "documentaries", name: "纪录片", torrent_count: 0 },
    ])
    const filters = {
      query: "release",
      categoryId: "movies",
      promotion: "free" as const,
      limit: 25,
      offset: 0,
    }
    queryClient.setQueryData(torrentListQueryOptions(filters).queryKey, {
      items: [
        {
          id: 42,
          name: "Release One 2026 1080p",
          subtitle: "首个目录结果",
          category: { id: "movies", name: "电影" },
          size_bytes: 4096,
          seeders: 8,
          leechers: 2,
          completed: 18,
          promotion: "free",
          sticky_until: null,
          uploaded_at: "2026-08-11T10:00:00Z",
          swarm_observed_at: "2026-08-11T10:00:00Z",
          swarm_stale: false,
        },
      ],
      total: 60,
      limit: 25,
      offset: 0,
    })
    queryClient.setQueryData(
      torrentListQueryOptions({ ...filters, limit: 100 }).queryKey,
      {
        items: [],
        total: 60,
        limit: 100,
        offset: 0,
      }
    )

    render(
      <MemoryRouter
        initialEntries={["/torrents?q=release&category=movies&promotion=free"]}
      >
        <QueryClientProvider client={queryClient}>
          <TooltipProvider>
            <Routes>
              <Route path="/torrents" element={<TorrentCatalogPage />} />
            </Routes>
          </TooltipProvider>
        </QueryClientProvider>
      </MemoryRouter>
    )

    expect(screen.getByRole("button", { name: /电影\s*12/ })).toHaveAttribute(
      "aria-pressed",
      "true"
    )
    expect(screen.getByRole("button", { name: /电影\s*12/ })).toHaveClass(
      "bg-card",
      "aria-pressed:border-primary",
      "aria-pressed:bg-primary"
    )
    expect(screen.getByRole("button", { name: /纪录片\s*0/ })).toBeVisible()
    const resultsHeading = screen.getByRole("heading", {
      name: /搜索结果\s*共\s*60\s*个种子/,
    })
    expect(resultsHeading).toBeVisible()
    expect(resultsHeading.closest("section")).toHaveClass("gap-3")
    expect(
      screen.getAllByText("Release One 2026 1080p").length
    ).toBeGreaterThan(0)

    // The table card carries the desktop pagination; phones get a copy below
    // the card list.
    const [desktopPagination] = screen.getAllByRole("navigation", {
      name: "种子目录分页",
    })
    expect(desktopPagination.closest('[data-slot="card"]')).toHaveClass(
      "md:flex"
    )
    const pagination = within(desktopPagination)
    expect(pagination.getByText("第 1 / 3 页")).toBeVisible()
    expect(pagination.getByRole("button", { name: "第 1 页" })).toHaveAttribute(
      "aria-current",
      "page"
    )
    expect(pagination.getByRole("button", { name: "上一页" })).toBeDisabled()
    expect(pagination.getByRole("button", { name: "下一页" })).toBeEnabled()
    expect(
      pagination.getByRole("spinbutton", { name: "跳转页码" })
    ).toBeVisible()
    expect(pagination.getByRole("combobox", { name: "每页条数" })).toHaveValue(
      "25"
    )
    expect(pagination.getByRole("option", { name: "100" })).toBeVisible()
    fireEvent.change(pagination.getByRole("combobox", { name: "每页条数" }), {
      target: { value: "100" },
    })
    expect(
      within(
        screen.getAllByRole("navigation", { name: "种子目录分页" })[0]
      ).getByRole("combobox", { name: "每页条数" })
    ).toHaveValue("100")
    expect(
      screen.getByRole("combobox", { name: "促销" }).parentElement
    ).toHaveClass("max-sm:w-full", "sm:w-36")
    expect(screen.getByRole("button", { name: "搜索" })).toHaveClass(
      "w-16",
      "shrink-0"
    )
    expect(screen.getByRole("search")).toHaveClass("items-end", "gap-2.5")
    expect(screen.getByText("促销")).toHaveClass("text-2xs", "leading-none")
  })
})

const disabledHumanVerification = {
  provider: "disabled" as const,
  site_key: "",
  registration_enabled: false,
  login_enabled: false,
  password_recovery_enabled: false,
}

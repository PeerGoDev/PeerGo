import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { fireEvent, render, screen } from "@testing-library/react"
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
      "bg-background",
      "aria-pressed:border-transparent",
      "dark:aria-pressed:bg-primary",
      "dark:aria-pressed:text-primary-foreground"
    )
    expect(screen.getByRole("button", { name: /纪录片\s*0/ })).toBeVisible()
    expect(screen.getByRole("heading", { name: "共 60 个种子" })).toBeVisible()
    expect(
      screen.getByRole("heading", { name: "共 60 个种子" }).closest("section")
    ).toHaveClass("gap-2.5", "sm:gap-4")
    expect(
      screen.getAllByText("Release One 2026 1080p").length
    ).toBeGreaterThan(0)
    expect(screen.getByText("第 1 页 / 共 3 页")).toBeVisible()
    expect(screen.getByRole("button", { name: "上一页" })).toBeDisabled()
    expect(screen.getByRole("button", { name: "下一页" })).toBeEnabled()
    expect(screen.getByRole("spinbutton", { name: "跳转页码" })).toBeVisible()
    expect(screen.getByRole("combobox", { name: "每页条数" })).toHaveValue("25")
    expect(screen.getByRole("option", { name: "100" })).toBeVisible()
    fireEvent.change(screen.getByRole("combobox", { name: "每页条数" }), {
      target: { value: "100" },
    })
    expect(screen.getByRole("combobox", { name: "每页条数" })).toHaveValue(
      "100"
    )
    expect(
      screen.getByRole("combobox", { name: "促销" }).parentElement
    ).toHaveClass(
      "w-[calc((100%_-_0.75rem)/2)]",
      "md:w-[calc((100%_-_1.5rem)/3)]",
      "lg:w-[calc((100%_-_2.25rem)/4)]",
      "xl:w-[calc((100%_-_3.75rem)/6)]"
    )
    expect(screen.getByRole("button", { name: "搜索" })).toHaveClass(
      "w-15",
      "px-0"
    )
    expect(screen.getByRole("search")).toHaveClass("gap-y-3", "sm:gap-y-4")
    expect(screen.getByText("促销")).toHaveClass("text-xs", "leading-none")
  })
})

const disabledHumanVerification = {
  provider: "disabled" as const,
  site_key: "",
  registration_enabled: false,
  login_enabled: false,
  password_recovery_enabled: false,
}

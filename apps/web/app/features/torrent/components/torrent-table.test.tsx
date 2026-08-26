import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { fireEvent, render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { MemoryRouter } from "react-router"

import { TooltipProvider } from "~/components/ui/tooltip"
import { TorrentTable } from "~/features/torrent/components/torrent-table"

describe("TorrentTable", () => {
  it("shows the current user's bounded download progress on a matching torrent row", () => {
    const queryClient = new QueryClient()
    render(
      <MemoryRouter>
        <QueryClientProvider client={queryClient}>
          <TooltipProvider>
            <TorrentTable
              torrents={[
                {
                  id: 42,
                  name: "Downloaded Release",
                  subtitle: "Resume this transfer",
                  category: { id: "movies", name: "电影" },
                  size_bytes: 4096,
                  seeders: 1,
                  leechers: 1,
                  completed: 1,
                  promotion: "none",
                  sticky_until: null,
                  uploaded_at: "2026-08-12T10:00:00Z",
                  swarm_observed_at: "2026-08-12T10:00:00Z",
                  swarm_stale: false,
                },
              ]}
              activityByTorrentId={
                new Map([
                  [
                    42,
                    {
                      torrent: { id: 42, title: "Downloaded Release" },
                      total_size_bytes: 4096,
                      raw_uploaded_bytes: "0",
                      raw_downloaded_bytes: "2048",
                      progress_basis_points: 5000,
                      completed: false,
                      last_settled_at: "2026-08-12T10:00:00Z",
                    },
                  ],
                ])
              }
            />
          </TooltipProvider>
        </QueryClientProvider>
      </MemoryRouter>
    )

    const progress = screen.getByRole("progressbar", {
      name: "下载进度 50.0%",
    })
    expect(progress).toBeVisible()
    expect(progress).toHaveAttribute("aria-valuenow", "50")
    expect(progress).toHaveClass("absolute", "bottom-0")
    expect(screen.getByText("Downloaded Release").closest("tr")).toHaveClass(
      "relative"
    )
  })

  it("renders promotion and known stale values without repeating a row badge", () => {
    const queryClient = new QueryClient({
      defaultOptions: { mutations: { retry: false } },
    })
    render(
      <MemoryRouter>
        <QueryClientProvider client={queryClient}>
          <TooltipProvider>
            <TorrentTable
              torrents={[
                {
                  id: 41,
                  name: "Example Release",
                  subtitle: "Synthetic fixture",
                  category: { id: "movies", name: "电影" },
                  size_bytes: 1_073_741_824,
                  seeders: 12,
                  leechers: 3,
                  completed: 40,
                  promotion: "free",
                  sticky_until: null,
                  uploaded_at: "2026-08-05T10:00:00Z",
                  swarm_observed_at: "2026-08-05T09:00:00Z",
                  swarm_stale: true,
                },
              ]}
            />
          </TooltipProvider>
        </QueryClientProvider>
      </MemoryRouter>
    )

    expect(screen.getByText("Example Release")).toBeInTheDocument()
    const table = screen.getByRole("table")
    expect(table).toHaveClass("block", "min-w-0")
    expect(table.closest("[data-slot=card]")).toHaveClass("hidden", "md:flex")
    expect(table.querySelector("thead")).toHaveClass("block")
    expect(screen.getByText("名称").closest("tr")).toHaveClass(
      "rounded-lg",
      "bg-muted"
    )
    expect(screen.getByText("名称").closest("tr")).toHaveClass(
      "grid-cols-[48px_minmax(0,1fr)_70px_70px_130px_90px_40px]"
    )
    expect(screen.getByText("Example Release").closest("tr")).toHaveClass(
      "px-3.5",
      "py-2.5"
    )
    expect(screen.getByText("Example Release").closest("tr")).not.toHaveClass(
      "border-t!"
    )
    expect(screen.getByText("Example Release")).not.toHaveClass("text-base")
    expect(screen.getByText("免费")).toBeInTheDocument()
    expect(screen.getByLabelText("做种数 / 下载数 / 完成数")).toBeVisible()
    expect(screen.getByText("1")).toBeVisible()
    expect(screen.getByText("GB")).toHaveClass("text-success-foreground")
    expect(screen.queryByText("统计延迟")).not.toBeInTheDocument()
    expect(screen.getByTitle("做种数")).toHaveTextContent("12")
    const downloadButton = screen.getByRole("button", {
      name: "下载“Example Release”的种子文件",
    })
    expect(downloadButton).toHaveAttribute("aria-disabled", "false")
    expect(downloadButton).toHaveClass("size-[22px]", "border-0", "p-1")

    fireEvent.error(screen.getByRole("img", { name: "Example Release封面" }))
    expect(
      screen.getByRole("img", { name: "Example Release暂无封面" })
    ).toHaveClass("from-neutral-100", "to-neutral-300")
  })

  it("uses the same neutral fallback in the bookmarks table", () => {
    const queryClient = new QueryClient()
    render(
      <MemoryRouter>
        <QueryClientProvider client={queryClient}>
          <TooltipProvider>
            <TorrentTable
              layout="bookmarks"
              torrents={[
                {
                  id: 42,
                  name: "Bookmarked Release",
                  subtitle: "",
                  category: { id: "movies", name: "电影" },
                  size_bytes: 1024,
                  seeders: 1,
                  leechers: 0,
                  completed: 1,
                  promotion: "none",
                  sticky_until: null,
                  uploaded_at: "2026-08-12T10:00:00Z",
                  swarm_observed_at: "2026-08-12T10:00:00Z",
                  swarm_stale: false,
                },
              ]}
            />
          </TooltipProvider>
        </QueryClientProvider>
      </MemoryRouter>
    )

    const cover = screen.getByRole("img", { name: "Bookmarked Release封面" })
    fireEvent.error(cover)
    expect(
      screen.getByRole("img", { name: "Bookmarked Release暂无封面" })
    ).toHaveClass("from-neutral-100", "to-neutral-300")
  })

  it("does not present unobserved import defaults as real Tracker values", () => {
    const queryClient = new QueryClient()
    render(
      <MemoryRouter>
        <QueryClientProvider client={queryClient}>
          <TooltipProvider>
            <TorrentTable
              torrents={[
                {
                  id: 42,
                  name: "Legacy Import",
                  subtitle: "Awaiting first announce",
                  category: { id: "movies", name: "电影" },
                  size_bytes: 1024,
                  seeders: 0,
                  leechers: 0,
                  completed: 0,
                  promotion: "none",
                  sticky_until: null,
                  uploaded_at: "2026-08-09T10:00:00Z",
                  swarm_observed_at: "1970-01-01T00:00:00Z",
                  swarm_stale: true,
                },
              ]}
            />
          </TooltipProvider>
        </QueryClientProvider>
      </MemoryRouter>
    )

    expect(screen.getByLabelText("尚无 Tracker 统计")).toHaveTextContent(
      "— / — / —"
    )
  })

  it("obscures a migrated 9kg thumbnail without adding a label to the dense row", () => {
    const queryClient = new QueryClient()
    render(
      <MemoryRouter>
        <QueryClientProvider client={queryClient}>
          <TooltipProvider>
            <TorrentTable
              torrents={[
                {
                  id: 42,
                  name: "Protected Release",
                  subtitle: "",
                  category: { id: "9kg", name: "9KG" },
                  size_bytes: 1024,
                  seeders: 1,
                  leechers: 0,
                  completed: 1,
                  promotion: "none",
                  sticky_until: null,
                  uploaded_at: "2026-08-12T10:00:00Z",
                  swarm_observed_at: "2026-08-12T10:00:00Z",
                  swarm_stale: false,
                },
              ]}
            />
          </TooltipProvider>
        </QueryClientProvider>
      </MemoryRouter>
    )

    expect(
      screen.getByRole("img", { name: "Protected Release封面已隐藏" })
    ).toHaveClass("blur-xl")
    expect(screen.queryByText("NSFW · 18+")).not.toBeInTheDocument()
  })

  it.each([
    ["free", "免费"],
    ["double_upload", "2X"],
    ["double_upload_free", "2X免费"],
    ["half_download", "50%"],
    ["double_upload_half_download", "2X50%"],
    ["thirty_percent_download", "30%"],
  ] as const)("renders the %s promotion label", (promotion, label) => {
    const queryClient = new QueryClient()
    render(
      <MemoryRouter>
        <QueryClientProvider client={queryClient}>
          <TooltipProvider>
            <TorrentTable
              torrents={[
                {
                  id: 42,
                  name: "Promotion release",
                  subtitle: "",
                  category: { id: "movies", name: "电影" },
                  size_bytes: 1024,
                  seeders: 1,
                  leechers: 0,
                  completed: 1,
                  promotion,
                  sticky_until: null,
                  uploaded_at: "2026-08-12T10:00:00Z",
                  swarm_observed_at: "2026-08-12T10:00:00Z",
                  swarm_stale: false,
                },
              ]}
            />
          </TooltipProvider>
        </QueryClientProvider>
      </MemoryRouter>
    )

    expect(screen.getByText(label)).toBeVisible()
  })

  it("links canonical numeric rows to detail", () => {
    const queryClient = new QueryClient()
    const torrentId = 42
    render(
      <MemoryRouter>
        <QueryClientProvider client={queryClient}>
          <TooltipProvider>
            <TorrentTable
              torrents={[
                {
                  id: torrentId,
                  name: "Published Release",
                  subtitle: "Real aggregate",
                  category: { id: "movies", name: "电影" },
                  size_bytes: 1024,
                  seeders: 1,
                  leechers: 0,
                  completed: 1,
                  promotion: "none",
                  sticky_until: null,
                  uploaded_at: "2026-08-09T10:00:00Z",
                  swarm_observed_at: "2026-08-09T10:00:00Z",
                  swarm_stale: false,
                },
                {
                  id: 43,
                  name: "Demo Release",
                  subtitle: "Legacy projection",
                  category: { id: "movies", name: "电影" },
                  size_bytes: 2048,
                  seeders: 0,
                  leechers: 0,
                  completed: 0,
                  promotion: "none",
                  sticky_until: null,
                  uploaded_at: "2026-08-09T09:00:00Z",
                  swarm_observed_at: "2026-08-09T09:00:00Z",
                  swarm_stale: false,
                },
              ]}
            />
          </TooltipProvider>
        </QueryClientProvider>
      </MemoryRouter>
    )

    expect(
      screen.getByRole("link", { name: "Published Release" })
    ).toHaveAttribute("href", `/torrents/${torrentId}`)
    expect(screen.getByRole("link", { name: "Demo Release" })).toHaveAttribute(
      "href",
      "/torrents/43"
    )
  })
})

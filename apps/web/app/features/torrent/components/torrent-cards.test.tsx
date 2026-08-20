import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { fireEvent, render, screen } from "@testing-library/react"
import { MemoryRouter } from "react-router"
import { describe, expect, it } from "vitest"

import { TooltipProvider } from "~/components/ui/tooltip"
import { TorrentCards } from "~/features/torrent/components/torrent-cards"

const torrent = {
  id: 42,
  name: "Published Release",
  subtitle: "PeerGo media fixture",
  category: { id: "movies", name: "电影" },
  size_bytes: 1_073_741_824,
  seeders: 12,
  leechers: 3,
  completed: 40,
  promotion: "free" as const,
  sticky_until: null,
  uploaded_at: "2026-08-05T10:00:00Z",
  swarm_observed_at: "2026-08-05T09:00:00Z",
  swarm_stale: false,
}

describe("TorrentCards", () => {
  it("uses a compact two-to-six column 2:3 poster layout without fake media", () => {
    const queryClient = new QueryClient()
    const { container } = render(
      <MemoryRouter>
        <QueryClientProvider client={queryClient}>
          <TooltipProvider>
            <TorrentCards torrents={[torrent]} poster />
          </TooltipProvider>
        </QueryClientProvider>
      </MemoryRouter>
    )

    const cover = screen.getByRole("img", {
      name: "Published Release封面",
    })
    expect(cover).toHaveAttribute(
      "src",
      `http://localhost:3000/api/v1/torrents/${torrent.id}/cover`
    )
    expect(cover.closest("div")).toHaveClass("aspect-[2/3]")
    expect(screen.queryByText("电影")).not.toBeInTheDocument()
    expect(screen.getByText("免费")).toBeInTheDocument()
    expect(screen.getByRole("link")).toHaveAttribute(
      "href",
      `/torrents/${torrent.id}`
    )
    expect(container.firstElementChild).toHaveClass(
      "grid-cols-2",
      "xl:grid-cols-6"
    )

    fireEvent.error(cover)
    expect(
      screen.getByRole("img", { name: "Published Release暂无封面" })
    ).toHaveClass("bg-gradient-to-br", "from-neutral-200", "to-neutral-500")
  })

  it("links canonical numeric IDs to torrent detail", () => {
    const queryClient = new QueryClient()
    render(
      <MemoryRouter>
        <QueryClientProvider client={queryClient}>
          <TooltipProvider>
            <TorrentCards torrents={[{ ...torrent, id: 43 }]} poster />
          </TooltipProvider>
        </QueryClientProvider>
      </MemoryRouter>
    )

    expect(screen.getByText("Published Release").closest("a")).toHaveAttribute(
      "href",
      "/torrents/43"
    )
  })

  it("obscures migrated 9kg covers until the local preference is confirmed", () => {
    const queryClient = new QueryClient()
    render(
      <MemoryRouter>
        <QueryClientProvider client={queryClient}>
          <TooltipProvider>
            <TorrentCards
              torrents={[
                {
                  ...torrent,
                  name: "Protected Release",
                  category: { id: "9kg", name: "9KG" },
                },
              ]}
              poster
            />
          </TooltipProvider>
        </QueryClientProvider>
      </MemoryRouter>
    )

    expect(
      screen.getByRole("img", { name: "Protected Release封面已隐藏" })
    ).toHaveClass("blur-xl")
    expect(screen.getByText("NSFW · 18+")).toBeVisible()
  })

  it("keeps the compact list hierarchy aligned with the reference mobile layout", () => {
    const queryClient = new QueryClient()
    const { container } = render(
      <MemoryRouter>
        <QueryClientProvider client={queryClient}>
          <TooltipProvider>
            <TorrentCards torrents={[torrent]} poster={false} />
          </TooltipProvider>
        </QueryClientProvider>
      </MemoryRouter>
    )

    const title = screen.getByText("Published Release")
    expect(title).toHaveClass("line-clamp-2")
    expect(screen.getByText("PeerGo media fixture")).toHaveClass("line-clamp-1")
    expect(title.closest("article")).toHaveClass("border-t", "p-3")
    expect(screen.getByText("免费")).toHaveClass("text-[10px]")
    expect(container.querySelector("time")).not.toBeInTheDocument()
    expect(screen.getByLabelText("做种数 12，下载数 3")).toHaveClass(
      "items-center",
      "tabular-nums"
    )
    expect(screen.queryByText("40")).not.toBeInTheDocument()

    const cover = screen.getByRole("img", { name: "Published Release封面" })
    fireEvent.error(cover)
    expect(
      screen.getByRole("img", { name: "Published Release暂无封面" })
    ).toHaveClass("from-neutral-100", "to-neutral-300")
  })
})

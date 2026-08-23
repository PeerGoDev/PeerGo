import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter } from "react-router"
import { describe, expect, it } from "vitest"

import { sessionKeys } from "~/features/auth/api/session.mutations"
import {
  torrentKeys,
  type TorrentPeerList,
  type TorrentSwarmOverview,
} from "~/features/torrent/api/torrent.queries"
import { TorrentPeerListCard } from "~/features/torrent/components/torrent-peer-list-card"

const torrentId = 42
const memberId = "0198f20a-6da8-7e51-9c64-111111111111"

describe("TorrentPeerListCard", () => {
  it("matches the PtYes split seeder and leecher tables for signed-in members", async () => {
    const queryClient = peerListClient()
    render(
      <MemoryRouter>
        <QueryClientProvider client={queryClient}>
          <TorrentPeerListCard torrentId={torrentId} />
        </QueryClientProvider>
      </MemoryRouter>
    )

    expect(screen.getByText("User List")).toBeVisible()
    expect(screen.getByText("1 seeding")).toBeVisible()
    expect(screen.getByText("1 个下载者")).toBeVisible()

    await userEvent.click(screen.getByRole("button", { name: /User List/ }))

    expect(screen.getByText("做种者 (1)")).toBeVisible()
    expect(screen.getByText("下载者 (1)")).toBeVisible()
    expect(screen.getByRole("link", { name: "发布者" })).toHaveAttribute(
      "href",
      "/user/uploader"
    )
    expect(screen.getByRole("link", { name: "下载者" })).toHaveAttribute(
      "href",
      "/user/leecher"
    )
    expect(screen.getByText("qBittorrent")).toBeVisible()
    expect(screen.getByText("Transmission")).toBeVisible()
    expect(screen.getByText("70%")).toBeVisible()
    expect(screen.getByText("0.50")).toBeVisible()
    expect(screen.queryByText(/192\.0\.2\./)).not.toBeInTheDocument()
  })
})

function peerListClient() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  })
  queryClient.setQueryData(sessionKeys.current(), {
    user: {
      id: memberId,
      username: "member",
      display_name: "站点成员",
      email_verified: true,
    },
    expires_at: "2026-08-24T00:00:00Z",
    csrf_token: "c".repeat(43),
  })
  queryClient.setQueryData(torrentKeys.swarm(torrentId), {
    torrent_id: torrentId,
    seeders: 1,
    leechers: 1,
    completed: 10,
    observed_at: "2026-08-23T11:59:00Z",
    stale: false,
    confidence: "fresh",
  } satisfies TorrentSwarmOverview)
  queryClient.setQueryData(torrentKeys.peers(torrentId), {
    torrent_id: torrentId,
    total_connections: 3,
    truncated: false,
    generated_at: "2026-08-23T12:00:00Z",
    items: [
      {
        user_numeric_id: 7,
        username: "uploader",
        display_name: "发布者",
        client_families: ["qbittorrent"],
        active_connections: 2,
        seeding_connections: 2,
        leeching_connections: 0,
        progress_basis_points: 10_000,
        uploaded: "2147483648",
        downloaded: "0",
        last_announce: "2026-08-23T11:59:00Z",
        uploader: true,
      },
      {
        user_numeric_id: 8,
        username: "leecher",
        display_name: "下载者",
        client_families: ["transmission"],
        active_connections: 1,
        seeding_connections: 0,
        leeching_connections: 1,
        progress_basis_points: 7000,
        uploaded: "1073741824",
        downloaded: "2147483648",
        last_announce: "2026-08-23T11:58:00Z",
        uploader: false,
      },
    ],
  } satisfies TorrentPeerList)
  return queryClient
}

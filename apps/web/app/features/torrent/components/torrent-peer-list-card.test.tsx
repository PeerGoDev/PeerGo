import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter } from "react-router"
import { describe, expect, it } from "vitest"

import { sessionKeys } from "~/features/auth/api/session.mutations"
import { capabilityKeys } from "~/features/authz/api/capabilities.queries"
import { staffSessionKeys } from "~/features/staff/api/staff-session.mutations"
import {
  torrentKeys,
  type ManagedTorrentPeerList,
  type TorrentSwarmOverview,
} from "~/features/torrent/api/torrent.queries"
import { TorrentPeerListCard } from "~/features/torrent/components/torrent-peer-list-card"

const torrentId = 42
const administratorId = "0198f20a-6da8-7e51-9c64-111111111111"

describe("TorrentPeerListCard", () => {
  it("shows the bounded live user view only with a capable staff session", async () => {
    const queryClient = peerListClient()
    render(
      <MemoryRouter>
        <QueryClientProvider client={queryClient}>
          <TorrentPeerListCard torrentId={torrentId} />
        </QueryClientProvider>
      </MemoryRouter>
    )

    await userEvent.click(screen.getByRole("button", { name: /用户列表/ }))

    expect(screen.getByRole("link", { name: "发布者" })).toHaveAttribute(
      "href",
      "/user/uploader"
    )
    expect(screen.getByText("上传者")).toBeVisible()
    expect(screen.getByText("qBittorrent")).toBeVisible()
    expect(screen.getByText("70.0%")).toBeVisible()
    expect(screen.getByText(/不写入活动明细/)).toBeVisible()
    expect(screen.queryByText(/192\.0\.2\./)).not.toBeInTheDocument()
  })
})

function peerListClient() {
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
    expires_at: "2026-08-23T00:00:00Z",
    csrf_token: "c".repeat(43),
  })
  queryClient.setQueryData(capabilityKeys.current(administratorId), {
    policy_version: "policy-2026-08-22",
    items: [
      {
        action: "staff.session.create.self",
        description: "进入后台",
        scope: { type: "site", id: "peergo" },
        expires_at: "2026-08-23T00:00:00Z",
      },
    ],
  })
  queryClient.setQueryData(staffSessionKeys.current(), {
    user: {
      id: administratorId,
      username: "admin",
      display_name: "管理员",
      email_verified: true,
    },
    expires_at: "2026-08-23T00:00:00Z",
    webauthn_authenticated_at: "2026-08-22T12:00:00Z",
    csrf_token: "s".repeat(43),
  })
  queryClient.setQueryData(staffSessionKeys.capabilities(administratorId), {
    policy_version: "policy-2026-08-22",
    items: [
      {
        action: "torrent.manage.read",
        description: "读取种子管理",
        scope: { type: "site", id: "peergo" },
        expires_at: "2026-08-23T00:00:00Z",
      },
    ],
  })
  queryClient.setQueryData(torrentKeys.swarm(torrentId), {
    torrent_id: torrentId,
    seeders: 1,
    leechers: 1,
    completed: 10,
    observed_at: "2026-08-22T11:59:00Z",
    stale: false,
    confidence: "fresh",
  } satisfies TorrentSwarmOverview)
  queryClient.setQueryData(torrentKeys.managedPeers(torrentId), {
    torrent_id: torrentId,
    total_connections: 1,
    truncated: false,
    generated_at: "2026-08-22T12:00:00Z",
    items: [
      {
        user_id: "0198f20a-6da8-7e51-9c64-222222222222",
        user_numeric_id: 7,
        username: "uploader",
        display_name: "发布者",
        client_families: ["qbittorrent"],
        active_connections: 1,
        seeding_connections: 0,
        leeching_connections: 1,
        progress_basis_points: 7000,
        uploaded: "1073741824",
        downloaded: "2147483648",
        last_announce: "2026-08-22T11:59:00Z",
        uploader: true,
      },
    ],
  } satisfies ManagedTorrentPeerList)
  return queryClient
}

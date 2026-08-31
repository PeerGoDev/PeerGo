import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import { MemoryRouter } from "react-router"
import { describe, expect, it } from "vitest"

import { sessionKeys } from "~/features/auth/api/session.mutations"
import { capabilityKeys } from "~/features/authz/api/capabilities.queries"
import { staffSessionKeys } from "~/features/staff/api/staff-session.mutations"
import { torrentReviewKeys } from "~/features/staff/api/torrent-review.queries"
import { StaffTorrentReviewsPage } from "~/features/staff/pages/staff-torrent-reviews-page"

const userId = "0198f20a-6da8-7e51-9c64-111111111111"

describe("StaffTorrentReviewsPage", () => {
  it("keeps the final-review heading visible while admin state synchronizes", () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false, staleTime: Infinity } },
    })
    queryClient.setQueryData(sessionKeys.current(), {
      user: {
        id: userId,
        username: "reviewer",
        display_name: "种子审核员",
        email_verified: true,
      },
      expires_at: "2026-08-10T00:00:00Z",
      csrf_token: "c".repeat(43),
    })
    queryClient.setQueryData(capabilityKeys.current(userId), {
      policy_version: "2026-08-09",
      items: [{ action: "staff.session.create.self" }],
    })
    queryClient.setQueryData(staffSessionKeys.current(), null)

    render(
      <MemoryRouter initialEntries={["/staff/content/torrent-reviews"]}>
        <QueryClientProvider client={queryClient}>
          <StaffTorrentReviewsPage />
        </QueryClientProvider>
      </MemoryRouter>
    )

    expect(screen.getByRole("heading", { name: "种子审核终审" })).toBeVisible()
    expect(
      screen.getByRole("heading", { name: "正在进入管理后台" })
    ).toBeVisible()
    expect(
      screen.queryByRole("button", { name: "刷新队列" })
    ).not.toBeInTheDocument()
  })

  it("renders the real review center queue for an active reviewer", () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    queryClient.setQueryData(sessionKeys.current(), {
      user: {
        id: userId,
        username: "reviewer",
        display_name: "种子审核员",
        email_verified: true,
      },
      expires_at: "2026-08-10T00:00:00Z",
      csrf_token: "c".repeat(43),
    })
    queryClient.setQueryData(capabilityKeys.current(userId), {
      policy_version: "2026-08-09",
      items: [{ action: "staff.session.create.self" }],
    })
    queryClient.setQueryData(staffSessionKeys.current(), {
      user: {
        id: userId,
        username: "reviewer",
        display_name: "种子审核员",
        email_verified: true,
      },
      expires_at: "2026-08-09T11:00:00Z",
      authentication_method: "account_session",
      authenticated_at: "2026-08-09T10:00:00Z",
      webauthn_authenticated_at: "2026-08-09T10:00:00Z",
      csrf_token: "s".repeat(43),
    })
    queryClient.setQueryData(staffSessionKeys.capabilities(userId), {
      policy_version: "2026-08-09",
      items: [
        {
          action: "torrent.review",
          description: "审核种子",
          scope: { type: "site", id: "peergo" },
          expires_at: "2026-08-10T00:00:00Z",
        },
      ],
    })
    queryClient.setQueryData(torrentReviewKeys.pending(20), {
      total: 1,
      items: [
        {
          id: "0198f20a-6da8-7e51-9c64-222222222222",
          uploader_id: "0198f20a-6da8-7e51-9c64-333333333333",
          uploader_display_name: "上传者",
          category_id: "movies",
          category_name: "电影",
          title: "Release 2026",
          subtitle: "首版",
          content_name: "release.bin",
          info_hash_v1: "a".repeat(40),
          total_size_bytes: 4096,
          file_count: 1,
          version: 1,
          submitted_at: "2026-08-09T10:00:00Z",
          review_requested_at: "2026-08-09T10:00:00Z",
        },
      ],
    })

    render(
      <MemoryRouter initialEntries={["/staff/content/torrent-reviews"]}>
        <QueryClientProvider client={queryClient}>
          <StaffTorrentReviewsPage />
        </QueryClientProvider>
      </MemoryRouter>
    )

    expect(screen.getByRole("main")).toHaveClass(
      "max-w-[1172px]",
      "p-4!",
      "gap-5",
      "sm:p-6!",
      "sm:pt-10!"
    )
    expect(screen.getByRole("button", { name: "刷新队列" })).toBeVisible()
    expect(screen.getByRole("heading", { name: "种子审核终审" })).toBeVisible()
    const pendingTab = screen.getByText("待审核")
    expect(pendingTab).toHaveClass("border-primary", "text-primary")
    expect(pendingTab?.querySelector("[data-slot=badge]")).toHaveAttribute(
      "data-variant",
      "destructiveSolid"
    )
    expect(screen.getAllByText("Release 2026").length).toBeGreaterThan(0)
    expect(screen.getByRole("button", { name: "最终处理" })).toBeVisible()
    expect(screen.queryByRole("table")).not.toBeInTheDocument()
    expect(
      screen.getByText("Release 2026").closest("[data-slot=card]")
    ).toHaveClass("min-h-[130px]")
    expect(
      screen
        .getByText("Release 2026")
        .closest("[data-slot=card]")
        ?.querySelectorAll("svg")
    ).toHaveLength(3)
    expect(screen.queryByText("reviewer@example.com")).not.toBeInTheDocument()
    expect(screen.queryByText("127.0.0.1")).not.toBeInTheDocument()
  })
})

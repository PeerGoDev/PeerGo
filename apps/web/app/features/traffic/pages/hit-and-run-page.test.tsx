import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import { MemoryRouter } from "react-router"
import { describe, expect, it } from "vitest"

import { sessionKeys } from "~/features/auth/api/session.mutations"
import { capabilityKeys } from "~/features/authz/api/capabilities.queries"
import {
  hitAndRunKeys,
  type HitAndRunPageData,
} from "~/features/traffic/api/hnr.queries"
import { HitAndRunPage } from "~/features/traffic/pages/hit-and-run-page"

const userId = "0198f20a-6da8-7e51-9c64-111111111111"

describe("HitAndRunPage", () => {
  it("renders the user-safe H&R overview and exact progress values", () => {
    const queryClient = hnrTestClient()
    seedIdentity(queryClient)
    const page: HitAndRunPageData = {
      as_of: "2026-08-10T08:00:00Z",
      summary: {
        total: "5",
        tracking: "1",
        grace: "1",
        overdue: "1",
        satisfied: "1",
        exempt: "1",
      },
      items: [
        {
          id: "0198f20a-6da8-7e51-9c64-333333333333",
          torrent: {
            id: 42,
            title: "PeerGo H&R Release",
          },
          completed_at: "2026-08-08T08:00:00Z",
          status: "overdue",
          seeded_seconds: "7200",
          required_seed_seconds: "14400",
          raw_uploaded_bytes: "9007199254740993",
          raw_downloaded_bytes: "72057594037927936",
          raw_ratio_basis_points: "12500",
          required_ratio_basis_points: "20000",
          assessment_due_at: "2026-08-09T08:00:00Z",
          grace_ends_at: "2026-08-10T08:00:00Z",
          satisfied_by: null,
          satisfied_at: null,
          updated_at: "2026-08-10T08:00:01Z",
          appeal: null,
          can_appeal: true,
        },
      ],
      next_cursor: null,
    }
    queryClient.setQueryData(
      hitAndRunKeys.page(userId, "open", undefined),
      page
    )

    const { container } = renderHitAndRunPage(queryClient)

    expect(screen.getByRole("heading", { name: "H&R" })).toBeVisible()
    expect(
      screen.getByText("考察概览").closest("[data-slot=card]")
    ).toHaveClass("shadow-sm")
    expect(screen.getByText("考察概览")).toBeVisible()
    expect(
      screen.getByRole("group", { name: "按 H&R 状态筛选" }).parentElement
    ).toHaveClass("overflow-x-auto", "px-4", "sm:px-0")
    expect(screen.getByRole("group", { name: "按 H&R 状态筛选" })).toHaveClass(
      "bg-muted/60",
      "p-1"
    )
    expect(screen.getAllByText("PeerGo H&R Release").length).toBeGreaterThan(0)
    expect(
      screen.getByText("当前有 H&R 待补做记录，新下载已受限")
    ).toBeVisible()
    expect(screen.getAllByText("2 小时 / 4 小时").length).toBeGreaterThan(0)
    expect(screen.getAllByText("1.25 / 2.00").length).toBeGreaterThan(0)
    expect(screen.getAllByText("待补做").length).toBeGreaterThan(0)
    expect(screen.getAllByText("实际分享率").length).toBeGreaterThan(0)
    expect(
      screen.getAllByRole("button", { name: "申诉" }).length
    ).toBeGreaterThan(0)
    expect(screen.queryByText("原始分享率")).not.toBeInTheDocument()
    expect(container.textContent).not.toMatch(
      /policy_revision|tracker|session|evidence|obligation/i
    )
  })

  it("shows a resolved state when history exists but no open record remains", () => {
    const queryClient = hnrTestClient()
    seedIdentity(queryClient)
    queryClient.setQueryData(hitAndRunKeys.page(userId, "open", undefined), {
      as_of: "2026-08-10T08:00:00Z",
      summary: {
        total: "2",
        tracking: "0",
        grace: "0",
        overdue: "0",
        satisfied: "1",
        exempt: "1",
      },
      items: [],
      next_cursor: null,
    } satisfies HitAndRunPageData)

    renderHitAndRunPage(queryClient)

    expect(screen.getByText("当前没有需要处理的 H&R")).toBeVisible()
    expect(screen.getByText(/可在“全部”中查看/)).toBeVisible()
  })
})

function hnrTestClient() {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } })
}

function seedIdentity(queryClient: QueryClient) {
  queryClient.setQueryData(sessionKeys.current(), {
    user: {
      id: userId,
      username: "demo",
      display_name: "PeerGo 演示用户",
      email_verified: true,
    },
    expires_at: "2026-08-10T10:00:00Z",
    csrf_token: "c".repeat(43),
  })
  queryClient.setQueryData(capabilityKeys.current(userId), {
    policy_version: "2026-08-10",
    items: [
      {
        action: "hnr.read.self",
        description: "查看自己的 H&R",
        scope: { type: "site", id: "peergo" },
        expires_at: "2026-08-10T10:00:00Z",
      },
      {
        action: "hnr.appeal.create.self",
        description: "提交自己的 H&R 申诉",
        scope: { type: "site", id: "peergo" },
        expires_at: "2026-08-10T10:00:00Z",
      },
    ],
  })
}

function renderHitAndRunPage(queryClient: QueryClient) {
  return render(
    <MemoryRouter>
      <QueryClientProvider client={queryClient}>
        <HitAndRunPage />
      </QueryClientProvider>
    </MemoryRouter>
  )
}

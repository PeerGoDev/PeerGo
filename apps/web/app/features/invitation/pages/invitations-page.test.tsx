import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter } from "react-router"
import { afterEach, describe, expect, it, vi } from "vitest"

import { sessionKeys } from "~/features/auth/api/session.mutations"
import { capabilityKeys } from "~/features/authz/api/capabilities.queries"
import {
  type InvitationOverview,
  invitationKeys,
} from "~/features/invitation/api/invitations.queries"
import { InvitationsPage } from "~/features/invitation/pages/invitations-page"

const userId = "0198f20a-6da8-7e51-9c64-111111111111"
const invitationId = "0198f20a-6da8-7e51-9c64-222222222222"

describe("InvitationsPage", () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it("explains why member issuance is currently unavailable", async () => {
    const queryClient = invitationQueryClient(disabledOverview)

    renderPage(queryClient)

    expect(await screen.findByRole("heading", { name: "邀请" })).toBeVisible()
    expect(screen.getByText("剩余邀请")).toBeVisible()
    expect(screen.getByText("5")).toBeVisible()
    expect(screen.getByText("成员邀请暂未开放")).toBeVisible()
    expect(screen.getByRole("button", { name: "生成邀请码" })).toBeDisabled()
  })

  it("shows the raw invitation credential only in the successful issue result", async () => {
    const user = userEvent.setup()
    const queryClient = invitationQueryClient(eligibleOverview)
    const rawToken = "A".repeat(43)
    const issuedInvitation = {
      id: invitationId,
      source: "member" as const,
      status: "available" as const,
      created_at: "2026-08-17T12:00:00Z",
      expires_at: "2026-08-24T12:00:00Z",
    }
    const updatedOverview: InvitationOverview = {
      ...eligibleOverview,
      eligibility: {
        ...eligibleOverview.eligibility,
        used_invites: 1,
        remaining_invites: 4,
      },
      items: [issuedInvitation],
      total: 1,
    }
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        jsonResponse({ invitation: issuedInvitation, token: rawToken }, 201)
      )
      .mockResolvedValueOnce(jsonResponse(updatedOverview))
    vi.stubGlobal("fetch", fetchMock)

    renderPage(queryClient)
    await user.click(await screen.findByRole("button", { name: "生成邀请码" }))

    expect(
      await screen.findByRole("heading", { name: "邀请码已生成" })
    ).toBeVisible()
    expect(screen.getByDisplayValue(rawToken)).toBeVisible()
    expect(
      screen.getByText(
        "明文只显示这一次。关闭窗口后只能撤销并重新生成，不能找回。"
      )
    ).toBeVisible()
    const request = fetchMock.mock.calls[0]?.[0] as Request
    expect(request.url).toContain("/api/v1/me/invitations")
    expect(request.method).toBe("POST")
    expect(request.headers.get("X-CSRF-Token")).toBe("c".repeat(43))
  })

  it("shows migrated invitation relationships without crediting legacy rewards again", async () => {
    const user = userEvent.setup()
    const queryClient = invitationQueryClient({
      ...eligibleOverview,
      network: {
        direct_count: 1,
        total_descendants: 2,
        direct_members: [
          {
            numeric_id: 1024,
            username: "legacy-member",
            display_name: "旧站成员",
            source: "legacy_import",
            established_at: "2026-06-01T08:00:00Z",
          },
        ],
        ancestor_members: [],
        harem_reward: {
          amount: "12345",
          source_rows: 88,
          last_rewarded_at: "2026-08-20T08:00:00Z",
        },
        invitation_reward: {
          amount: "5000",
          source_rows: 1,
          last_rewarded_at: "2026-06-01T08:00:00Z",
        },
      },
    })

    renderPage(queryClient)

    await user.click(await screen.findByRole("tab", { name: "后宫" }))
    expect(await screen.findByText("后宫与历史奖励")).toBeVisible()
    expect(screen.getByText("12,345 魔力值")).toBeVisible()
    expect(screen.getByText("旧站成员")).toBeVisible()
    expect(screen.getByText("Rousi 继承")).toBeVisible()
    expect(screen.getByText("旧站奖励已计入期初魔力值")).toBeVisible()
  })
})

function renderPage(queryClient: QueryClient) {
  render(
    <MemoryRouter initialEntries={["/account/invitations"]}>
      <QueryClientProvider client={queryClient}>
        <InvitationsPage />
      </QueryClientProvider>
    </MemoryRouter>
  )
}

function invitationQueryClient(overview: InvitationOverview) {
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
    expires_at: "2026-09-17T12:00:00Z",
    csrf_token: "c".repeat(43),
  })
  queryClient.setQueryData(capabilityKeys.current(userId), {
    policy_version: "2026-08-17",
    items: [
      {
        action: "invitation.read.self",
        description: "查看自己的邀请记录",
        scope: { type: "site", id: "peergo" },
        expires_at: "2026-09-17T12:00:00Z",
      },
    ],
  })
  queryClient.setQueryData(invitationKeys.page(userId, 20, 0), overview)
  return queryClient
}

const disabledOverview: InvitationOverview = {
  eligibility: {
    enabled: false,
    eligible: false,
    blocker: "disabled",
    invite_valid_days: 7,
    max_invites_per_member: 5,
    used_invites: 0,
    remaining_invites: 5,
    minimum_account_age_days: 30,
    current_account_age_days: 365,
    minimum_level: 1,
    current_level: 1,
    email_verified: true,
  },
  items: [],
  network: {
    direct_members: [],
    ancestor_members: [],
    direct_count: 0,
    total_descendants: 0,
    harem_reward: { amount: "0", source_rows: 0 },
    invitation_reward: { amount: "0", source_rows: 0 },
  },
  total: 0,
  limit: 20,
  offset: 0,
  observed_at: "2026-08-17T12:00:00Z",
}

const eligibleOverview: InvitationOverview = {
  ...disabledOverview,
  eligibility: {
    ...disabledOverview.eligibility,
    enabled: true,
    eligible: true,
    blocker: "none",
  },
}

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  })
}

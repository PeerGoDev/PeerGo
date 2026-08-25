import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen, waitFor, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter } from "react-router"
import { afterEach, describe, expect, it, vi } from "vitest"

import { sessionKeys } from "~/features/auth/api/session.mutations"
import { capabilityKeys } from "~/features/authz/api/capabilities.queries"
import {
  type MemberMedalOverview,
  medalKeys,
} from "~/features/economy/api/medals.queries"
import { MedalsPage } from "~/features/economy/pages/medals-page"

const userId = "0198f20a-6da8-7e51-9c64-111111111111"

describe("MedalsPage", () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it("presents migrated holdings and shop entries using the Rousi-compatible structure", async () => {
    const user = userEvent.setup()
    const queryClient = medalQueryClient()

    render(
      <MemoryRouter initialEntries={["/medals"]}>
        <QueryClientProvider client={queryClient}>
          <MedalsPage />
        </QueryClientProvider>
      </MemoryRouter>
    )

    expect(
      await screen.findByRole("heading", { name: "勋章中心" })
    ).toBeVisible()
    expect(screen.getByText("10,000")).toBeVisible()
    expect(screen.getByText("2/3 已佩戴")).toBeVisible()
    expect(screen.getByText("转种组")).toBeVisible()
    expect(
      screen.getByRole("button", { name: "工作组权益自动生效" })
    ).toBeDisabled()
    expect(screen.getByText("上传 +15%")).toBeVisible()

    await user.click(screen.getByText("勋章商店"))

    expect(screen.getByText("周年纪念章")).toBeVisible()
    expect(screen.getByText("2,500 魔力值")).toBeVisible()
    expect(screen.getByRole("button", { name: "购买" })).toBeEnabled()
  })

  it("clears the wearing button loading state after the request settles", async () => {
    const user = userEvent.setup()
    const queryClient = medalQueryClient()

    let resolveWearing!: (value: Response) => void
    const wearingResponse = new Promise<Response>((resolve) => {
      resolveWearing = resolve
    })
    const fetchMock = vi.fn((input: Request) => {
      if (input.url.includes("/wearing")) return wearingResponse
      return Promise.resolve(jsonResponse(overviewAfterWear))
    })
    vi.stubGlobal("fetch", fetchMock)

    render(
      <MemoryRouter initialEntries={["/medals"]}>
        <QueryClientProvider client={queryClient}>
          <MedalsPage />
        </QueryClientProvider>
      </MemoryRouter>
    )

    const wearButton = await screen.findByRole("button", { name: "取下" })
    await user.click(wearButton)

    expect(wearButton).toBeDisabled()
    expect(within(wearButton).getByRole("status")).toBeInTheDocument()

    resolveWearing(jsonResponse(holdingAfterWear))

    await waitFor(() => {
      expect(wearButton).toBeEnabled()
      expect(wearButton).toHaveTextContent("佩戴")
      expect(within(wearButton).queryByRole("status")).toBeNull()
    })
  })
})

function medalQueryClient() {
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
    expires_at: "2026-09-19T01:30:00Z",
    csrf_token: "c".repeat(43),
  })
  queryClient.setQueryData(medalKeys.current(userId), overview)
  queryClient.setQueryData(capabilityKeys.current(userId), {
    policy_version: "2026-08-19",
    items: [
      {
        action: "economy.medal.wear.self",
        description: "佩戴、取下并调整勋章顺序",
        scope: { type: "site", id: "peergo" },
        expires_at: "2026-09-19T12:00:00Z",
      },
      {
        action: "economy.medal.purchase.self",
        description: "购买勋章",
        scope: { type: "site", id: "peergo" },
        expires_at: "2026-09-19T12:00:00Z",
      },
    ],
  })
  return queryClient
}

const overview: MemberMedalOverview = {
  settings: {
    enabled: true,
    maximum_wear_count: 3,
    maximum_upload_bonus_bps: 5000,
    maximum_download_discount_bps: 5000,
    maximum_magic_bonus_bps: 5000,
    maximum_invite_bonus: "10",
    condition_check_day: 1,
    condition_warning_days: 7,
    version: 1,
    updated_at: "2026-08-19T01:00:00Z",
  },
  magic_balance: "10000",
  benefits: {
    upload_bonus_bps: 1500,
    download_discount_bps: 500,
    magic_bonus_bps: 1000,
    invite_bonus: "2",
  },
  owned_count: "2",
  wearing_count: "2",
  shop_count: "1",
  items: [
    {
      id: "1",
      name: "转种组",
      description: "转种工作组成员勋章",
      acquisition_method: "workgroup",
      price: "0",
      duration_days: 0,
      upload_bonus_bps: 1500,
      download_discount_bps: 0,
      magic_bonus_bps: 0,
      invite_bonus: "0",
      is_workgroup: true,
      holding: {
        id: "101",
        state: "wearing",
        priority: 20,
        acquired_at: "2026-08-01T00:00:00Z",
        version: 1,
      },
      purchasable: false,
      purchase_unavailable_reason: "工作组勋章不能购买",
    },
    {
      id: "2",
      name: "老站贡献者",
      acquisition_method: "grant",
      price: "0",
      duration_days: 0,
      upload_bonus_bps: 0,
      download_discount_bps: 500,
      magic_bonus_bps: 1000,
      invite_bonus: "2",
      is_workgroup: false,
      holding: {
        id: "102",
        state: "wearing",
        priority: 10,
        acquired_at: "2026-08-01T00:00:00Z",
        version: 1,
      },
      purchasable: false,
    },
    {
      id: "3",
      name: "周年纪念章",
      description: "限时开放购买",
      acquisition_method: "purchase",
      price: "2500",
      duration_days: 365,
      upload_bonus_bps: 0,
      download_discount_bps: 0,
      magic_bonus_bps: 500,
      invite_bonus: "0",
      is_workgroup: false,
      inventory: "50",
      purchasable: true,
    },
  ],
}

const holdingAfterWear = {
  id: "102",
  state: "owned" as const,
  priority: 0,
  acquired_at: "2026-08-01T00:00:00Z",
  version: 2,
}

const overviewAfterWear: MemberMedalOverview = {
  ...overview,
  wearing_count: "1",
  items: overview.items.map((item) =>
    item.id === "2" ? { ...item, holding: holdingAfterWear } : item
  ),
}

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  })
}

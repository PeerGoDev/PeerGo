import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { fireEvent, render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter } from "react-router"
import { afterEach, expect, it, vi } from "vitest"

import { sessionKeys } from "~/features/auth/api/session.mutations"
import {
  magicStoreKeys,
  type MagicShopOverview,
} from "~/features/economy/api/magic-store.queries"
import { MagicShopPage } from "./magic-shop-page"

const overview: MagicShopOverview = {
  settings: {
    timezone: "Asia/Shanghai",
    shop_enabled: true,
    invite: { enabled: true, price: 10000, monthly_limit: 2 },
    temporary_invite: {
      enabled: true,
      price: 3000,
      monthly_limit: 5,
      valid_days: 7,
    },
    vip: { enabled: true, price: 50000, duration_days: 30 },
    name_color: {
      enabled: true,
      solid_price: 5000,
      gradient_price: 12000,
      duration_days: 30,
    },
    makeup_card: {
      enabled: true,
      price: 800,
      window_days: 7,
      maximum_holding: 10,
    },
    games_enabled: false,
    scratch: { enabled: false, price: 100, daily_limit: 20, prizes: [] },
    lottery: {
      enabled: false,
      ticket_price: 100,
      maximum_tickets_per_round: 50,
      draw_hour: 21,
      burn_bps: 1000,
      tiers: [],
    },
    pvp: {
      enabled: false,
      minimum_stake: 100,
      maximum_stake: 10000,
      fee_bps: 500,
      maximum_open_per_member: 3,
      expiry_hours: 24,
    },
  },
  settings_version: 1,
  magic_balance: "2000",
  invites_purchased_this_month: 0,
  temporary_invites_purchased_this_month: 0,
  remaining_invites: 0,
  temporary_invites: 0,
  vip: { active: false, permanent: false },
  makeup_cards: 8,
  name_style_options: [],
  recent_orders: [],
}

afterEach(() => vi.unstubAllGlobals())

it("checks quantity and submits the displayed unit price for a bulk purchase", async () => {
  const user = userEvent.setup()
  const queryClient = new QueryClient()
  queryClient.setQueryData(sessionKeys.current(), {
    user: { id: "viewer", username: "viewer" },
    csrf_token: "session-csrf",
  })
  queryClient.setQueryData(magicStoreKeys.shop("viewer"), overview)
  const order = {
    id: "order",
    item_key: "makeup_card",
    quantity: 2,
    unit_price: 800,
    total_price: 1600,
    purchased_at: "2026-11-22T10:00:00Z",
  }
  const fetchMock = vi
    .fn()
    .mockResolvedValueOnce(
      Response.json(
        { order, balance_after: "400", replayed: false },
        { status: 201 }
      )
    )
    .mockResolvedValueOnce(
      Response.json({
        ...overview,
        magic_balance: "400",
        makeup_cards: 10,
        recent_orders: [order],
      })
    )
  vi.stubGlobal("fetch", fetchMock)

  render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter>
        <MagicShopPage />
      </MemoryRouter>
    </QueryClientProvider>
  )

  expect(screen.getByRole("link", { name: "前往签到补签" })).toHaveAttribute(
    "href",
    "/account/economy?tab=attendance"
  )
  const quantity = screen.getByRole("spinbutton", { name: "补签卡购买数量" })
  fireEvent.change(quantity, { target: { value: "1.5" } })
  expect(
    screen.getByRole("button", { name: "请输入 1–2 的整数" })
  ).toBeDisabled()
  fireEvent.change(quantity, { target: { value: "2" } })
  await user.click(screen.getByRole("button", { name: "购买 · 1,600 魔力" }))

  expect(await screen.findByText("兑换成功")).toBeVisible()
  expect(screen.getByRole("button", { name: "已达持有上限" })).toBeDisabled()
  const request = fetchMock.mock.calls[0][0] as Request
  expect(request.url).toContain("/api/v1/me/magic-shop/orders")
  expect(request.headers.get("X-CSRF-Token")).toBe("session-csrf")
  await expect(request.clone().json()).resolves.toEqual({
    item_key: "makeup_card",
    quantity: 2,
    expected_unit_price: 800,
  })
})

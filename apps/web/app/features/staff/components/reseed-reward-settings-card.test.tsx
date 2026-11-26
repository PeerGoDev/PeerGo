import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { afterEach, describe, expect, it, vi } from "vitest"

import { ReseedRewardSettingsCard } from "~/features/staff/components/reseed-reward-settings-card"

describe("ReseedRewardSettingsCard", () => {
  afterEach(() => vi.unstubAllGlobals())

  it("saves the threshold and amount against the displayed revision", async () => {
    const settings = {
      revision: 1,
      enabled: false,
      required_hours: 24,
      magic_amount: 1000,
      issued_at: "2026-11-26T00:00:00Z",
    }
    const csrfToken = "c".repeat(43)
    const save = vi.fn()
    vi.stubGlobal("fetch", async (request: Request) => {
      if (request.method === "GET") return Response.json(settings)
      const body = await request.json()
      save(
        new URL(request.url).pathname,
        request.headers.get("X-CSRF-Token"),
        body
      )
      return Response.json({
        ...settings,
        enabled: true,
        required_hours: 12,
        magic_amount: 750,
        revision: 2,
      })
    })

    render(
      <QueryClientProvider client={new QueryClient()}>
        <ReseedRewardSettingsCard csrfToken={csrfToken} canIssue />
      </QueryClientProvider>
    )

    const user = userEvent.setup()
    await user.click(
      await screen.findByRole("switch", { name: "启用复活奖励" })
    )
    const hours = screen.getByRole("spinbutton", {
      name: "有效做种时长（小时）",
    })
    const amount = screen.getByRole("spinbutton", {
      name: "奖励金额（魔力值）",
    })
    await user.clear(hours)
    await user.type(hours, "12")
    await user.clear(amount)
    await user.type(amount, "750")
    await user.click(screen.getByRole("button", { name: "保存复活奖励设置" }))

    expect(await screen.findByRole("status")).toHaveTextContent(
      "复活奖励设置已保存"
    )
    expect(save).toHaveBeenCalledExactlyOnceWith(
      "/api/v1/admin/settings/reseed-reward",
      csrfToken,
      {
        expected_revision: 1,
        enabled: true,
        required_hours: 12,
        magic_amount: 750,
      }
    )
  })
})

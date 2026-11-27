import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { afterEach, describe, expect, it, vi } from "vitest"

import { WorkgroupPayrollPanel } from "~/features/workgroups/components/workgroup-monthly"
import type { components } from "~/generated/api"

const member: components["schemas"]["WorkgroupMembership"] = {
  id: "0198f200-6da8-7e51-9c64-111111111111",
  user_id: "0198f200-6da8-7e51-9c64-222222222222",
  user_numeric_id: 12368,
  username: "member",
  display_name: "成员",
  group_kind: "retention",
  status: "active",
  source: "staff",
  version: 1,
  started_at: "2026-08-01T00:00:00Z",
  updated_at: "2026-08-01T00:00:00Z",
}

describe("WorkgroupPayrollPanel", () => {
  afterEach(() => vi.unstubAllGlobals())

  it("sends the preview fingerprint only after explicit confirmation", async () => {
    const user = userEvent.setup()
    const preview = {
      month: "2026-10",
      username: member.username,
      group_kind: member.group_kind,
      amount: 500000,
      state: "eligible",
      contribution: 12,
      target: 10,
      qualified_bytes: "2199023255552",
      pending_count: 0,
      missing_hours: 5,
      preview_digest: "a".repeat(64),
    }
    const fetchMock = vi
      .fn()
      .mockImplementationOnce(() => Response.json(preview))
      .mockImplementationOnce(() =>
        Response.json({
          ...preview,
          state: "paid",
          paid_at: "2026-11-27T00:00:00Z",
        })
      )
    vi.stubGlobal("fetch", fetchMock)
    render(
      <QueryClientProvider client={new QueryClient()}>
        <WorkgroupPayrollPanel
          membership={member}
          csrfToken={"s".repeat(43)}
          canPay
        />
      </QueryClientProvider>
    )
    await user.type(
      screen.getByRole("spinbutton", { name: "应发魔力" }),
      "500000"
    )
    await user.click(screen.getByRole("button", { name: "预览工资" }))

    const confirmation = await screen.findByRole("button", {
      name: "确认发放 500,000 魔力",
    })
    expect(fetchMock).toHaveBeenCalledTimes(1)
    const previewRequest = fetchMock.mock.calls[0][0] as Request
    expect(previewRequest.url).toMatch(/\/payroll\/preview$/)
    expect(await previewRequest.json()).not.toHaveProperty("preview_digest")

    await user.click(confirmation)
    expect(await screen.findByText(/member · 已发放/)).toBeVisible()
    const issueRequest = fetchMock.mock.calls[1][0] as Request
    expect(issueRequest.url).toMatch(/\/payroll\/issue$/)
    expect(await issueRequest.json()).toMatchObject({
      amount: 500000,
      preview_digest: preview.preview_digest,
    })
  })
})

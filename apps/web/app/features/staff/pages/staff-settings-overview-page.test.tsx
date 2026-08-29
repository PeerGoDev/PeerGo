import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter } from "react-router"
import { describe, expect, it } from "vitest"

import { sessionKeys } from "~/features/auth/api/session.mutations"
import { capabilityKeys } from "~/features/authz/api/capabilities.queries"
import { staffSessionKeys } from "~/features/staff/api/staff-session.mutations"
import { StaffSettingsOverviewPage } from "~/features/staff/pages/staff-settings-overview-page"
import type { components } from "~/generated/api"

type CapabilityAction = components["schemas"]["CapabilityAction"]

describe("StaffSettingsOverviewPage", () => {
  it("groups, filters and permission-bounds the available setting modules", async () => {
    const user = userEvent.setup()
    const queryClient = createQueryClient([
      "site.display.manage.read",
      "tracker.policy.read",
    ])

    render(
      <MemoryRouter initialEntries={["/staff/settings"]}>
        <QueryClientProvider client={queryClient}>
          <StaffSettingsOverviewPage />
        </QueryClientProvider>
      </MemoryRouter>
    )

    expect(screen.getByRole("heading", { name: "设置中心" })).toBeVisible()
    expect(screen.getByText("3 个可用模块")).toBeVisible()
    expect(screen.getByRole("link", { name: "打开基础设置" })).toHaveAttribute(
      "href",
      "/staff/settings/site"
    )
    expect(
      screen.getByRole("link", { name: "打开Tracker 参数" })
    ).toHaveAttribute("href", "/staff/settings/tracker")
    expect(screen.queryByText("邮件设置")).toBeNull()

    await user.type(screen.getByLabelText("搜索设置"), "下载前缀")
    expect(screen.getByText("基础设置")).toBeVisible()
    expect(screen.queryByText("Tracker 参数")).toBeNull()
  })
})

function createQueryClient(actions: CapabilityAction[]) {
  const userId = "0198f20a-6da8-7e51-9c64-777777777777"
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
    expires_at: "2026-08-30T00:00:00Z",
    csrf_token: "c".repeat(43),
  })
  queryClient.setQueryData(capabilityKeys.current(userId), {
    policy_version: "2026-08-29",
    items: [],
  })
  queryClient.setQueryData(staffSessionKeys.current(), {
    user: {
      id: userId,
      username: "admin",
      display_name: "管理员",
      email_verified: true,
    },
    expires_at: "2026-08-30T00:00:00Z",
    webauthn_authenticated_at: "2026-08-29T08:00:00Z",
    csrf_token: "s".repeat(43),
  })
  queryClient.setQueryData(staffSessionKeys.capabilities(userId), {
    policy_version: "2026-08-29",
    items: actions.map((action) => ({
      action,
      description: action,
      scope: { type: "site", id: "peergo" },
      expires_at: "2026-08-30T00:00:00Z",
    })),
  })
  return queryClient
}

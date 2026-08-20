import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter } from "react-router"
import { describe, expect, it } from "vitest"

import { sessionKeys } from "~/features/auth/api/session.mutations"
import { capabilityKeys } from "~/features/authz/api/capabilities.queries"
import { TorrentReportDialog } from "~/features/torrent/components/torrent-report-dialog"

const userId = "0198f20a-6da8-7e51-9c64-111111111111"

describe("TorrentReportDialog", () => {
  it("uses the member capability and explains the bounded moderation flow", async () => {
    const user = userEvent.setup()
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    queryClient.setQueryData(sessionKeys.current(), {
      user: {
        id: userId,
        username: "reporter",
        display_name: "举报测试用户",
        email_verified: true,
      },
      expires_at: "2026-08-19T00:00:00Z",
      csrf_token: "c".repeat(43),
    })
    queryClient.setQueryData(capabilityKeys.current(userId), {
      policy_version: "test",
      items: [{ action: "torrent.report.create.self" }],
    })

    render(
      <MemoryRouter>
        <QueryClientProvider client={queryClient}>
          <TorrentReportDialog torrentId={42} torrentTitle="安全边界测试种子" />
        </QueryClientProvider>
      </MemoryRouter>
    )

    await user.click(screen.getByRole("button", { name: "举报种子" }))

    expect(screen.getByRole("dialog", { name: "举报种子" })).toBeVisible()
    expect(screen.getByText("举报不会立即删除种子")).toBeVisible()
    expect(screen.getByText(/最多先临时下架/)).toBeVisible()
    expect(screen.getByRole("button", { name: "提交举报" })).toBeEnabled()
  })
})

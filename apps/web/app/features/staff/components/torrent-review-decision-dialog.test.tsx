import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { afterEach, describe, expect, it, vi } from "vitest"

import { TorrentReviewDecisionDialog } from "~/features/staff/components/torrent-review-decision-dialog"

describe("TorrentReviewDecisionDialog", () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it("asks the reviewer to verify again when Core requires a session", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(
          JSON.stringify({
            type: "about:blank",
            title: "需要登录",
            status: 401,
            code: "session_required",
            detail: "请先登录站点账号。",
          }),
          {
            status: 401,
            headers: { "Content-Type": "application/problem+json" },
          }
        )
      )
    )
    const user = userEvent.setup()

    render(
      <QueryClientProvider client={new QueryClient()}>
        <TorrentReviewDecisionDialog
          torrent={{
            id: 1234,
            uploader_id: "0198f20a-6da8-7e51-9c64-444444444444",
            uploader_display_name: "发布员",
            category_id: "movies",
            category_name: "电影",
            title: "演示种子 2026 2160p WEB-DL",
            subtitle: "",
            content_name: "Demo.2026.2160p.WEB-DL",
            info_hash_v1: "0123456789abcdef0123456789abcdef01234567",
            total_size_bytes: 4096,
            file_count: 1,
            version: 1,
            submitted_at: "2026-08-16T08:00:00Z",
            review_requested_at: "2026-08-16T08:00:00Z",
          }}
          csrfToken={"s".repeat(43)}
          onOpenChange={() => {}}
          onDecided={() => {}}
        />
      </QueryClientProvider>
    )

    await user.click(screen.getByRole("button", { name: "确认通过" }))

    expect(
      await screen.findByText("后台会话已经变化，请重新验证后再处理。")
    ).toBeVisible()
  })
})

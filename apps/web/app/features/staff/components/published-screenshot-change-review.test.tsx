import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { describe, expect, it } from "vitest"

import { managedPublishedTorrentScreenshotChangesQueryOptions } from "~/features/staff/api/torrent-administration.queries"
import { PublishedScreenshotChangeReview } from "~/features/staff/components/published-screenshot-change-review"

const requestId = "0198f20a-6da8-7e51-9c64-222222222222"

describe("PublishedScreenshotChangeReview", () => {
  it("previews both screenshot sets through the staff image route", async () => {
    const user = userEvent.setup()
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false, staleTime: Infinity } },
    })
    queryClient.setQueryData(
      managedPublishedTorrentScreenshotChangesQueryOptions.queryKey,
      {
        items: [
          {
            change: {
              id: requestId,
              torrent_id: 1234,
              base_torrent_version: 7,
              base_set_version: 3,
              base_count: 1,
              candidate_count: 2,
              reason: "替换为更清晰的 2160p 截图。",
              status: "pending",
              version: 1,
              created_at: "2026-08-16T08:00:00Z",
              decided_at: null,
            },
            uploader_numeric_id: 327,
            uploader_username: "uploader",
            uploader_display_name: "发布员",
            torrent_title: "演示种子 2026 2160p WEB-DL",
          },
        ],
        total: 1,
        limit: 20,
        offset: 0,
      }
    )

    render(
      <QueryClientProvider client={queryClient}>
        <PublishedScreenshotChangeReview
          enabled
          csrfToken={"s".repeat(43)}
          onDecided={() => {}}
        />
      </QueryClientProvider>
    )

    await user.click(await screen.findByRole("button", { name: "对比并处理" }))
    const dialog = await screen.findByRole("dialog")
    const imageBase = `http://localhost:3000/api/v1/admin/torrents/screenshot-changes/${requestId}`
    expect(within(dialog).getByAltText("当前公开图集 1")).toHaveAttribute(
      "src",
      `${imageBase}/base/screenshots/0`
    )
    expect(within(dialog).getByAltText("候选图集 1")).toHaveAttribute(
      "src",
      `${imageBase}/candidate/screenshots/0`
    )
    expect(within(dialog).getByAltText("候选图集 2")).toHaveAttribute(
      "src",
      `${imageBase}/candidate/screenshots/1`
    )
  })
})

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import { MemoryRouter, Route, Routes } from "react-router"
import { describe, expect, it } from "vitest"

import { TooltipProvider } from "~/components/ui/tooltip"
import { sessionKeys } from "~/features/auth/api/session.mutations"
import {
  type MyTorrentReviewDetail,
  type TorrentReviewFilePage,
  torrentReviewVotingKeys,
} from "~/features/review/api/torrent-review-voting.queries"
import { TorrentReviewDetailPage } from "~/features/review/pages/torrent-review-detail-page"

const torrentId = 42

describe("TorrentReviewDetailPage", () => {
  it("presents the full PtYes-style evidence before an independent vote", () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false, staleTime: Infinity } },
    })
    queryClient.setQueryData(sessionKeys.current(), {
      user: {
        id: "0198f20a-6da8-7e51-9c64-111111111111",
        username: "reviewer",
        display_name: "种审成员",
        email_verified: true,
      },
      expires_at: "2026-08-25T00:00:00Z",
      csrf_token: "c".repeat(43),
    })
    queryClient.setQueryData(torrentReviewVotingKeys.detail(torrentId), {
      id: torrentId,
      uploader_id: "0198f20a-6da8-7e51-9c64-222222222222",
      uploader_display_name: "旧版发布者",
      category_id: "movies",
      category_name: "电影",
      title: "Legacy Review Release 2026",
      subtitle: "完整审核资料",
      content_name: "Legacy.Review.Release.2026",
      info_hash_v1: "a".repeat(40),
      total_size_bytes: 1_073_741_824,
      payload_size_bytes: 1_073_741_000,
      file_count: 2,
      padding_file_count: 1,
      piece_length_bytes: 262_144,
      piece_count: 4_096,
      screenshot_count: 2,
      version: 3,
      submitted_at: "2026-08-24T10:00:00Z",
      review_requested_at: "2026-08-24T10:05:00Z",
      votes_cast: 2,
      required_votes: 3,
      maximum_votes: 4,
      anonymous: false,
      facets: [
        {
          facet_id: "resolution",
          facet_name: "分辨率",
          option_key: "1080p",
          option_label: "1080p",
        },
      ],
      external_identifiers: [{ provider: "imdb", external_id: "tt1234567" }],
      description: "**发布说明已经完整展示**",
      description_format: "markdown",
      media_info:
        "General\nFormat : Matroska\nVideo\nFormat : AVC\nWidth : 1 920 pixels\nHeight : 1 080 pixels",
    } satisfies MyTorrentReviewDetail)
    queryClient.setQueryData(torrentReviewVotingKeys.files(torrentId, 50, 0), {
      torrent_id: torrentId,
      total: 2,
      limit: 50,
      offset: 0,
      items: [
        {
          file_index: 0,
          display_path: "Legacy.Review.Release.2026/movie.mkv",
          size_bytes: 1_073_741_000,
          is_padding: false,
        },
        {
          file_index: 1,
          display_path: "Legacy.Review.Release.2026/.pad/824",
          size_bytes: 824,
          is_padding: true,
        },
      ],
    } satisfies TorrentReviewFilePage)

    render(
      <MemoryRouter initialEntries={[`/review/torrent/${torrentId}`]}>
        <QueryClientProvider client={queryClient}>
          <TooltipProvider>
            <Routes>
              <Route
                path="/review/torrent/:torrentId"
                element={<TorrentReviewDetailPage />}
              />
            </Routes>
          </TooltipProvider>
        </QueryClientProvider>
      </MemoryRouter>
    )

    expect(screen.getByText("Legacy Review Release 2026")).toBeVisible()
    expect(screen.getByText("旧版发布者")).toBeVisible()
    expect(screen.getByText("分辨率：1080p")).toBeVisible()
    expect(screen.getByText("MediaInfo/BDInfo")).toBeVisible()
    expect(screen.getByText("发布说明已经完整展示")).toBeVisible()
    expect(
      screen.getByText("Legacy.Review.Release.2026/movie.mkv")
    ).toBeVisible()
    expect(screen.getByRole("img", { name: "截图 1" })).toHaveAttribute(
      "src",
      `http://localhost:3000/api/v1/me/torrent-reviews/${torrentId}/screenshots/0`
    )
    expect(screen.getByText("2/4 票")).toBeVisible()
    expect(screen.getByText("投票前隐藏立场分布")).toBeVisible()
    expect(screen.queryByText(/同意 2 票/)).not.toBeInTheDocument()
    expect(screen.queryByText(/拒绝 0 票/)).not.toBeInTheDocument()
    expect(screen.getByRole("button", { name: "提交同意票" })).toBeDisabled()
    expect(
      screen.getByRole("button", { name: "返回审核队列" })
    ).toHaveAttribute("href", "/review/queue")
  })
})

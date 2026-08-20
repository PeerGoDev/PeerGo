import { QueryClient } from "@tanstack/react-query"
import { afterEach, describe, expect, it, vi } from "vitest"

import {
  pendingTorrentReviewsQueryOptions,
  torrentReviewKeys,
} from "~/features/staff/api/torrent-review.queries"
import { apiClient } from "~/shared/api/client"

describe("torrent review queries", () => {
  afterEach(() => vi.restoreAllMocks())

  it("reads the bounded staff review projection", async () => {
    const page = {
      total: 1,
      items: [
        {
          id: "0198f20a-6da8-7e51-9c64-222222222222",
          uploader_id: "0198f20a-6da8-7e51-9c64-111111111111",
          uploader_display_name: "上传者",
          category_id: "movies",
          category_name: "电影",
          title: "Release 2026",
          subtitle: "首版",
          content_name: "release.bin",
          info_hash_v1: "a".repeat(40),
          total_size_bytes: 4096,
          file_count: 1,
          version: 1,
          submitted_at: "2026-08-09T10:00:00Z",
          review_requested_at: "2026-08-09T10:00:00Z",
        },
      ],
    }
    const get = vi.spyOn(apiClient, "GET").mockResolvedValue({
      data: page,
      error: undefined,
      response: new Response(null, { status: 200 }),
    } as never)

    const client = new QueryClient()
    const result = await client.fetchQuery(
      pendingTorrentReviewsQueryOptions(20)
    )

    expect(result).toEqual(page)
    expect(get).toHaveBeenCalledWith("/api/v1/admin/torrent-reviews", {
      params: { query: { limit: 20 } },
    })
    expect(torrentReviewKeys.pending(20)).toEqual([
      "staff",
      "torrent-reviews",
      "pending",
      { limit: 20 },
    ])
  })
})

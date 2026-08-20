import { afterEach, describe, expect, it, vi } from "vitest"

import { resubmitTorrentSubmission } from "~/features/torrent/api/torrent-resubmission.mutations"
import { ApiProblemError } from "~/shared/api/problem"

const torrentId = 42
const requestId = "0198f20a-6da8-7e51-9c64-222222222222"

describe("torrent resubmission API", () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it("sends only editable metadata with CSRF, idempotency and optimistic version", async () => {
    const result = {
      id: requestId,
      torrent_id: torrentId,
      state: "pending_review" as const,
      version: 3,
      category_id: "tv",
      title: "Corrected release",
      subtitle: "完整副标题",
      review_requested_at: "2026-08-10T12:00:00Z",
    }
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify(result), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      })
    )
    vi.stubGlobal("fetch", fetchMock)

    await expect(
      resubmitTorrentSubmission({
        torrentId,
        csrfToken: "csrf-token",
        idempotencyKey: requestId,
        body: {
          expected_version: 2,
          category_id: "tv",
          title: "Corrected release",
          subtitle: "完整副标题",
          correction_note: "已经补全分类、标题与副标题信息。",
        },
      })
    ).resolves.toEqual(result)

    const request = fetchMock.mock.calls[0][0] as Request
    expect(request.method).toBe("PUT")
    expect(request.url).toContain(
      `/api/v1/me/torrent-submissions/${torrentId}/resubmit`
    )
    expect(request.credentials).toBe("include")
    expect(request.headers.get("X-CSRF-Token")).toBe("csrf-token")
    expect(request.headers.get("Idempotency-Key")).toBe(requestId)
    const body = await request.clone().json()
    expect(body).toEqual({
      expected_version: 2,
      category_id: "tv",
      title: "Corrected release",
      subtitle: "完整副标题",
      correction_note: "已经补全分类、标题与副标题信息。",
    })
    expect(body).not.toHaveProperty("torrent_file")
    expect(body).not.toHaveProperty("info_hash_v1")
  })

  it("preserves structured conflict codes for the dialog", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(
          JSON.stringify({
            type: "about:blank",
            title: "提交记录已经变化",
            status: 409,
            code: "torrent_resubmission_version_conflict",
          }),
          {
            status: 409,
            headers: { "Content-Type": "application/problem+json" },
          }
        )
      )
    )

    await expect(
      resubmitTorrentSubmission({
        torrentId,
        csrfToken: "csrf-token",
        idempotencyKey: requestId,
        body: {
          expected_version: 2,
          category_id: "tv",
          title: "Corrected release",
          subtitle: "",
          correction_note: "已经修改了发布分类并补全必要信息。",
        },
      })
    ).rejects.toMatchObject({
      status: 409,
      code: "torrent_resubmission_version_conflict",
      message: "提交记录已经变化",
    } satisfies Partial<ApiProblemError>)
  })
})

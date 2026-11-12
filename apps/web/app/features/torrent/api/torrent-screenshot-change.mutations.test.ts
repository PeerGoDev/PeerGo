import { afterEach, describe, expect, it, vi } from "vitest"

import { submitPublishedTorrentScreenshotChange } from "~/features/torrent/api/torrent-screenshot-change.mutations"

const torrentId = 42
const requestId = "0198f20a-6da8-7e51-9c64-333333333333"
const reason = "替换模糊截图并调整封面顺序。"

describe("published torrent screenshot change API", () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it("sends each manifest item as an ordered JSON part", async () => {
    const result = {
      id: requestId,
      torrent_id: torrentId,
      base_torrent_version: 7,
      base_set_version: 3,
      base_count: 2,
      candidate_count: 2,
      reason,
      status: "pending" as const,
      version: 1,
      created_at: "2026-08-18T17:30:00Z",
      decided_at: null,
    }
    const fetchMock = stubCreatedResponse(result)
    const upload = new File(["screenshot"], "new.png", { type: "image/png" })

    await expect(
      submitPublishedTorrentScreenshotChange({
        torrentId,
        expectedVersion: 7,
        manifest: [
          { source: "upload", index: 0 },
          { source: "existing", index: 1 },
        ],
        uploads: [upload],
        reason,
        csrfToken: "csrf-token",
        idempotencyKey: requestId,
      })
    ).resolves.toEqual(result)

    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(url).toContain(
      `/api/v1/me/torrent-submissions/${torrentId}/screenshot-change`
    )
    expect(init.method).toBe("POST")
    const headers = new Headers(init.headers)
    expect(headers.get("X-CSRF-Token")).toBe("csrf-token")
    expect(headers.get("Idempotency-Key")).toBe(requestId)
    const body = init.body as FormData
    expect([...body.keys()].filter((key) => key.includes("["))).toEqual([])
    expect(body.get("expected_version")).toBe("7")
    expect(body.get("reason")).toBe(reason)
    const manifest = body.getAll("manifest") as File[]
    expect(manifest.map((part) => part.type)).toEqual([
      "application/json",
      "application/json",
    ])
    expect(
      await Promise.all(
        manifest.map(async (part) => JSON.parse(await part.text()))
      )
    ).toEqual([
      { source: "upload", index: 0 },
      { source: "existing", index: 1 },
    ])
    expect((body.getAll("uploads") as File[]).map((file) => file.name)).toEqual(
      ["new.png"]
    )
  })

  it("sends no manifest part when every screenshot is removed", async () => {
    const fetchMock = stubCreatedResponse({})

    await submitPublishedTorrentScreenshotChange({
      torrentId,
      expectedVersion: 7,
      manifest: [],
      uploads: [],
      reason,
      csrfToken: "csrf-token",
      idempotencyKey: requestId,
    })

    const body = (fetchMock.mock.calls[0] as [string, RequestInit])[1]
      .body as FormData
    expect(body.getAll("manifest")).toEqual([])
    expect(body.getAll("uploads")).toEqual([])
  })
})

function stubCreatedResponse(payload: unknown) {
  const fetchMock = vi.fn().mockResolvedValue(
    new Response(JSON.stringify(payload), {
      status: 201,
      headers: { "Content-Type": "application/json" },
    })
  )
  vi.stubGlobal("fetch", fetchMock)
  return fetchMock
}

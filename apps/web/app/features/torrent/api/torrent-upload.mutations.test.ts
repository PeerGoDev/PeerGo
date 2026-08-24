import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import {
  submitTorrent,
  type TorrentUploadProgress,
} from "~/features/torrent/api/torrent-upload.mutations"
import { ApiProblemError } from "~/shared/api/problem"
import { MockUploadXMLHttpRequest } from "~/test/upload-xhr"

const submission = {
  id: 42,
  info_hash_v1: "a".repeat(40),
  state: "pending_review" as const,
  content_name: "Example Release",
  total_size_bytes: 4096,
  file_count: 2,
  submitted_at: "2026-08-09T12:00:00Z",
}

describe("submitTorrent", () => {
  beforeEach(() => {
    MockUploadXMLHttpRequest.reset()
    vi.stubGlobal("XMLHttpRequest", MockUploadXMLHttpRequest)
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it("sends the exact multipart fields and reports upload/processing progress", async () => {
    const progress: TorrentUploadProgress[] = []
    const torrentFile = new File(["d4:infodee"], "example.torrent", {
      type: "application/x-bittorrent",
    })
    const screenshot = new File(["image"], "cover.png", {
      type: "image/png",
    })
    const resultPromise = submitTorrent({
      category_id: "movies",
      title: "Example Release",
      subtitle: "First edition",
      description: "Release description",
      media_info: "General",
      anonymous: true,
      imdb_id: "tt1234567",
      tmdb_id: "123",
      douban_id: "456",
      facet_selections: [
        { facet_id: "genre", option_keys: ["drama", "action"] },
        { facet_id: "region", option_keys: ["mainland-china"] },
      ],
      screenshots: [screenshot],
      torrent_file: torrentFile,
      csrfToken: "csrf-token",
      idempotencyKey: "0198f20a-6da8-7e51-9c64-444444444444",
      onProgress: (value) => progress.push(value),
    })

    const request = MockUploadXMLHttpRequest.instances[0]
    expect(request.method).toBe("POST")
    expect(request.url).toMatch(/\/api\/v1\/torrents$/)
    expect(request.withCredentials).toBe(true)
    expect(request.headers.get("X-CSRF-Token")).toBe("csrf-token")
    expect(request.headers.get("Idempotency-Key")).toBe(
      "0198f20a-6da8-7e51-9c64-444444444444"
    )
    expect(request.headers.has("Content-Type")).toBe(false)
    expect(request.body).toBeInstanceOf(FormData)
    const body = request.body as FormData
    expect(body.get("category_id")).toBe("movies")
    expect(body.get("title")).toBe("Example Release")
    expect(body.get("subtitle")).toBe("First edition")
    expect(body.get("description")).toBe("Release description")
    expect(body.get("media_info")).toBe("General")
    expect(body.get("anonymous")).toBe("true")
    expect(body.get("imdb_id")).toBe("tt1234567")
    expect(body.get("tmdb_id")).toBe("123")
    expect(body.get("douban_id")).toBe("456")
    const facetParts = body.getAll("facet_selections")
    expect(facetParts).toHaveLength(2)
    await expect(readJsonPart(facetParts[0])).resolves.toEqual({
      facet_id: "genre",
      option_keys: ["drama", "action"],
    })
    await expect(readJsonPart(facetParts[1])).resolves.toEqual({
      facet_id: "region",
      option_keys: ["mainland-china"],
    })
    expect(body.getAll("screenshots")).toEqual([screenshot])
    expect(body.get("torrent_file")).toEqual(torrentFile)

    request.reportProgress(5, 10)
    request.completeUpload()
    request.respond(201, submission)

    await expect(resultPromise).resolves.toEqual(submission)
    expect(progress).toEqual([
      { phase: "uploading", percent: 0 },
      { phase: "uploading", percent: 50 },
      { phase: "processing", percent: 100 },
    ])
  })

  it("preserves structured Core problems", async () => {
    const resultPromise = submitTorrent({
      category_id: "movies",
      title: "Duplicate",
      subtitle: "",
      description: "Description",
      media_info: "MediaInfo",
      anonymous: false,
      torrent_file: new File(["d4:infodee"], "duplicate.torrent"),
      csrfToken: "csrf-token",
      idempotencyKey: "0198f20a-6da8-7e51-9c64-666666666666",
    })
    const request = MockUploadXMLHttpRequest.instances[0]
    request.respond(409, {
      title: "种子已经存在",
      status: 409,
      code: "torrent_already_exists",
    })

    await expect(resultPromise).rejects.toMatchObject({
      status: 409,
      code: "torrent_already_exists",
      message: "种子已经存在",
    } satisfies Partial<ApiProblemError>)
  })

  it("accepts trusted-publisher submissions returned as published", async () => {
    const resultPromise = submitTorrent({
      category_id: "movies",
      title: "Trusted release",
      subtitle: "",
      description: "Description",
      media_info: "MediaInfo",
      anonymous: false,
      torrent_file: new File(["d4:infodee"], "trusted.torrent"),
      csrfToken: "csrf-token",
      idempotencyKey: "0198f20a-6da8-7e51-9c64-777777777777",
    })
    const request = MockUploadXMLHttpRequest.instances[0]
    request.respond(201, { ...submission, state: "published" })

    await expect(resultPromise).resolves.toMatchObject({ state: "published" })
  })
})

async function readJsonPart(value: FormDataEntryValue) {
  if (!(value instanceof Blob)) throw new TypeError("expected a JSON Blob")
  return JSON.parse(await value.text()) as unknown
}

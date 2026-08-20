import { afterEach, describe, expect, it, vi } from "vitest"

import {
  isTorrentId,
  requestTorrentDownload,
  torrentDownloadFilename,
} from "~/features/torrent/api/torrent.download"
import { ApiProblemError } from "~/shared/api/problem"

const torrentId = 42

describe("torrent download API", () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it("requests the binary copy by numeric ID without exposing a passkey", async () => {
    const bytes = new Uint8Array([100, 52, 58, 105, 110, 102, 111, 100, 101])
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(bytes, {
        status: 200,
        headers: {
          "Content-Type": "application/x-bittorrent",
          "Content-Disposition":
            "attachment; filename*=UTF-8''%E7%94%B5%E5%BD%B1.torrent",
        },
      })
    )
    vi.stubGlobal("fetch", fetchMock)

    const download = await requestTorrentDownload(torrentId)

    expect(download.filename).toBe("电影.torrent")
    expect(new Uint8Array(await download.blob.arrayBuffer())).toEqual(bytes)
    expect(fetchMock).toHaveBeenCalledOnce()
    const request = fetchMock.mock.calls[0][0] as Request
    expect(request.url).toContain(`/api/v1/torrents/${torrentId}/download`)
    expect(request.url).not.toMatch(/[0-9a-f]{32}/)
    expect(request.credentials).toBe("include")
  })

  it("keeps structured API errors when the success parser is binary", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(
          JSON.stringify({
            type: "about:blank",
            title: "需要登录",
            status: 401,
            code: "web_session_required",
          }),
          {
            status: 401,
            headers: { "Content-Type": "application/problem+json" },
          }
        )
      )
    )

    await expect(requestTorrentDownload(torrentId)).rejects.toMatchObject({
      status: 401,
      code: "web_session_required",
      message: "需要登录",
    } satisfies Partial<ApiProblemError>)
  })
})

describe("torrent download filename", () => {
  it("supports RFC 5987 and strips path-like values", () => {
    expect(
      torrentDownloadFilename(
        "attachment; filename*=UTF-8''folder%2Frelease%202026"
      )
    ).toBe("release 2026.torrent")
    expect(
      torrentDownloadFilename('attachment; filename="..\\release.torrent"')
    ).toBe("release.torrent")
    expect(torrentDownloadFilename(null)).toBe("peergo.torrent")
  })

  it("accepts only positive safe integer torrent IDs", () => {
    expect(isTorrentId(torrentId)).toBe(true)
    expect(isTorrentId("42")).toBe(false)
    expect(isTorrentId(0)).toBe(false)
  })
})

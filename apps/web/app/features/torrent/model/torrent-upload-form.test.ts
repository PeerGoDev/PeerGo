import { describe, expect, it } from "vitest"

import {
  maximumTorrentFileBytes,
  parseExternalIdentifier,
  torrentUploadFieldErrors,
  torrentUploadFormSchema,
} from "~/features/torrent/model/torrent-upload-form"

describe("torrentUploadFormSchema", () => {
  it("normalizes contract fields while retaining the original File", () => {
    const torrentFile = new File(["d4:infodee"], "release.torrent", {
      type: "application/x-bittorrent",
    })

    const result = torrentUploadFormSchema.parse({
      torrentFile,
      categoryId: " movies ",
      title: "  Example Release  ",
      subtitle: "  First edition  ",
      description: "  Release description  ",
      mediaInfo: "  General\nComplete name: Example.mkv  ",
      anonymous: true,
      imdbId: "https://www.imdb.com/title/tt1234567/",
      tmdbId: "https://www.themoviedb.org/movie/123-example",
      doubanId: "https://movie.douban.com/subject/456/",
    })

    expect(result).toEqual({
      torrentFile,
      categoryId: "movies",
      title: "Example Release",
      subtitle: "First edition",
      description: "Release description",
      mediaInfo: "General\nComplete name: Example.mkv",
      anonymous: true,
      imdbId: "tt1234567",
      tmdbId: "123",
      doubanId: "456",
    })
  })

  it.each([
    {
      name: "missing file",
      file: undefined,
      message: "请选择一个 .torrent 文件",
    },
    {
      name: "empty file",
      file: new File([], "empty.torrent"),
      message: "种子文件不能为空",
    },
    {
      name: "wrong extension",
      file: new File(["torrent"], "release.txt"),
      message: "请选择扩展名为 .torrent 的文件",
    },
    {
      name: "contract ceiling",
      file: new File(
        [new Uint8Array(maximumTorrentFileBytes + 1)],
        "large.torrent"
      ),
      message: "种子文件不能超过 16 MiB",
    },
  ])("rejects $name before upload", ({ file, message }) => {
    const result = torrentUploadFormSchema.safeParse({
      torrentFile: file,
      categoryId: "movies",
      title: "Example",
      subtitle: "",
      description: "Description",
      mediaInfo: "MediaInfo",
      anonymous: false,
      imdbId: "",
      tmdbId: "",
      doubanId: "",
    })

    expect(result.success).toBe(false)
    if (!result.success) {
      expect(torrentUploadFieldErrors(result.error).torrentFile).toBe(message)
    }
  })

  it.each([
    ["imdb", "tt7654321", "tt7654321"],
    [
      "imdb",
      "https://www.imdb.com/title/tt7654321/?ref_=fn_all_ttl_1",
      "tt7654321",
    ],
    ["tmdb", "https://www.themoviedb.org/tv/24680-example", "24680"],
    ["douban", "https://movie.douban.com/subject/13579/?from=showing", "13579"],
  ] as const)(
    "normalizes %s identifiers from ids or URLs",
    (provider, raw, expected) => {
      expect(parseExternalIdentifier(provider, raw)).toBe(expected)
    }
  )
})

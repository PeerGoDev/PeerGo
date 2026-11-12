import { describe, expect, it } from "vitest"

import { nsfwCategoryIds } from "~/features/torrent/model/nsfw"

describe("nsfwCategoryIds", () => {
  it("collects only the categories staff marked as NSFW", () => {
    const ids = nsfwCategoryIds([
      { id: "movies", nsfw: false },
      { id: "9kg", nsfw: true },
      { id: "anime", nsfw: false },
    ])

    expect([...ids]).toEqual(["9kg"])
  })

  it("treats a missing category list as having no NSFW categories", () => {
    expect(nsfwCategoryIds(undefined).size).toBe(0)
  })
})

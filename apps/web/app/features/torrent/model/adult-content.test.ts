import { describe, expect, it } from "vitest"

import { torrentCoverRequiresAdultConfirmation } from "~/features/torrent/model/adult-content"

describe("torrentCoverRequiresAdultConfirmation", () => {
  it("recognizes the migrated 9kg category key without affecting normal categories", () => {
    expect(
      torrentCoverRequiresAdultConfirmation({ id: "9kg", name: "其他" })
    ).toBe(true)
    expect(
      torrentCoverRequiresAdultConfirmation({ id: "legacy", name: "9KG" })
    ).toBe(true)
    expect(
      torrentCoverRequiresAdultConfirmation({ id: "movies", name: "电影" })
    ).toBe(false)
  })
})

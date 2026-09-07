import { describe, expect, it } from "vitest"

import { socialPostKeys } from "~/features/social/api/posts.queries"

describe("socialPostKeys", () => {
  it("isolates viewer-specific interaction and poll state", () => {
    const viewerA = "0198f20a-6da8-7e51-9c64-111111111111"
    const viewerB = "0198f20a-6da8-7e51-9c64-222222222222"

    expect(
      socialPostKeys.page("newest", 20, 0, { viewerId: viewerA })
    ).not.toEqual(socialPostKeys.page("newest", 20, 0, { viewerId: viewerB }))
    expect(socialPostKeys.detail("post-id", viewerA)).not.toEqual(
      socialPostKeys.detail("post-id", viewerB)
    )
    expect(
      socialPostKeys.infinite("newest", 20, "member", viewerA)
    ).not.toEqual(socialPostKeys.infinite("newest", 20, "member", viewerB))
  })
})

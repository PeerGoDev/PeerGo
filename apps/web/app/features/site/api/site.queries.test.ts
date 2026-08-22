import { describe, expect, it } from "vitest"

import { siteInfoQueryOptions } from "./site.queries"

describe("siteInfoQueryOptions", () => {
  it("refreshes online presence without polling aggressively", () => {
    expect(siteInfoQueryOptions.staleTime).toBe(30_000)
    expect(siteInfoQueryOptions.refetchInterval).toBe(60_000)
  })
})

import { act, renderHook } from "@testing-library/react"
import { afterEach, describe, expect, it } from "vitest"

import { useNsfwFilter } from "~/features/torrent/hooks/use-nsfw-filter"

describe("useNsfwFilter", () => {
  afterEach(() => localStorage.clear())

  it("hides NSFW until the member turns it off", () => {
    const { result } = renderHook(() => useNsfwFilter())
    expect(result.current[0]).toBe(true)

    act(() => result.current[1](false))
    expect(result.current[0]).toBe(false)
    expect(localStorage.getItem("peergo-hide-nsfw")).toBe("false")
  })

  it("keeps showing NSFW for members who already confirmed adult covers", () => {
    localStorage.setItem("peergo-adult-covers-visible", "true")

    const { result } = renderHook(() => useNsfwFilter())
    expect(result.current[0]).toBe(false)
  })

  it("synchronizes every mounted control in the same tab", () => {
    const first = renderHook(() => useNsfwFilter())
    const second = renderHook(() => useNsfwFilter())

    act(() => first.result.current[1](false))
    expect(second.result.current[0]).toBe(false)
  })
})

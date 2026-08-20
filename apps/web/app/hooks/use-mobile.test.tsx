import { renderHook, waitFor } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

import { useIsMobile } from "~/hooks/use-mobile"

afterEach(() => {
  vi.unstubAllGlobals()
})

describe("useIsMobile", () => {
  it.each([
    [1023, true],
    [1024, false],
  ])("treats a %ipx viewport as mobile: %s", async (width, expected) => {
    Object.defineProperty(window, "innerWidth", {
      configurable: true,
      value: width,
    })
    vi.stubGlobal(
      "matchMedia",
      vi.fn((query: string) => ({
        matches: query === "(max-width: 1023px)" && width <= 1023,
        media: query,
        onchange: null,
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
        addListener: vi.fn(),
        removeListener: vi.fn(),
        dispatchEvent: vi.fn(),
      }))
    )

    const { result } = renderHook(() => useIsMobile())

    await waitFor(() => expect(result.current).toBe(expected))
  })
})

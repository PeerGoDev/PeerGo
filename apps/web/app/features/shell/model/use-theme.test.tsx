import { act, renderHook } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"

import { useTheme } from "~/features/shell/model/use-theme"

describe("useTheme", () => {
  beforeEach(() => {
    window.localStorage.clear()
    document.documentElement.classList.remove("dark")
    document.documentElement.style.colorScheme = ""
  })

  it("follows the system preference when no PeerGo choice is stored", () => {
    vi.stubGlobal("matchMedia", () => mediaQueryList({ matches: true }))

    const { result } = renderHook(() => useTheme())

    expect(result.current.theme).toBe("dark")
    expect(document.documentElement).toHaveClass("dark")
    expect(document.documentElement.style.colorScheme).toBe("dark")
    expect(window.localStorage.getItem("peergo-theme")).toBe("dark")
  })

  it("prefers the stored choice and toggles the document theme", () => {
    window.localStorage.setItem("peergo-theme", "light")
    vi.stubGlobal("matchMedia", () => mediaQueryList({ matches: true }))

    const { result } = renderHook(() => useTheme())
    expect(result.current.theme).toBe("light")
    expect(document.documentElement).not.toHaveClass("dark")

    act(() => result.current.toggleTheme())

    expect(result.current.theme).toBe("dark")
    expect(document.documentElement).toHaveClass("dark")
    expect(window.localStorage.getItem("peergo-theme")).toBe("dark")
  })
})

function mediaQueryList({ matches }: { matches: boolean }): MediaQueryList {
  return {
    matches,
    media: "(prefers-color-scheme: dark)",
    onchange: null,
    addListener: vi.fn(),
    removeListener: vi.fn(),
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    dispatchEvent: vi.fn(),
  }
}

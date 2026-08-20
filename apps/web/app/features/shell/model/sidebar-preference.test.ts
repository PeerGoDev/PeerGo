import { describe, expect, it, vi } from "vitest"

import {
  legacySidebarCollapsedKey,
  readDesktopSidebarOpen,
  writeDesktopSidebarOpen,
} from "~/features/shell/model/sidebar-preference"

describe("desktop sidebar preference", () => {
  it("keeps the PtYes collapsed preference across the new shell", () => {
    const storage = {
      getItem: vi.fn((key: string) =>
        key === legacySidebarCollapsedKey ? "true" : null
      ),
    }

    expect(readDesktopSidebarOpen(storage, "sidebar_state=true")).toBe(false)
  })

  it("falls back to the shadcn sidebar cookie", () => {
    const storage = { getItem: vi.fn(() => null) }

    expect(
      readDesktopSidebarOpen(storage, "theme=light; sidebar_state=false")
    ).toBe(false)
    expect(readDesktopSidebarOpen(storage, "sidebar_state=true")).toBe(true)
    expect(readDesktopSidebarOpen(storage, "")).toBe(true)
  })

  it("writes the same legacy key PtYes already uses", () => {
    const storage = { setItem: vi.fn() }

    writeDesktopSidebarOpen(false, storage)
    expect(storage.setItem).toHaveBeenCalledWith(
      legacySidebarCollapsedKey,
      "true"
    )

    writeDesktopSidebarOpen(true, storage)
    expect(storage.setItem).toHaveBeenLastCalledWith(
      legacySidebarCollapsedKey,
      "false"
    )
  })
})

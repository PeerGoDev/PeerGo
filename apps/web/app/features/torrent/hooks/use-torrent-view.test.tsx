import { act, renderHook } from "@testing-library/react"
import { afterEach, describe, expect, it } from "vitest"

import { useTorrentView } from "~/features/torrent/hooks/use-torrent-view"
import type { TorrentView } from "~/features/torrent/model/torrent-view"

afterEach(() => {
  globalThis.localStorage.clear()
})

describe("useTorrentView", () => {
  it("uses the configured view until the member stores a preference", () => {
    const { result, rerender } = renderHook<
      ReturnType<typeof useTorrentView>,
      { configuredView: TorrentView }
    >(({ configuredView }) => useTorrentView(configuredView), {
      initialProps: { configuredView: "list" },
    })

    expect(result.current[0]).toBe("list")

    rerender({ configuredView: "poster" })
    expect(result.current[0]).toBe("poster")

    act(() => result.current[1]("list"))
    rerender({ configuredView: "poster" })

    expect(result.current[0]).toBe("list")
    expect(globalThis.localStorage.getItem("peergo-torrent-view")).toBe("list")
  })

  it("synchronizes a valid preference from another tab", () => {
    const { result } = renderHook(() => useTorrentView("list"))

    act(() => {
      globalThis.dispatchEvent(
        new StorageEvent("storage", {
          key: "peergo-torrent-view",
          newValue: "poster",
        })
      )
    })

    expect(result.current[0]).toBe("poster")
  })

  it("inherits the PtYes preference until PeerGo stores its own choice", () => {
    globalThis.localStorage.setItem("torrent_view_mode", "poster")

    const { result } = renderHook(() => useTorrentView("list"))

    expect(result.current[0]).toBe("poster")

    act(() => result.current[1]("list"))
    expect(result.current[0]).toBe("list")
    expect(globalThis.localStorage.getItem("peergo-torrent-view")).toBe("list")
    expect(globalThis.localStorage.getItem("torrent_view_mode")).toBe("poster")
  })
})

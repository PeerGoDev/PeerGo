import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"

import { TorrentListEmpty } from "~/features/torrent/components/torrent-list-state"

describe("TorrentListEmpty", () => {
  it("uses the compact catalog empty state for active filters", () => {
    render(<TorrentListEmpty query="missing" />)

    const message = screen.getByText("没有找到符合条件的种子")
    expect(message).toHaveClass("text-muted-foreground")
    expect(message.closest('[data-slot="card"]')).toHaveClass(
      "py-12",
      "text-center"
    )
  })

  it("distinguishes an empty catalog from an empty search result", () => {
    render(<TorrentListEmpty query="" />)

    expect(screen.getByText("暂无种子")).toBeVisible()
  })
})

import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"

import { AnnouncementBody } from "~/features/announcement/components/announcement-body"

describe("AnnouncementBody", () => {
  it("keeps plain text safe while restoring paragraph rhythm", () => {
    const { container } = render(
      <AnnouncementBody
        body={"第一段。\n仍在第一段。\n\n第二段 <script>alert(1)</script>"}
      />
    )

    expect(container.querySelectorAll("p")).toHaveLength(2)
    expect(container.firstChild).toHaveClass("text-base", "leading-7")
    expect(screen.getByText(/第一段。/)).toHaveTextContent(
      /第一段。\s+仍在第一段。/
    )
    expect(screen.getByText(/第二段/)).toHaveTextContent(
      "第二段 <script>alert(1)</script>"
    )
    expect(container.querySelector("script")).toBeNull()
  })

  it("marks imported BBCode as literal compact text", () => {
    const { container } = render(
      <AnnouncementBody body="[b]旧公告[/b]" legacy />
    )

    expect(screen.getByText("[b]旧公告[/b]")).toBeVisible()
    expect(container.firstChild).toHaveClass("font-mono", "text-[13px]")
  })
})

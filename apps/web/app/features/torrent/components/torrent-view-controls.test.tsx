import * as React from "react"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { describe, expect, it } from "vitest"

import type { TorrentView } from "~/features/torrent/model/torrent-view"
import { TorrentViewControls } from "~/features/torrent/components/torrent-view-controls"

describe("TorrentViewControls", () => {
  it("switches between the list, poster and waterfall views", async () => {
    const user = userEvent.setup()
    render(<ControlledTorrentView />)

    expect(screen.getByRole("group", { name: "种子显示方式" })).toHaveAttribute(
      "data-variant",
      "segment"
    )
    const list = screen.getByRole("button", { name: "列表视图" })
    const waterfall = screen.getByRole("button", { name: "瀑布流视图" })
    expect(list).toHaveAttribute("aria-pressed", "true")
    expect(screen.getByRole("button", { name: "海报视图" })).toHaveAttribute(
      "aria-pressed",
      "false"
    )
    expect(list.querySelector("span")).toHaveClass("max-sm:sr-only")

    await user.click(waterfall)
    expect(waterfall).toHaveAttribute("aria-pressed", "true")
    expect(list).toHaveAttribute("aria-pressed", "false")
  })

  it("hides NSFW by default and asks for an age confirmation before showing it", async () => {
    const user = userEvent.setup()
    render(<ControlledTorrentView />)

    const nsfwSwitch = screen.getByRole("switch", { name: "屏蔽 NSFW" })
    expect(nsfwSwitch).toHaveAttribute("aria-checked", "true")

    await user.click(nsfwSwitch)
    const confirmation = screen.getByRole("alertdialog", {
      name: "显示 NSFW 内容？",
    })
    expect(confirmation).toBeVisible()
    expect(confirmation).toHaveClass("data-[size=default]:sm:max-w-[425px]")
    expect(
      screen.getByRole("heading", { name: "显示 NSFW 内容？" })
    ).toHaveClass("text-md", "font-semibold")
    expect(nsfwSwitch).toHaveAttribute("aria-checked", "true")

    await user.click(screen.getByRole("button", { name: "取消" }))
    expect(nsfwSwitch).toHaveAttribute("aria-checked", "true")

    await user.click(nsfwSwitch)
    await user.click(
      screen.getByRole("button", { name: "我已年满 18 周岁，确认显示" })
    )
    expect(nsfwSwitch).toHaveAttribute("aria-checked", "false")

    await user.click(nsfwSwitch)
    expect(nsfwSwitch).toHaveAttribute("aria-checked", "true")
    expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument()
  })
})

function ControlledTorrentView() {
  const [view, setView] = React.useState<TorrentView>("list")
  const [hideNsfw, setHideNsfw] = React.useState(true)

  return (
    <TorrentViewControls
      value={view}
      onValueChange={setView}
      hideNsfw={hideNsfw}
      onHideNsfwChange={setHideNsfw}
    />
  )
}

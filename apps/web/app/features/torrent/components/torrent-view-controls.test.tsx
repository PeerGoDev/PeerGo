import * as React from "react"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { describe, expect, it } from "vitest"

import { TorrentViewControls } from "~/features/torrent/components/torrent-view-controls"

describe("TorrentViewControls", () => {
  it("requires an age confirmation before revealing adult covers", async () => {
    const user = userEvent.setup()
    render(<ControlledTorrentView />)

    const adultToggle = screen.getByRole("button", { name: "18+" })
    expect(adultToggle).toHaveAttribute("aria-pressed", "false")
    expect(adultToggle).toHaveClass("max-sm:w-8", "max-sm:h-6")
    expect(adultToggle.querySelector("span")).toHaveClass("max-sm:sr-only")
    expect(screen.getByRole("button", { name: "列表视图" })).toHaveClass(
      "max-sm:w-[30px]"
    )
    expect(screen.getByRole("button", { name: "海报视图" })).toHaveClass(
      "max-sm:w-[31px]"
    )

    await user.click(adultToggle)
    const confirmation = screen.getByRole("alertdialog", {
      name: "显示成人内容？",
    })
    expect(confirmation).toBeVisible()
    expect(confirmation).toHaveClass("p-6", "sm:max-w-[425px]!")
    expect(screen.getByRole("heading", { name: "显示成人内容？" })).toHaveClass(
      "text-lg",
      "leading-none"
    )
    expect(screen.getByRole("button", { name: "取消" })).toHaveClass("w-[62px]")
    expect(screen.getByRole("button", { name: "关闭" })).toHaveClass(
      "size-4",
      "p-0"
    )

    const confirm = screen.getByRole("button", {
      name: "我已年满 18 周岁，确认显示",
    })
    expect(confirm).toHaveClass("w-[209px]")
    await user.click(confirm)
    expect(adultToggle).toHaveAttribute("aria-pressed", "true")

    await user.click(adultToggle)
    expect(adultToggle).toHaveAttribute("aria-pressed", "false")
    expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument()
  })
})

function ControlledTorrentView() {
  const [adultCoversVisible, setAdultCoversVisible] = React.useState(false)

  return (
    <TorrentViewControls
      value="list"
      onValueChange={() => undefined}
      adultCoversVisible={adultCoversVisible}
      onAdultCoversVisibleChange={setAdultCoversVisible}
    />
  )
}

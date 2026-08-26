import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { describe, expect, it } from "vitest"

import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogMedia,
  AlertDialogTitle,
  AlertDialogTrigger,
} from "~/components/ui/alert-dialog"

describe("AlertDialog", () => {
  it("uses the opaque unblurred backdrop shared with the reference UI", async () => {
    const user = userEvent.setup()

    render(
      <AlertDialog>
        <AlertDialogTrigger render={<button type="button" />}>
          删除
        </AlertDialogTrigger>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogMedia aria-hidden="true">!</AlertDialogMedia>
            <AlertDialogTitle>确认删除？</AlertDialogTitle>
            <AlertDialogDescription>该操作无法撤销。</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction>确认</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    )

    await user.click(screen.getByRole("button", { name: "删除" }))

    expect(
      document.querySelector('[data-slot="alert-dialog-overlay"]')
    ).toHaveClass("bg-black/80", "supports-backdrop-filter:backdrop-blur-none")
    expect(
      document.querySelector('[data-slot="alert-dialog-overlay"]')
    ).not.toHaveClass("supports-backdrop-filter:backdrop-blur-xs")
    expect(
      document.querySelector('[data-slot="alert-dialog-content"]')
    ).toHaveClass(
      "max-w-[calc(100%-2rem)]",
      "data-[size=default]:sm:max-w-[425px]",
      "rounded-3xl",
      "shadow-soft",
      "p-6"
    )
    expect(screen.getByRole("button", { name: "关闭" })).toBeVisible()
  })
})

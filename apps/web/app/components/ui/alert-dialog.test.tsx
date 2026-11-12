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
  it("uses an unblurred dimming backdrop and the dialog elevation", async () => {
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

    const overlay = document.querySelector('[data-slot="alert-dialog-overlay"]')
    expect(overlay).toHaveClass("bg-black/50", "dark:bg-black/65")
    expect(overlay?.className).not.toMatch(/backdrop-blur/)
    expect(
      document.querySelector('[data-slot="alert-dialog-content"]')
    ).toHaveClass(
      "max-h-[calc(100dvh-1rem)]",
      "max-w-[calc(100%-1rem)]",
      "data-[size=default]:sm:max-w-[425px]",
      "rounded-xl",
      "border",
      "shadow-dialog",
      "p-4",
      "sm:p-6"
    )
    expect(screen.getByRole("button", { name: "关闭" })).toBeVisible()
  })
})

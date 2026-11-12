import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { describe, expect, it, vi } from "vitest"

import {
  NumberedPagination,
  paginationWindow,
} from "~/shared/components/numbered-pagination"

describe("paginationWindow", () => {
  it("lists every page when the range is short", () => {
    expect(paginationWindow(1, 1)).toEqual([1])
    expect(paginationWindow(2, 4)).toEqual([1, 2, 3, 4])
  })

  it("keeps the edges and current neighbours with ellipses between", () => {
    expect(paginationWindow(1, 20)).toEqual([1, 2, 3, "gap", 20])
    expect(paginationWindow(10, 20)).toEqual([1, "gap", 9, 10, 11, "gap", 20])
    expect(paginationWindow(20, 20)).toEqual([1, "gap", 18, 19, 20])
  })

  it("shows a single skipped page instead of an ellipsis", () => {
    expect(paginationWindow(4, 20)).toEqual([1, 2, 3, 4, 5, "gap", 20])
  })

  it("clamps out-of-range pages and handles an empty result", () => {
    expect(paginationWindow(99, 5)).toEqual([1, 2, 3, 4, 5])
    expect(paginationWindow(99, 9)).toEqual([1, "gap", 7, 8, 9])
    expect(paginationWindow(1, 0)).toEqual([])
  })
})

describe("NumberedPagination", () => {
  it("marks the current page and moves with the numbered and edge controls", async () => {
    const user = userEvent.setup()
    const onPageChange = vi.fn()
    render(
      <NumberedPagination
        ariaLabel="测试分页"
        currentPage={3}
        totalPages={10}
        onPageChange={onPageChange}
        summary="共 200 条"
      />
    )

    const navigation = screen.getByRole("navigation", { name: "测试分页" })
    expect(navigation).toHaveTextContent("共 200 条")
    expect(screen.getByRole("button", { name: "第 3 页" })).toHaveAttribute(
      "aria-current",
      "page"
    )
    expect(screen.getByText("第 3 / 10 页")).toBeInTheDocument()

    await user.click(screen.getByRole("button", { name: "第 4 页" }))
    expect(onPageChange).toHaveBeenLastCalledWith(4)
    await user.click(screen.getByRole("button", { name: "上一页" }))
    expect(onPageChange).toHaveBeenLastCalledWith(2)
    await user.click(screen.getByRole("button", { name: "第 3 页" }))
    expect(onPageChange).toHaveBeenCalledTimes(2)
  })
})

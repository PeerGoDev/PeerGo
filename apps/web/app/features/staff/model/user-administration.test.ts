import { describe, expect, it } from "vitest"

import {
  managedUserPageNumbers,
  managedUserSearchParams,
  parseManagedUserFilters,
} from "~/features/staff/model/user-administration"

describe("user administration filters", () => {
  it("normalizes unknown status and unsafe page", () => {
    expect(
      parseManagedUserFilters(
        new URLSearchParams("query=%20demo%20&filter=blocked&page=0")
      )
    ).toEqual({ query: "demo", status: "all", page: 1, pageSize: 20 })
  })

  it("serializes only non-default values", () => {
    expect(
      managedUserSearchParams({
        query: "demo",
        status: "active",
        page: 3,
        pageSize: 20,
      }).toString()
    ).toBe("query=demo&filter=active&page=3")
  })

  it("keeps pagination compact around the current page", () => {
    expect(managedUserPageNumbers(6, 12)).toEqual([4, 5, 6, 7, 8])
    expect(managedUserPageNumbers(1, 3)).toEqual([1, 2, 3])
  })
})

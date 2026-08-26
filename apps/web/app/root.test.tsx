import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"

import { HydrateFallback, isAuthShellPathname } from "~/root"

describe("HydrateFallback", () => {
  it("keeps route-module loading visible instead of showing a blank page", () => {
    render(<HydrateFallback />)

    expect(screen.getByRole("status", { name: "加载中" })).toBeVisible()
    expect(screen.getByText("页面加载中")).toBeVisible()
  })
})

describe("isAuthShellPathname", () => {
  it("routes the public auth pages and their children to the auth shell", () => {
    for (const pathname of [
      "/login",
      "/register",
      "/forgot-password",
      "/reset-password",
      "/verify-email",
      "/register/invited",
    ]) {
      expect(isAuthShellPathname(pathname)).toBe(true)
    }
  })

  it("keeps every other route in the app or staff shells", () => {
    for (const pathname of [
      "/",
      "/restrictions",
      "/registered",
      "/login-history",
      "/account/email",
      "/staff",
      "/torrents",
    ]) {
      expect(isAuthShellPathname(pathname)).toBe(false)
    }
  })
})

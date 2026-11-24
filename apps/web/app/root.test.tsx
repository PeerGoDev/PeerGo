import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import { MemoryRouter, Route, Routes } from "react-router"
import { describe, expect, it } from "vitest"

import { sessionKeys } from "~/features/auth/api/session.mutations"
import { HydrateFallback, RequireMember, isAuthShellPathname } from "~/root"

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
      "/restrictions",
      "/register/invited",
    ]) {
      expect(isAuthShellPathname(pathname)).toBe(true)
    }
  })

  it("keeps every other route in the app or staff shells", () => {
    for (const pathname of [
      "/",
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

it("redirects a visitor who opens a member route without rendering its content", async () => {
  const client = new QueryClient({
    defaultOptions: { queries: { staleTime: Infinity } },
  })
  client.setQueryData(sessionKeys.current(), null)
  render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={["/torrents/9060"]}>
        <Routes>
          <Route
            path="/torrents/:id"
            element={<RequireMember>种子详情</RequireMember>}
          />
          <Route path="/login" element={<h1>登录</h1>} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>
  )
  expect(await screen.findByRole("heading", { name: "登录" })).toBeVisible()
  expect(screen.queryByText("种子详情")).not.toBeInTheDocument()
})

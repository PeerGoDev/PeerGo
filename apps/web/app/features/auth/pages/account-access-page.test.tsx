import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import { MemoryRouter } from "react-router"
import { expect, it } from "vitest"

import { AccountAccessPage } from "~/features/auth/pages/account-access-page"
import { siteKeys } from "~/features/site/api/site.queries"

it("shows configured public contacts before credential proof", () => {
  const queryClient = new QueryClient()
  queryClient.setQueryData(siteKeys.info(), {
    custom_navigation_items: [
      {
        label: "官方 Telegram 群",
        url: "https://t.me/peergo_example",
        enabled: false,
        open_in_new_tab: true,
        show_on_appeal: true,
      },
      {
        label: "申诉邮箱",
        url: "mailto:help@example.com",
        enabled: false,
        open_in_new_tab: true,
        show_on_appeal: true,
      },
      {
        label: "Wiki",
        url: "/wiki",
        enabled: true,
        open_in_new_tab: false,
      },
    ],
  })

  render(
    <MemoryRouter initialEntries={["/restrictions"]}>
      <QueryClientProvider client={queryClient}>
        <AccountAccessPage />
      </QueryClientProvider>
    </MemoryRouter>
  )

  expect(screen.getByRole("button", { name: "查询本人限制" })).toBeVisible()
  const telegram = screen.getByRole("link", { name: "官方 Telegram 群" })
  expect(telegram).toHaveAttribute("href", "https://t.me/peergo_example")
  expect(telegram).toHaveAttribute("rel", "noopener noreferrer")
  const email = screen.getByRole("link", { name: "申诉邮箱" })
  expect(email).toHaveAttribute("href", "mailto:help@example.com")
  expect(email).not.toHaveAttribute("target")
  expect(screen.queryByRole("link", { name: "Wiki" })).not.toBeInTheDocument()
})

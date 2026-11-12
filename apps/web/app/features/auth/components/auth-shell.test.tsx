import type { ReactNode } from "react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { afterEach, describe, expect, it } from "vitest"

import { AuthShell } from "~/features/auth/components/auth-shell"
import { siteKeys } from "~/features/site/api/site.queries"

afterEach(() => {
  window.localStorage.clear()
  document.documentElement.classList.remove("dark")
  document.documentElement.style.colorScheme = ""
})

describe("AuthShell", () => {
  it("renders children without the app sidebar or header chrome", () => {
    window.localStorage.setItem("peergo-theme", "light")
    renderWithSite(
      <AuthShell>
        <main>auth content</main>
      </AuthShell>
    )

    expect(screen.getByText("auth content")).toBeVisible()
    expect(screen.queryByRole("navigation")).not.toBeInTheDocument()
    expect(screen.queryByRole("banner")).not.toBeInTheDocument()
    const brandPanel = screen.getByRole("complementary", {
      name: "PeerGo 简介",
    })
    expect(brandPanel).toHaveClass("dark", "hidden", "lg:flex")
    expect(brandPanel).toHaveTextContent("热爱可抵岁月漫长")
    expect(brandPanel).toHaveTextContent("邀请注册")
  })

  it("toggles the theme from the corner button", async () => {
    window.localStorage.setItem("peergo-theme", "light")
    const user = userEvent.setup()
    renderWithSite(<AuthShell>content</AuthShell>)

    await user.click(screen.getByRole("button", { name: "切换到深色模式" }))
    expect(document.documentElement).toHaveClass("dark")
    expect(window.localStorage.getItem("peergo-theme")).toBe("dark")

    await user.click(screen.getByRole("button", { name: "切换到浅色模式" }))
    expect(document.documentElement).not.toHaveClass("dark")
    expect(window.localStorage.getItem("peergo-theme")).toBe("light")
  })
})

function renderWithSite(children: ReactNode) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  })
  queryClient.setQueryData(siteKeys.info(), {
    name: "PeerGo",
    description: "热爱可抵岁月漫长",
    registration_mode: "invite",
    registration_username_min_characters: 3,
    registration_username_max_characters: 20,
    registration_email_domain_mode: "any",
    human_verification: {
      provider: "disabled",
      site_key: "",
      registration_enabled: false,
      login_enabled: false,
      password_recovery_enabled: false,
    },
    online_users: 8,
    default_torrent_view: "list",
    show_latest_announcement: true,
  })
  return render(
    <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  )
}

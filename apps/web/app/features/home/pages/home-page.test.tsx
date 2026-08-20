import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import { MemoryRouter } from "react-router"
import { describe, expect, it } from "vitest"

import { sessionKeys } from "~/features/auth/api/session.mutations"
import { HomePage } from "~/features/home/pages/home-page"
import { siteKeys } from "~/features/site/api/site.queries"
import { torrentListQueryOptions } from "~/features/torrent/api/torrent.queries"

describe("HomePage", () => {
  it("shows the product welcome entry instead of catalog error states to guests", () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false, staleTime: Infinity } },
    })
    queryClient.setQueryData(sessionKeys.current(), null)
    queryClient.setQueryData(siteKeys.info(), {
      name: "PeerGo",
      description: "热爱可抵岁月漫长",
      registration_mode: "open",
      registration_username_min_characters: 3,
      registration_username_max_characters: 20,
      registration_email_domain_mode: "any",
      human_verification: disabledHumanVerification,
      online_users: 8,
      default_torrent_view: "list",
      show_latest_announcement: true,
    })

    render(
      <MemoryRouter>
        <QueryClientProvider client={queryClient}>
          <HomePage />
        </QueryClientProvider>
      </MemoryRouter>
    )

    expect(screen.getByRole("heading", { name: "PeerGo" })).toBeVisible()
    expect(screen.getByText("热爱可抵岁月漫长")).toBeVisible()
    expect(screen.getByRole("button", { name: "登录" })).toHaveAttribute(
      "href",
      "/login"
    )
    expect(screen.getByRole("button", { name: "注册" })).toHaveAttribute(
      "href",
      "/register"
    )
    expect(screen.queryByText("最新种子暂时无法读取")).not.toBeInTheDocument()
  })

  it("keeps the latest announcement publisher and date on the compact PtYes-style line", () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false, staleTime: Infinity } },
    })
    queryClient.setQueryData(sessionKeys.current(), {
      user: { id: "user-1", username: "demo" },
    })
    queryClient.setQueryData(siteKeys.info(), {
      name: "PeerGo",
      description: "热爱可抵岁月漫长",
      registration_mode: "open",
      registration_username_min_characters: 3,
      registration_username_max_characters: 20,
      registration_email_domain_mode: "any",
      human_verification: disabledHumanVerification,
      online_users: 8,
      default_torrent_view: "list",
      show_latest_announcement: true,
    })
    queryClient.setQueryData(siteKeys.latestAnnouncement(), {
      id: "welcome-to-peergo",
      title: "欢迎来到 PeerGo",
      summary: "这是一段需要保持单行显示的公告摘要",
      published_at: "2026-08-09T12:00:00Z",
    })
    queryClient.setQueryData(torrentListQueryOptions().queryKey, {
      items: [],
      total: 0,
      limit: 20,
      offset: 0,
    })

    render(
      <MemoryRouter>
        <QueryClientProvider client={queryClient}>
          <HomePage />
        </QueryClientProvider>
      </MemoryRouter>
    )

    expect(screen.getByText("PeerGo 站务")).toHaveClass("truncate")
    expect(
      screen.queryByText("这是一段需要保持单行显示的公告摘要")
    ).not.toBeInTheDocument()
    const pin = screen
      .getByRole("heading", { level: 2, name: "欢迎来到 PeerGo" })
      .parentElement?.parentElement?.querySelector("svg")
    expect(pin).toHaveClass("text-primary")
    expect(pin).not.toHaveClass("fill-primary")
    expect(screen.getByText("2026年8月9日")).toBeVisible()
    expect(screen.queryByText(/12:00/)).not.toBeInTheDocument()
  })
})

const disabledHumanVerification = {
  provider: "disabled" as const,
  site_key: "",
  registration_enabled: false,
  login_enabled: false,
  password_recovery_enabled: false,
}

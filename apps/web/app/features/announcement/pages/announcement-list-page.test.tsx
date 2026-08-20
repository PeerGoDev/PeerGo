import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import { MemoryRouter, Route, Routes } from "react-router"
import { describe, expect, it } from "vitest"

import { AnnouncementListPage } from "~/features/announcement/pages/announcement-list-page"
import { siteKeys } from "~/features/site/api/site.queries"

describe("AnnouncementListPage", () => {
  it("renders public summaries in a compact linked list", () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    queryClient.setQueryData(siteKeys.announcementPage(20, 0), {
      items: [
        {
          id: "maintenance-window",
          title: "站点维护通知",
          summary: "本周末将进行短时维护。",
          published_at: "2026-08-10T10:00:00Z",
        },
        {
          id: "welcome-to-peergo",
          title: "欢迎来到 PeerGo",
          summary: "请先阅读站点规则。",
          published_at: "2026-08-09T08:00:00Z",
        },
      ],
      total: 2,
      limit: 20,
      offset: 0,
    })
    queryClient.setQueryData(siteKeys.info(), {
      name: "PeerGo",
      description: "测试站点",
      registration_mode: "open",
      registration_username_min_characters: 3,
      registration_username_max_characters: 20,
      registration_email_domain_mode: "any",
      human_verification: disabledHumanVerification,
      online_users: 8,
      default_torrent_view: "list",
      show_latest_announcement: true,
    })

    renderAnnouncementList(queryClient, "/announcements")

    expect(
      screen.getByRole("heading", { level: 1, name: "公告" })
    ).toBeVisible()
    const pinnedAnnouncement = screen.getByRole("link", {
      name: /站点维护通知/,
    })
    expect(pinnedAnnouncement).toHaveAttribute(
      "href",
      "/announcements/maintenance-window"
    )
    expect(screen.getAllByText(/由 PeerGo 站务 发布于/)).toHaveLength(2)
    expect(screen.queryByText("共 2 篇公告")).not.toBeInTheDocument()
    expect(pinnedAnnouncement.querySelector("svg")).toHaveClass(
      "size-3.5",
      "fill-primary"
    )
  })

  it("reads the page number from the stable URL", () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    queryClient.setQueryData(siteKeys.announcementPage(20, 20), {
      items: [
        {
          id: "archive-21",
          title: "较早公告",
          summary: "第二页的公告摘要。",
          published_at: "2026-07-01T08:00:00Z",
        },
      ],
      total: 21,
      limit: 20,
      offset: 20,
    })

    renderAnnouncementList(queryClient, "/announcements?page=2")

    expect(screen.getByRole("link", { name: /较早公告/ })).toBeVisible()
    expect(screen.getByText("第 2 / 2 页")).toBeVisible()
  })
})

const disabledHumanVerification = {
  provider: "disabled" as const,
  site_key: "",
  registration_enabled: false,
  login_enabled: false,
  password_recovery_enabled: false,
}

function renderAnnouncementList(
  queryClient: QueryClient,
  initialEntry: string
) {
  render(
    <MemoryRouter initialEntries={[initialEntry]}>
      <QueryClientProvider client={queryClient}>
        <Routes>
          <Route path="/announcements" element={<AnnouncementListPage />} />
        </Routes>
      </QueryClientProvider>
    </MemoryRouter>
  )
}

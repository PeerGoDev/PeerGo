import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter, Route, Routes } from "react-router"
import { describe, expect, it } from "vitest"

import { sessionKeys } from "~/features/auth/api/session.mutations"
import { capabilityKeys } from "~/features/authz/api/capabilities.queries"
import { socialPostKeys } from "~/features/social/api/posts.queries"
import { staffSessionKeys } from "~/features/staff/api/staff-session.mutations"
import { userAdministrationKeys } from "~/features/staff/api/user-administration.queries"
import { torrentBookmarkKeys } from "~/features/torrent/api/torrent-bookmarks.queries"
import { torrentKeys } from "~/features/torrent/api/torrent.queries"
import { trafficKeys } from "~/features/traffic/api/traffic.queries"
import { trackerActivityKeys } from "~/features/user/api/tracker-activity.queries"
import { userKeys } from "~/features/user/api/user.queries"
import { UserProfilePage } from "~/features/user/pages/user-profile-page"
import { ApiProblemError } from "~/shared/api/problem"

const userId = "0198f20a-6da8-7e51-9c64-111111111111"

describe("UserProfilePage", () => {
  it("builds the own-profile summary only from current-user projections", () => {
    const queryClient = profileTestClient()

    renderProfile(queryClient, "/user/legacy-user")

    expect(screen.getByRole("heading", { name: "迁移用户" })).toBeVisible()
    expect(screen.getByText("@legacy-user")).toBeVisible()
    expect(screen.getByText("流量统计")).toBeVisible()
    expect(
      screen.getByText("基本信息").closest('[data-slot="card"]')
    ).toHaveClass("md:min-h-[262px]")
    expect(
      screen.getByRole("heading", { name: "迁移用户" }).closest("header")
    ).toHaveClass("min-h-[116px]", "md:min-h-[88px]")
    expect(screen.getByText("2 KB")).toBeVisible()
    expect(screen.getAllByText("1 KB")).toHaveLength(2)
    expect(screen.getByText("2.00")).toBeVisible()
    expect(screen.getAllByText("3 个")).toHaveLength(2)
    expect(screen.getByText("注册时间")).toBeVisible()
    expect(screen.getByText(/2024\/03\/18 \d{2}:\d{2}/)).toBeVisible()
    expect(screen.getByText(/\(.*前, .*周\)/)).toBeVisible()
    expect(screen.getByRole("button", { name: "设置" })).toHaveAttribute(
      "href",
      "/account"
    )
    expect(screen.getByRole("button", { name: "设置" })).toHaveClass(
      "font-normal",
      "gap-1"
    )
    expect(
      screen.getByRole("heading", { name: "基本信息" }).parentElement
    ).toHaveClass("leading-6", "font-bold")
    expect(screen.queryByText(/等级|魔力值|PT币/)).not.toBeInTheDocument()
    expect(screen.getByRole("heading", { name: "最新动态" })).toBeVisible()
    expect(screen.getByText("(1 条)")).toBeVisible()
    expect(screen.getByText("迁移后的首条动态")).toBeVisible()
    expect(
      screen.getByRole("button", { name: "查看 legacy-user 的全部动态" })
    ).toHaveAttribute("href", "/social/user/legacy-user")
  })

  it("uses Rousi-style activity tabs without inventing tracker projections", async () => {
    const user = userEvent.setup()
    const queryClient = profileTestClient()

    renderProfile(queryClient, "/user/legacy-user")

    expect(screen.getByRole("button", { name: "发布1" })).toHaveAttribute(
      "aria-pressed",
      "true"
    )
    expect(
      screen.getByRole("region", { name: "种子活动" }).firstElementChild
    ).not.toHaveClass("md:col-span-2")
    expect(screen.getByRole("link", { name: /已发布的电影/ })).toHaveAttribute(
      "href",
      "/torrents/submission-published"
    )
    expect(
      screen.queryByRole("button", { name: /做种中|下载中|已完成|未完成/ })
    ).not.toBeInTheDocument()

    await user.click(screen.getByRole("button", { name: "审核中1" }))

    expect(screen.getByText("等待审核的电影")).toBeVisible()
    expect(screen.getByText("审核中", { selector: "span" })).toBeVisible()
    expect(screen.queryByText("已发布的电影")).not.toBeInTheDocument()
  })

  it("renders another member from the bounded public projection", () => {
    const queryClient = profileTestClient()

    renderProfile(queryClient, "/user/someone-else")

    expect(screen.getByRole("heading", { name: "另一位成员" })).toBeVisible()
    expect(screen.getByText("@someone-else")).toBeVisible()
    expect(screen.getByText("7 个")).toBeVisible()
    expect(screen.getAllByText("未公开")).toHaveLength(6)
    expect(
      screen.queryByRole("button", { name: "设置" })
    ).not.toBeInTheDocument()
    expect(screen.queryByText("邮箱状态")).not.toBeInTheDocument()
    expect(screen.queryByText("发布1")).not.toBeInTheDocument()
  })

  it("adds private account and live client fields only for an authorized admin view", () => {
    const queryClient = profileTestClient()
    const targetId = "0198f20a-6da8-7e51-9c64-222222222222"
    queryClient.setQueryData(capabilityKeys.current(userId), {
      policy_version: "2026-08-12",
      items: [
        capability("traffic.read.self"),
        capability("torrent.submission.read.self"),
        capability("torrent.bookmark.read.self"),
        capability("staff.session.create.self"),
      ],
    })
    queryClient.setQueryData(staffSessionKeys.current(), {
      user: {
        id: userId,
        username: "legacy-user",
        display_name: "迁移用户",
        email_verified: true,
      },
      expires_at: "2026-09-05T12:00:00Z",
      csrf_token: "s".repeat(43),
    })
    queryClient.setQueryData(staffSessionKeys.capabilities(userId), {
      policy_version: "2026-08-12",
      items: [capability("user.account.read")],
    })
    const filters = {
      query: "someone-else",
      status: "all" as const,
      page: 1,
      pageSize: 20,
    }
    const managed = {
      id: targetId,
      numeric_id: 2180,
      username: "someone-else",
      display_name: "另一位成员",
      email: "member@example.test",
      email_verified: true,
      banned: false,
      download_restricted: false,
      vip_enabled: false,
      vip_active: false,
      status: "active" as const,
      version: 3,
      active_restriction_count: 0,
      uploaded_bytes: "4096",
      downloaded_bytes: "2048",
      magic_balance: "1234",
      level: 8,
      role_names: ["member", "reviewer"],
      last_active_at: "2026-08-13T06:00:00Z",
      created_at: "2025-06-01T08:00:00Z",
      updated_at: "2026-08-13T06:00:00Z",
    }
    queryClient.setQueryData(userAdministrationKeys.list(filters), {
      items: [managed],
      total: 1,
      page: 1,
      page_size: 20,
      summary: {
        total: 1,
        active: 1,
        banned: 0,
        pending_activation: 0,
        vip: 0,
        download_restricted: 0,
        unverified: 0,
      },
    })
    queryClient.setQueryData(userAdministrationKeys.detail(targetId), {
      ...managed,
      active_restrictions: [],
      manual_download_restriction: { active: false, version: 0 },
      manual_download_restriction_history: [],
      vip_state: { enabled: false, active: false, version: 0 },
      vip_history: [],
    })
    queryClient.setQueryData(trackerActivityKeys.managed(targetId), {
      items: [
        {
          torrent_id: 42,
          info_hash_v1: "a".repeat(40),
          client_families: ["qBittorrent"],
          address_families: ["IPv4", "IPv6"],
          active_connections: 2,
          seeding_connections: 1,
          leeching_connections: 1,
          progress_basis_points: 10000,
          uploaded: "4096",
          downloaded: "2048",
          upload_speed: "1024",
          download_speed: "0",
          last_announce: "2026-08-13T06:00:00Z",
          seedbox: true,
        },
      ],
      total_connections: 2,
      truncated: false,
      generated_at: "2026-08-13T06:00:00Z",
    })

    renderProfile(queryClient, "/user/someone-else")

    expect(screen.getByText("管理员视图")).toBeVisible()
    expect(screen.getByText("member@example.test")).toBeVisible()
    expect(screen.getByText("member、reviewer")).toBeVisible()
    expect(screen.getByText("双栈")).toBeVisible()
    expect(screen.getByText("盒子")).toBeVisible()
    expect(screen.getByText("qBittorrent")).toBeVisible()
  })

  it("does not misreport a temporary profile failure as a missing member", async () => {
    const queryClient = profileTestClient()
    queryClient.removeQueries({ queryKey: userKeys.profile("temporary") })
    queryClient.setQueryDefaults(userKeys.profile("temporary"), {
      queryFn: () =>
        Promise.reject(
          new ApiProblemError(503, {
            title: "服务暂不可用",
            status: 503,
            code: "temporarily_unavailable",
          })
        ),
      retry: false,
    })

    renderProfile(queryClient, "/user/temporary")

    expect(
      await screen.findByRole("heading", { name: "成员资料暂时无法读取" })
    ).toBeVisible()
    expect(screen.getByRole("button", { name: "重试" })).toBeVisible()
    expect(screen.queryByText("没有找到该成员")).not.toBeInTheDocument()
  })
})

function renderProfile(queryClient: QueryClient, path: string) {
  render(
    <MemoryRouter initialEntries={[path]}>
      <QueryClientProvider client={queryClient}>
        <Routes>
          <Route path="/user/:username" element={<UserProfilePage />} />
        </Routes>
      </QueryClientProvider>
    </MemoryRouter>
  )
}

function profileTestClient() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  })
  queryClient.setQueryData(sessionKeys.current(), {
    user: {
      id: userId,
      username: "legacy-user",
      display_name: "迁移用户",
      email_verified: true,
    },
    expires_at: "2026-09-05T12:00:00Z",
    csrf_token: "c".repeat(43),
  })
  queryClient.setQueryData(capabilityKeys.current(userId), {
    policy_version: "2026-08-12",
    items: [
      capability("traffic.read.self"),
      capability("torrent.submission.read.self"),
      capability("torrent.bookmark.read.self"),
    ],
  })
  queryClient.setQueryData(userKeys.profile("legacy-user"), {
    numeric_id: 2179,
    username: "legacy-user",
    display_name: "迁移用户",
    joined_at: "2024-03-18T09:30:00Z",
    published_torrent_count: 3,
    published_torrents: [],
  })
  queryClient.setQueryData(userKeys.profile("someone-else"), {
    numeric_id: 2180,
    username: "someone-else",
    display_name: "另一位成员",
    joined_at: "2025-06-01T08:00:00Z",
    published_torrent_count: 7,
    published_torrents: [],
  })
  queryClient.setQueryData(trafficKeys.current(userId), {
    totals: {
      raw_uploaded_bytes: "2048",
      raw_downloaded_bytes: "1024",
      credited_uploaded_bytes: "2048",
      charged_downloaded_bytes: "1024",
      entry_count: "0",
      last_settled_at: null,
      projection_updated_at: null,
    },
    entries: [],
    torrent_activity: [],
  })
  queryClient.setQueryData(trackerActivityKeys.mine(userId), {
    items: [],
    total_connections: 0,
    truncated: false,
    generated_at: "2026-08-13T06:00:00Z",
  })
  queryClient.setQueryData(torrentKeys.mySubmissions(userId, 20), {
    total: 3,
    limit: 20,
    items: [
      submission({
        id: "submission-published",
        title: "已发布的电影",
        state: "published",
        published_at: "2026-08-11T08:00:00Z",
      }),
      submission({
        id: "submission-pending",
        title: "等待审核的电影",
        state: "pending_review",
      }),
      submission({
        id: "submission-rejected",
        title: "需要修改的电影",
        state: "rejected",
        resubmission_allowed: true,
        latest_review: {
          outcome: "rejected",
          reason_code: "uploader_action_required",
          reason: "请补充媒体信息",
          decided_at: "2026-08-12T08:00:00Z",
        },
      }),
    ],
  })
  queryClient.setQueryData(torrentBookmarkKeys.list(userId, 1, 0), {
    total: 3,
    limit: 1,
    offset: 0,
    items: [],
  })
  queryClient.setQueryData(
    socialPostKeys.page("newest", 3, 0, "legacy-user"),
    socialPostPage("legacy-user", "迁移用户", "迁移后的首条动态")
  )
  queryClient.setQueryData(
    socialPostKeys.page("newest", 3, 0, "someone-else"),
    socialPostPage("someone-else", "另一位成员", "另一位成员的公开动态")
  )
  return queryClient
}

function socialPostPage(
  username: string,
  displayName: string,
  content: string
) {
  return {
    items: [
      {
        id: "0198f20a-6da8-7e51-9c64-333333333333",
        author: { id: userId, username, display_name: displayName },
        content,
        version: 1,
        comment_count: 0,
        created_at: "2026-08-13T06:00:00Z",
        updated_at: "2026-08-13T06:00:00Z",
      },
    ],
    total: 1,
    limit: 3,
    offset: 0,
    sort: "newest" as const,
  }
}

function submission(
  overrides: Partial<{
    id: string
    title: string
    state: "pending_review" | "published" | "rejected"
    published_at: string | null
    resubmission_allowed: boolean
    latest_review: {
      outcome: "published" | "rejected"
      reason_code: "uploader_action_required"
      reason: string
      decided_at: string
    } | null
  }>
) {
  return {
    id: "submission-default",
    category: { id: "movies", name: "电影" },
    title: "测试种子",
    subtitle: "测试副标题",
    content_name: "Test.Movie.2026",
    info_hash_v1: "a".repeat(40),
    total_size_bytes: 1024,
    file_count: 1,
    state: "pending_review" as const,
    version: 1,
    submitted_at: "2026-08-10T08:00:00Z",
    published_at: null,
    state_changed_at: "2026-08-12T08:00:00Z",
    latest_review: null,
    resubmission_allowed: false,
    ...overrides,
  }
}

function capability(action: string) {
  return {
    action,
    description: action,
    scope: { type: "site", id: "peergo" },
    expires_at: "2026-09-05T12:00:00Z",
  }
}

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter } from "react-router"
import { describe, expect, it } from "vitest"

import { sessionKeys } from "~/features/auth/api/session.mutations"
import { capabilityKeys } from "~/features/authz/api/capabilities.queries"
import {
  torrentKeys,
  type MyTorrentSubmissionPage,
} from "~/features/torrent/api/torrent.queries"
import { MyTorrentSubmissionsPage } from "~/features/torrent/pages/my-torrent-submissions-page"
import { workgroupKeys } from "~/features/workgroups/api/workgroups.queries"

const userId = "0198f20a-6da8-7e51-9c64-111111111111"
const publishedId = 42

describe("MyTorrentSubmissionsPage", () => {
  it("renders public-safe review feedback and opens only the bounded correction form", async () => {
    const user = userEvent.setup()
    const queryClient = submissionTestClient()
    queryClient.setQueryData(sessionKeys.current(), {
      user: {
        id: userId,
        username: "demo",
        display_name: "PeerGo 演示用户",
        email_verified: true,
      },
      expires_at: "2026-08-10T00:00:00Z",
      csrf_token: "c".repeat(43),
    })
    queryClient.setQueryData(capabilityKeys.current(userId), {
      policy_version: "2026-08-09",
      items: [
        {
          action: "torrent.submission.read.self",
          description: "查看自己的种子提交状态与审核反馈",
          scope: { type: "site", id: "peergo" },
          expires_at: "2026-08-10T00:00:00Z",
        },
        {
          action: "torrent.submission.resubmit.self",
          description: "整改自己的被驳回种子发布资料",
          scope: { type: "site", id: "peergo" },
          expires_at: "2026-08-10T00:00:00Z",
        },
        {
          action: "torrent.metadata.update.self",
          description: "修改自己已发布种子的基础发布资料",
          scope: { type: "site", id: "peergo" },
          expires_at: "2026-08-10T00:00:00Z",
        },
        {
          action: "torrent.content.change.submit.self",
          description: "修改自己已发布种子的公开内容",
          scope: { type: "site", id: "peergo" },
          expires_at: "2026-08-10T00:00:00Z",
        },
        {
          action: "torrent.withdraw.request.self",
          description: "申请撤回自己已发布的种子",
          scope: { type: "site", id: "peergo" },
          expires_at: "2026-08-10T00:00:00Z",
        },
      ],
    })
    const page: MyTorrentSubmissionPage = {
      total: 2,
      limit: 20,
      items: [
        {
          id: publishedId,
          category: { id: "movies", name: "电影" },
          title: "Published Release",
          subtitle: "",
          content_name: "Published.Release",
          info_hash_v1: "a".repeat(40),
          total_size_bytes: 4096,
          file_count: 1,
          state: "published",
          version: 2,
          submitted_at: "2026-08-09T10:00:00Z",
          published_at: "2026-08-09T11:00:00Z",
          state_changed_at: "2026-08-09T11:00:00Z",
          latest_review: {
            outcome: "published",
            reason_code: "meets_requirements",
            reason: "已核对文件清单和发布规则，同意正式发布。",
            decided_at: "2026-08-09T11:00:00Z",
          },
          latest_content_change: null,
          latest_screenshot_change: null,
          latest_withdrawal: null,
          resubmission_allowed: false,
        },
        {
          id: 43,
          category: { id: "movies", name: "电影" },
          title: "Rejected Release",
          subtitle: "",
          content_name: "Rejected.Release",
          info_hash_v1: "b".repeat(40),
          total_size_bytes: 8192,
          file_count: 2,
          state: "rejected",
          version: 2,
          submitted_at: "2026-08-09T09:00:00Z",
          published_at: null,
          state_changed_at: "2026-08-09T09:30:00Z",
          latest_review: {
            outcome: "rejected",
            reason_code: "metadata_incomplete",
            reason: "缺少首版要求的必要元数据，请补充后重新提交。",
            decided_at: "2026-08-09T09:30:00Z",
          },
          latest_content_change: null,
          latest_screenshot_change: null,
          latest_withdrawal: null,
          resubmission_allowed: true,
        },
      ],
    }
    queryClient.setQueryData(torrentKeys.mySubmissions(userId, 20), page)
    queryClient.setQueryData(torrentKeys.categories(), [
      { id: "movies", name: "电影" },
      { id: "tv", name: "剧集" },
    ])

    renderSubmissionsPage(queryClient, "/account/submissions")

    expect(
      screen.getByRole("heading", { name: "我的上传审核状态" })
    ).toBeVisible()
    expect(screen.getAllByText("已发布").length).toBeGreaterThan(0)
    expect(screen.getAllByText("已驳回").length).toBeGreaterThan(0)
    expect(screen.getAllByText("符合发布要求").length).toBeGreaterThan(0)
    expect(screen.getAllByText("元数据不完整").length).toBeGreaterThan(0)
    expect(
      screen.getAllByRole("link", { name: "Published Release" })[0]
    ).toHaveAttribute("href", `/torrents/${publishedId}`)
    expect(
      screen.queryByRole("link", { name: "Rejected Release" })
    ).not.toBeInTheDocument()
    expect(screen.queryByText(/reviewer/i)).not.toBeInTheDocument()
    expect(screen.queryByText(/版本 2/)).not.toBeInTheDocument()
    expect(
      screen.getAllByRole("button", { name: "整改并重新提交" })
    ).toHaveLength(1)
    expect(screen.getAllByRole("button", { name: "修改资料" })).toHaveLength(1)
    expect(screen.getAllByRole("button", { name: "修改内容" })).toHaveLength(1)
    expect(screen.getAllByRole("button", { name: "申请撤回" })).toHaveLength(1)
    expect(screen.getByRole("navigation", { name: "审核中心" })).toBeVisible()
    expect(screen.getByRole("link", { name: "我的上传" })).toHaveAttribute(
      "aria-current",
      "page"
    )
    expect(
      screen.queryByRole("link", { name: "审核队列" })
    ).not.toBeInTheDocument()
    expect(
      screen.queryByRole("link", { name: "申请种审团" })
    ).not.toBeInTheDocument()
    expect(screen.queryByText(/暂不可用/)).not.toBeInTheDocument()

    await user.click(screen.getAllByRole("button", { name: "修改资料" })[0])
    expect(
      screen.getByRole("heading", { name: "修改已发布资料" })
    ).toBeVisible()
    expect(screen.getByLabelText("标题")).toHaveValue("Published Release")
    expect(screen.getByText(/信息哈希/)).toBeVisible()
    await user.click(screen.getByRole("button", { name: "取消" }))

    await user.click(screen.getByRole("button", { name: "申请撤回" }))
    expect(screen.getByRole("heading", { name: "申请撤回种子" })).toBeVisible()
    expect(screen.getByText("提交后会立即停止公开")).toBeVisible()
    await user.click(screen.getByRole("button", { name: "取消" }))

    await user.click(
      screen.getAllByRole("button", { name: "整改并重新提交" })[0]
    )
    expect(screen.getByRole("heading", { name: "整改发布信息" })).toBeVisible()
    expect(screen.getByText("原种子内容保持不变")).toBeVisible()
    expect(screen.getByLabelText("标题")).toHaveValue("Rejected Release")
    expect(screen.queryByLabelText("种子文件")).not.toBeInTheDocument()
  })

  it("shows the review center navigation for an active reviewer", () => {
    const queryClient = submissionTestClient()
    queryClient.setQueryData(sessionKeys.current(), {
      user: {
        id: userId,
        username: "demo",
        display_name: "PeerGo 演示用户",
        email_verified: true,
      },
      expires_at: "2026-08-10T00:00:00Z",
      csrf_token: "c".repeat(43),
    })
    queryClient.setQueryData(capabilityKeys.current(userId), {
      policy_version: "2026-08-09",
      items: [
        {
          action: "torrent.submission.read.self",
          description: "查看自己的种子提交状态与审核反馈",
          scope: { type: "site", id: "peergo" },
          expires_at: "2026-08-10T00:00:00Z",
        },
      ],
    })
    queryClient.setQueryData(workgroupKeys.mine(userId), {
      items: [
        {
          definition: { kind: "review" },
          membership: { status: "active" },
        },
      ],
    })
    queryClient.setQueryData(torrentKeys.mySubmissions(userId, 20), {
      total: 0,
      limit: 20,
      items: [],
    })

    renderSubmissionsPage(queryClient)

    expect(screen.getByRole("navigation", { name: "审核中心" })).toBeVisible()
    expect(screen.getByRole("main")).toHaveClass("p-10", "lg:p-12")
    expect(screen.getByRole("main")).not.toHaveClass("max-w-3xl")
    expect(screen.getByRole("link", { name: "审核队列" })).toHaveAttribute(
      "href",
      "/review/queue"
    )
    expect(screen.getByRole("link", { name: "我的上传" })).toHaveAttribute(
      "aria-current",
      "page"
    )
    expect(screen.getByText("暂无待审核的种子")).toBeVisible()
  })

  it("does not treat staff elevation as review workgroup membership", () => {
    const queryClient = submissionTestClient()
    queryClient.setQueryData(sessionKeys.current(), {
      user: {
        id: userId,
        username: "demo",
        display_name: "PeerGo 演示用户",
        email_verified: true,
      },
      expires_at: "2026-08-10T00:00:00Z",
      csrf_token: "c".repeat(43),
    })
    queryClient.setQueryData(capabilityKeys.current(userId), {
      policy_version: "2026-08-09",
      items: [
        {
          action: "torrent.submission.read.self",
          description: "查看自己的种子提交状态与审核反馈",
          scope: { type: "site", id: "peergo" },
          expires_at: "2026-08-10T00:00:00Z",
        },
        {
          action: "staff.session.create.self",
          description: "建立员工安全会话",
          scope: { type: "site", id: "peergo" },
          expires_at: "2026-08-10T00:00:00Z",
        },
      ],
    })
    queryClient.setQueryData(torrentKeys.mySubmissions(userId, 20), {
      total: 0,
      limit: 20,
      items: [],
    })

    renderSubmissionsPage(queryClient)

    expect(
      screen.queryByRole("link", { name: "审核队列" })
    ).not.toBeInTheDocument()
    expect(screen.getByRole("link", { name: "我的上传" })).toHaveAttribute(
      "aria-current",
      "page"
    )
  })
})

function submissionTestClient() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  queryClient.setQueryData(workgroupKeys.mine(userId), { items: [] })
  return queryClient
}

function renderSubmissionsPage(
  queryClient: QueryClient,
  initialEntry = "/account/submissions"
) {
  render(
    <MemoryRouter initialEntries={[initialEntry]}>
      <QueryClientProvider client={queryClient}>
        <MyTorrentSubmissionsPage />
      </QueryClientProvider>
    </MemoryRouter>
  )
}

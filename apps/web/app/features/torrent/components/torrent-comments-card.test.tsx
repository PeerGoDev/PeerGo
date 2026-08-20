import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter } from "react-router"
import { describe, expect, it } from "vitest"

import { sessionKeys } from "~/features/auth/api/session.mutations"
import { capabilityKeys } from "~/features/authz/api/capabilities.queries"
import {
  commentKeys,
  type CommentPage,
  torrentCommentTarget,
} from "~/features/social/api/comments.queries"
import { TorrentCommentsCard } from "~/features/torrent/components/torrent-comments-card"

const torrentId = 42
const userId = "0198f20a-6da8-7e51-9c64-111111111111"
const rootCommentId = "0198f20a-6da8-7e51-9c64-333333333333"
const target = torrentCommentTarget(torrentId)

describe("TorrentCommentsCard", () => {
  it("renders compact threaded comments and exposes owned actions", async () => {
    const user = userEvent.setup()
    const queryClient = commentsTestClient()
    queryClient.setQueryData(sessionKeys.current(), {
      user: {
        id: userId,
        username: "north-shore",
        display_name: "北岸",
        email_verified: true,
      },
      expires_at: "2026-08-10T12:00:00Z",
      csrf_token: "c".repeat(43),
    })
    queryClient.setQueryData(capabilityKeys.current(userId), {
      policy_version: "test",
      items: [
        capability("torrent.comment.create.self"),
        capability("comment.update.self"),
        capability("comment.delete.self"),
        capability("comment.report.create.self"),
      ],
    })
    queryClient.setQueryData(commentKeys.page(target, 20, 0), commentPage())

    renderComments(queryClient)

    expect(screen.getByPlaceholderText("写下你的评论...")).toHaveClass(
      "min-h-[78px]"
    )
    expect(screen.getByRole("button", { name: "发表评论" })).toHaveClass("w-20")
    expect(screen.getByText("0/2000")).toBeVisible()
    expect(screen.getByText("这份资源已经校验。")).toBeVisible()
    expect(screen.getByText("该评论已由作者删除。")).toBeVisible()
    expect(screen.getByText("回复 @北岸")).toBeVisible()
    expect(screen.queryByText("我")).not.toBeInTheDocument()
    expect(screen.getByText("回复 @北岸").closest("article")).toHaveClass(
      "py-0"
    )
    expect(
      screen
        .getByText("回复 @北岸")
        .closest("article")
        ?.querySelector("[data-slot='avatar']")
    ).toHaveAttribute("data-size", "default")
    expect(
      screen.getByText("这份资源已经校验。").closest("article")
    ).toHaveClass("py-0")
    expect(
      screen
        .getByText("这份资源已经校验。")
        .closest("article")
        ?.querySelector("[data-slot='avatar']")
    ).toHaveClass("size-10")
    expect(screen.getAllByRole("button", { name: "回复" })[0]).toHaveClass(
      "h-auto",
      "px-0",
      "text-xs"
    )
    expect(
      screen.getAllByRole("button", { name: "回复" })[0].querySelector("svg")
    ).not.toBeInTheDocument()
    expect(
      screen
        .getByText("这份资源已经校验。")
        .closest("article")
        ?.querySelector("header > span:last-child")
    ).not.toHaveClass("sm:ml-auto")
    expect(screen.getAllByText(/2026\/08\/09 \d{2}:\d{2}/)).toHaveLength(3)
    expect(screen.queryByText(/天前|小时前/)).not.toBeInTheDocument()

    await user.click(screen.getByRole("button", { name: "举报" }))
    expect(screen.getByRole("dialog", { name: "举报评论" })).toBeVisible()
    expect(screen.getByText(/不会在队列中看到举报人身份/)).toBeVisible()
    await user.click(screen.getByRole("button", { name: "取消" }))

    await user.click(screen.getAllByRole("button", { name: "回复" })[0])
    expect(screen.getByLabelText("写回复")).toHaveAttribute(
      "placeholder",
      "回复 @北岸…"
    )

    await user.click(screen.getByRole("button", { name: "取消回复" }))
    await user.click(screen.getByRole("button", { name: "编辑" }))
    expect(screen.getByLabelText("编辑评论")).toHaveValue("这份资源已经校验。")
  })

  it("keeps public reading available while anonymous", () => {
    const queryClient = commentsTestClient()
    queryClient.setQueryData(sessionKeys.current(), null)
    queryClient.setQueryData(commentKeys.page(target, 20, 0), {
      items: [],
      total: 0,
      limit: 20,
      offset: 0,
    } satisfies CommentPage)

    renderComments(queryClient)

    expect(screen.getByText("登录后参与评论与回复。")).toBeVisible()
    expect(screen.getByRole("link", { name: "登录" })).toHaveAttribute(
      "href",
      "/login"
    )
    expect(screen.getByText("暂无评论，来发表第一条评论吧")).toBeVisible()
  })
})

function commentPage(): CommentPage {
  return {
    total: 3,
    limit: 20,
    offset: 0,
    items: [
      {
        id: rootCommentId,
        author: { id: userId, display_name: "北岸" },
        body: "这份资源已经校验。",
        body_format: "plain_text",
        state: "visible",
        version: 1,
        created_at: "2026-08-09T10:00:00Z",
        updated_at: "2026-08-09T10:00:00Z",
      },
      {
        id: "0198f20a-6da8-7e51-9c64-444444444444",
        parent_comment_id: rootCommentId,
        author: {
          id: "0198f20a-6da8-7e51-9c64-555555555555",
          display_name: "海风",
        },
        body: "",
        body_format: "plain_text",
        state: "author_deleted",
        version: 2,
        created_at: "2026-08-09T10:05:00Z",
        updated_at: "2026-08-09T10:06:00Z",
      },
      {
        id: "0198f20a-6da8-7e51-9c64-666666666666",
        author: {
          id: "0198f20a-6da8-7e51-9c64-777777777777",
          display_name: "灯塔",
        },
        body: "这是一条可由当前成员举报的公开评论。",
        body_format: "plain_text",
        state: "visible",
        version: 1,
        created_at: "2026-08-09T10:08:00Z",
        updated_at: "2026-08-09T10:08:00Z",
      },
    ],
  }
}

function capability(action: string) {
  return {
    action,
    description: action,
    scope: { type: "self" as const, id: userId },
    expires_at: "2026-08-10T12:00:00Z",
  }
}

function commentsTestClient() {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } })
}

function renderComments(queryClient: QueryClient) {
  render(
    <MemoryRouter>
      <QueryClientProvider client={queryClient}>
        <TorrentCommentsCard torrentId={torrentId} />
      </QueryClientProvider>
    </MemoryRouter>
  )
}

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { fireEvent, render, screen, within } from "@testing-library/react"
import { MemoryRouter, Route, Routes } from "react-router"
import { describe, expect, it } from "vitest"

import { sessionKeys } from "~/features/auth/api/session.mutations"
import { capabilityKeys } from "~/features/authz/api/capabilities.queries"
import {
  commentKeys,
  type CommentPage,
  postCommentTarget,
} from "~/features/social/api/comments.queries"
import { socialPostKeys } from "~/features/social/api/posts.queries"
import { SocialPostDetailPage } from "~/features/social/pages/social-post-detail-page"

const postId = "0198f20a-6da8-7e51-9c64-222222222222"
const userId = "0198f20a-6da8-7e51-9c64-111111111111"

function commentAuthor() {
  return {
    id: userId,
    username: "demo",
    display_name: "演示用户",
    online: true,
    vip: false,
    administrator: false,
    medals: [],
  }
}

describe("SocialPostDetailPage", () => {
  it("renders the PtYes-compatible detail and flat comment frame", () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false, staleTime: Infinity } },
    })
    queryClient.setQueryData(sessionKeys.current(), {
      user: {
        id: userId,
        username: "demo",
        display_name: "演示用户",
        email_verified: true,
      },
      expires_at: "2026-08-14T08:00:00Z",
      csrf_token: "a".repeat(43),
    })
    queryClient.setQueryData(capabilityKeys.current(userId), {
      policy_version: "test",
      items: [
        {
          action: "social.post.comment.create.self",
          description: "发表评论",
          scope: { type: "site", id: "site" },
          expires_at: "2026-08-14T08:00:00Z",
        },
        {
          action: "comment.update.self",
          description: "编辑自己的评论",
          scope: { type: "self", id: userId },
          expires_at: "2026-08-14T08:00:00Z",
        },
        {
          action: "comment.delete.self",
          description: "删除自己的评论",
          scope: { type: "self", id: userId },
          expires_at: "2026-08-14T08:00:00Z",
        },
      ],
    })
    queryClient.setQueryData(socialPostKeys.detail(postId), {
      id: postId,
      author: {
        id: userId,
        username: "demo",
        display_name: "演示用户",
      },
      content: "欢迎来到 #PeerGo 动态圈",
      version: 1,
      comment_count: 1,
      created_at: "2026-08-13T06:00:00Z",
      updated_at: "2026-08-13T06:00:00Z",
    })
    queryClient.setQueryData(
      commentKeys.page(postCommentTarget(postId), 20, 0),
      {
        items: [
          {
            id: "0198f20a-6da8-7e51-9c64-333333333333",
            author: commentAuthor(),
            body: "欢迎来到新的动态讨论。",
            body_format: "plain_text",
            state: "visible",
            version: 1,
            created_at: "2026-08-13T06:05:00Z",
            updated_at: "2026-08-13T06:05:00Z",
          },
          {
            id: "0198f20a-6da8-7e51-9c64-444444444444",
            parent_comment_id: "0198f20a-6da8-7e51-9c64-333333333333",
            author: commentAuthor(),
            body: "这是楼中楼回复。",
            body_format: "plain_text",
            state: "visible",
            version: 1,
            created_at: "2026-08-13T06:06:00Z",
            updated_at: "2026-08-13T06:06:00Z",
          },
          {
            id: "0198f20a-6da8-7e51-9c64-555555555555",
            parent_comment_id: "0198f20a-6da8-7e51-9c64-000000000000",
            author: commentAuthor(),
            body: "父评论不在当前页的回复。",
            body_format: "plain_text",
            state: "visible",
            version: 1,
            created_at: "2026-08-13T06:07:00Z",
            updated_at: "2026-08-13T06:07:00Z",
          },
        ],
        total: 3,
        limit: 20,
        offset: 0,
      } satisfies CommentPage
    )

    render(
      <MemoryRouter initialEntries={[`/social/post/${postId}`]}>
        <QueryClientProvider client={queryClient}>
          <Routes>
            <Route
              path="/social/post/:postId"
              element={<SocialPostDetailPage />}
            />
          </Routes>
        </QueryClientProvider>
      </MemoryRouter>
    )

    expect(
      screen.getByRole("heading", { level: 1, name: "动态详情" })
    ).toHaveClass("text-3xl")
    expect(screen.getByRole("main")).toHaveClass(
      "max-w-[704px]",
      "lg:max-w-[720px]"
    )
    expect(screen.getByRole("button", { name: "返回动态圈" })).toHaveAttribute(
      "href",
      "/social"
    )
    expect(screen.getByPlaceholderText("写下你的评论...")).toHaveClass(
      "min-h-[60px]"
    )
    expect(screen.getByRole("button", { name: "发送" })).toHaveClass("h-10")
    expect(screen.getByRole("button", { name: "发送" })).toBeDisabled()
    expect(screen.getByText("1", { selector: "article span" })).toHaveClass(
      "h-9",
      "shrink-0",
      "whitespace-nowrap"
    )
    expect(screen.getByText("共 3 条评论")).toBeVisible()
    expect(screen.getByRole("button", { name: "最新" })).toHaveClass(
      "h-7",
      "text-xs"
    )
    const comments = screen.getByRole("region", { name: "评论列表" })
    const newestComment = within(comments).getByText("这是楼中楼回复。")
    const oldestComment = within(comments).getByText("欢迎来到新的动态讨论。")
    expect(
      oldestComment.compareDocumentPosition(newestComment) &
        Node.DOCUMENT_POSITION_FOLLOWING
    ).toBeTruthy()
    fireEvent.click(screen.getByRole("button", { name: "最新" }))
    expect(screen.getByRole("button", { name: "最早" })).toBeVisible()
    expect(
      oldestComment.compareDocumentPosition(newestComment) &
        Node.DOCUMENT_POSITION_FOLLOWING
    ).toBeTruthy()
    expect(within(comments).getByText("欢迎来到新的动态讨论。")).toBeVisible()
    expect(within(comments).getByText("父评论不在当前页的回复。")).toBeVisible()
    expect(
      within(comments).getByText("这是楼中楼回复。").closest("article")
    ).toHaveClass("ml-8", "py-3", "border-l-2", "sm:ml-11")
    expect(
      within(comments).getByText("这是楼中楼回复。").closest("article")
    ).toHaveAttribute("id", "comment-0198f20a-6da8-7e51-9c64-444444444444")
    expect(
      within(comments).getAllByRole("button", { name: "回复" })
    ).toHaveLength(3)
    const rootArticle = within(comments)
      .getByText("欢迎来到新的动态讨论。")
      .closest("article")!
    fireEvent.click(within(rootArticle).getByRole("button", { name: "回复" }))
    expect(
      within(rootArticle).getByPlaceholderText("回复 @演示用户…")
    ).toHaveClass("min-h-[76px]", "resize-y")
    expect(screen.getAllByLabelText("演示用户在线").length).toBeGreaterThan(0)
  })

  it("uses the PtYes-sized empty comment state", () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false, staleTime: Infinity } },
    })
    queryClient.setQueryData(sessionKeys.current(), {
      user: {
        id: userId,
        username: "demo",
        display_name: "演示用户",
        email_verified: true,
      },
      expires_at: "2026-08-14T08:00:00Z",
      csrf_token: "a".repeat(43),
    })
    queryClient.setQueryData(capabilityKeys.current(userId), {
      policy_version: "test",
      items: [],
    })
    queryClient.setQueryData(socialPostKeys.detail(postId), {
      id: postId,
      author: {
        id: userId,
        username: "demo",
        display_name: "演示用户",
      },
      content: "暂无评论的动态",
      version: 1,
      comment_count: 0,
      created_at: "2026-08-13T06:00:00Z",
      updated_at: "2026-08-13T06:00:00Z",
    })
    queryClient.setQueryData(
      commentKeys.page(postCommentTarget(postId), 20, 0),
      {
        items: [],
        total: 0,
        limit: 20,
        offset: 0,
      } satisfies CommentPage
    )

    render(
      <MemoryRouter initialEntries={[`/social/post/${postId}`]}>
        <QueryClientProvider client={queryClient}>
          <Routes>
            <Route
              path="/social/post/:postId"
              element={<SocialPostDetailPage />}
            />
          </Routes>
        </QueryClientProvider>
      </MemoryRouter>
    )

    const emptyDescription = screen.getByText("暂无评论，来发表第一条评论吧")
    const empty = emptyDescription.closest("[data-slot='empty']")
    expect(empty).toHaveClass("mt-16", "min-h-32", "py-8")
    expect(emptyDescription).toHaveClass("text-base", "leading-6")
    const emptyIcon = empty?.querySelector("[data-slot='empty-icon']")
    expect(emptyIcon).toHaveClass("[&_svg]:size-8")
    expect(emptyIcon?.querySelector("svg")).toHaveClass("lucide-message-circle")
  })
})

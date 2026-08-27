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
  it("keeps replies inside their top-level thread with precise reply context", () => {
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
    const rootId = "0198f20a-6da8-7e51-9c64-333333333333"
    const secondRoot = {
      id: "0198f20a-6da8-7e51-9c64-555555555555",
      author: commentAuthor(),
      body: "第二条一级评论。",
      body_format: "plain_text" as const,
      state: "visible" as const,
      version: 1,
      created_at: "2026-08-13T06:10:00Z",
      updated_at: "2026-08-13T06:10:00Z",
    }
    const root = {
      id: rootId,
      author: commentAuthor(),
      body: "欢迎来到新的动态讨论。",
      body_format: "plain_text" as const,
      state: "visible" as const,
      version: 1,
      created_at: "2026-08-13T06:05:00Z",
      updated_at: "2026-08-13T06:05:00Z",
    }
    const replies = Array.from({ length: 4 }, (_, index) => ({
      id: `0198f20a-6da8-7e51-9c64-44444444444${index}`,
      parent_comment_id: rootId,
      root_comment_id: rootId,
      reply_to: commentAuthor(),
      author: commentAuthor(),
      body: index === 0 ? "这是楼中楼回复。" : `楼中楼回复 ${index + 1}。`,
      body_format: "plain_text" as const,
      state: "visible" as const,
      version: 1,
      created_at: `2026-08-13T06:0${index + 6}:00Z`,
      updated_at: `2026-08-13T06:0${index + 6}:00Z`,
    }))
    const newestPage = {
      items: [secondRoot, root, ...replies],
      total: 6,
      thread_total: 2,
      limit: 20,
      offset: 0,
    } satisfies CommentPage
    const oldestPage = {
      ...newestPage,
      items: [root, ...replies, secondRoot],
    } satisfies CommentPage
    queryClient.setQueryData(
      commentKeys.page(postCommentTarget(postId), 20, 0, "newest"),
      newestPage
    )
    queryClient.setQueryData(
      commentKeys.page(postCommentTarget(postId), 20, 0, "oldest"),
      oldestPage
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
    expect(screen.getByText("共 6 条评论")).toBeVisible()
    expect(screen.getByRole("button", { name: "热门" })).toBeVisible()
    expect(screen.getByRole("button", { name: "最新" })).toHaveClass(
      "h-7",
      "text-xs"
    )
    const comments = screen.getByRole("region", { name: "评论列表" })
    const newestComment = within(comments).getByText("第二条一级评论。")
    const oldestComment = within(comments).getByText("欢迎来到新的动态讨论。")
    expect(
      newestComment.compareDocumentPosition(oldestComment) &
        Node.DOCUMENT_POSITION_FOLLOWING
    ).toBeTruthy()
    expect(
      within(comments).queryByText("楼中楼回复 4。")
    ).not.toBeInTheDocument()
    fireEvent.click(
      within(comments).getByRole("button", { name: "查看全部 4 条回复" })
    )
    expect(within(comments).getByText("楼中楼回复 4。")).toBeVisible()
    expect(within(comments).getAllByText("回复 @演示用户")).toHaveLength(4)
    expect(
      within(comments).queryByText("回复 一条早先评论")
    ).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole("button", { name: "最早" }))
    expect(
      oldestComment.compareDocumentPosition(newestComment) &
        Node.DOCUMENT_POSITION_FOLLOWING
    ).toBeTruthy()
    expect(within(comments).getByText("欢迎来到新的动态讨论。")).toBeVisible()
    expect(
      within(comments).getByText("这是楼中楼回复。").closest("article")
    ).toHaveClass("ml-8", "py-3", "border-l-2", "sm:ml-11")
    expect(
      within(comments).getByText("这是楼中楼回复。").closest("article")
    ).toHaveAttribute("id", "comment-0198f20a-6da8-7e51-9c64-444444444440")
    expect(
      within(comments).getAllByRole("button", { name: "回复" })
    ).toHaveLength(6)
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
      commentKeys.page(postCommentTarget(postId), 20, 0, "newest"),
      {
        items: [],
        total: 0,
        thread_total: 0,
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

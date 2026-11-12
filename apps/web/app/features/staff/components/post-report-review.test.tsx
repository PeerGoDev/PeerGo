import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen, waitFor, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter } from "react-router"
import { afterEach, describe, expect, it, vi } from "vitest"

import {
  postModerationCasesQueryOptions,
  type SocialPostModerationCase,
} from "~/features/staff/api/post-moderation.queries"
import { PostReportReview } from "~/features/staff/components/post-report-review"

const caseId = "0198f20a-6da8-7e51-9c64-555555555555"
const postId = "0198f20a-6da8-7e51-9c64-222222222222"

describe("PostReportReview", () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it("hides a reported post against the reviewed versions", async () => {
    const user = userEvent.setup()
    const fetchMock = vi.fn(async (request: Request) =>
      request.method === "POST"
        ? jsonResponse(201, {
            decision_id: "0198f20a-6da8-7e51-9c64-666666666666",
            case_id: caseId,
            post_id: postId,
            decision: "hide_post",
            reason_code: "harassment",
            case_state: "post_hidden",
            post_state: "moderator_hidden",
            case_version: 2,
            post_version: 3,
            decided_at: "2026-11-12T09:00:00Z",
          })
        : jsonResponse(200, { items: [], total: 0, limit: 20, offset: 0 })
    )
    vi.stubGlobal("fetch", fetchMock)
    const onDecided = vi.fn()
    renderReview(moderationCase(), { canResolve: true, onDecided })

    expect(screen.getByText("需要审核的动态正文")).toBeVisible()
    expect(screen.getByText("2 人举报")).toBeVisible()
    await user.click(screen.getByRole("button", { name: "审核" }))
    const dialog = await screen.findByRole("dialog")
    await user.click(within(dialog).getByRole("combobox", { name: "处置结果" }))
    await user.click(await screen.findByRole("option", { name: "隐藏动态" }))
    await user.click(within(dialog).getByRole("combobox", { name: "违规类别" }))
    await user.click(
      await screen.findByRole("option", { name: "骚扰或人身攻击" })
    )
    await user.click(within(dialog).getByRole("button", { name: "确认隐藏" }))

    await waitFor(() =>
      expect(onDecided).toHaveBeenCalledWith(
        "动态已隐藏，举报案件已关闭并留下处置记录。"
      )
    )
    const request = fetchMock.mock.calls
      .map(([call]) => call)
      .find((call) => call.method === "POST")
    expect(request?.url).toContain(
      `/api/v1/admin/social/post-moderation-cases/${caseId}/decisions`
    )
    expect(request?.headers.get("X-CSRF-Token")).toBe("s".repeat(43))
    expect(await request?.clone().json()).toEqual({
      expected_case_version: 1,
      expected_post_version: 2,
      decision: "hide_post",
      reason_code: "harassment",
      note: "",
    })
  })

  it("only closes cases whose post is no longer public", async () => {
    const user = userEvent.setup()
    const deleted = moderationCase()
    deleted.post = { ...deleted.post, state: "author_deleted", content: "" }
    renderReview(deleted, { canResolve: true })

    expect(screen.getByText("该动态已由作者删除。")).toBeVisible()
    expect(screen.getByText("作者已删除")).toBeVisible()
    await user.click(screen.getByRole("button", { name: "审核" }))
    const dialog = await screen.findByRole("dialog")
    expect(
      within(dialog).getByText("动态当前已不再公开，只能核对举报后关闭案件。")
    ).toBeVisible()
    await user.click(within(dialog).getByRole("combobox", { name: "处置结果" }))
    expect(
      await screen.findByRole("option", { name: "确认无违规并关闭" })
    ).toBeVisible()
    expect(
      screen.queryByRole("option", { name: "隐藏动态" })
    ).not.toBeInTheDocument()
  })

  it("keeps the queue read-only without the resolve capability", () => {
    renderReview(moderationCase(), { canResolve: false })

    expect(screen.getByText("只读")).toBeVisible()
    expect(
      screen.queryByRole("button", { name: "审核" })
    ).not.toBeInTheDocument()
  })
})

function renderReview(
  item: SocialPostModerationCase,
  {
    canResolve,
    onDecided = () => {},
  }: { canResolve: boolean; onDecided?: (message: string) => void }
) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  })
  queryClient.setQueryData(postModerationCasesQueryOptions(20, 0).queryKey, {
    items: [item],
    total: 1,
    limit: 20,
    offset: 0,
  })
  render(
    <MemoryRouter>
      <QueryClientProvider client={queryClient}>
        <PostReportReview
          enabled
          canResolve={canResolve}
          csrfToken={"s".repeat(43)}
          onDecided={onDecided}
        />
      </QueryClientProvider>
    </MemoryRouter>
  )
}

function moderationCase(): SocialPostModerationCase {
  return {
    id: caseId,
    state: "open",
    version: 1,
    post: {
      id: postId,
      author: {
        id: "0198f20a-6da8-7e51-9c64-111111111111",
        username: "post-author",
        display_name: "动态作者",
      },
      board_id: "general",
      board_name: "生活茶馆",
      content: "需要审核的动态正文",
      state: "visible",
      version: 2,
      media_count: 0,
      created_at: "2026-11-12T06:00:00Z",
    },
    report_count: 2,
    reports: [
      {
        reason_code: "spam",
        details: "",
        created_at: "2026-11-12T07:00:00Z",
      },
      {
        reason_code: "harassment",
        details: "评论区持续骚扰",
        created_at: "2026-11-12T08:00:00Z",
      },
    ],
    opened_at: "2026-11-12T07:00:00Z",
    latest_reported_at: "2026-11-12T08:00:00Z",
  }
}

function jsonResponse(status: number, body: unknown) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  })
}

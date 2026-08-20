import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import { MemoryRouter, Route, Routes } from "react-router"
import { describe, expect, it } from "vitest"

import { sessionKeys } from "~/features/auth/api/session.mutations"
import { socialPostKeys } from "~/features/social/api/posts.queries"
import { UserSocialPostsPage } from "~/features/social/pages/user-social-posts-page"
import { userKeys } from "~/features/user/api/user.queries"

const userId = "0198f20a-6da8-7e51-9c64-111111111111"

describe("UserSocialPostsPage", () => {
  it("keeps the PtYes content width and header spacing", () => {
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
    queryClient.setQueryData(userKeys.profile("demo"), {
      username: "demo",
      display_name: "演示用户",
      joined_at: "2026-08-01T08:00:00Z",
      published_torrent_count: 0,
    })
    queryClient.setQueryData(socialPostKeys.infinite("newest", 20, "demo"), {
      pages: [
        {
          items: [
            {
              id: "0198f20a-6da8-7e51-9c64-222222222222",
              author: {
                id: userId,
                username: "demo",
                display_name: "演示用户",
              },
              content: "用户动态页面布局测试",
              version: 1,
              comment_count: 0,
              created_at: "2026-08-13T06:00:00Z",
              updated_at: "2026-08-13T06:00:00Z",
            },
          ],
          total: 1,
          limit: 20,
          offset: 0,
          sort: "newest",
        },
      ],
      pageParams: [0],
    })

    render(
      <MemoryRouter initialEntries={["/social/user/demo"]}>
        <QueryClientProvider client={queryClient}>
          <Routes>
            <Route
              path="/social/user/:username"
              element={<UserSocialPostsPage />}
            />
          </Routes>
        </QueryClientProvider>
      </MemoryRouter>
    )

    expect(screen.getByRole("main")).toHaveClass(
      "max-w-[704px]",
      "lg:max-w-[720px]"
    )
    const heading = screen.getByRole("heading", { name: "demo 的动态" })
    expect(heading.closest("header")).toHaveClass("gap-4")
    expect(screen.getByText("共 1 条")).toBeVisible()
    expect(screen.getByText("用户动态页面布局测试")).toBeVisible()
    expect(
      screen.getByRole("button", { name: "返回成员资料" })
    ).toHaveAttribute("href", "/user/demo")
  })
})

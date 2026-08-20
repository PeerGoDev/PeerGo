import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import { MemoryRouter, Route, Routes, useLocation } from "react-router"
import { describe, expect, it } from "vitest"

import { sessionKeys } from "~/features/auth/api/session.mutations"
import { workgroupKeys } from "~/features/workgroups/api/workgroups.queries"
import LegacyReviewRoute from "~/routes/legacy-review"

const userId = "0198f20a-6da8-7e51-9c64-111111111111"

describe("LegacyReviewRoute", () => {
  it("sends a regular member to their upload review status", () => {
    const queryClient = reviewRouteClient(false)

    renderReviewRoute(queryClient)

    expect(screen.getByTestId("location")).toHaveTextContent(
      "/account/submissions?from=sidebar"
    )
  })

  it("sends an active review workgroup member to the voting queue", () => {
    const queryClient = reviewRouteClient(true)

    renderReviewRoute(queryClient)

    expect(screen.getByTestId("location")).toHaveTextContent(
      "/review/queue?from=sidebar"
    )
  })
})

function reviewRouteClient(activeReviewer: boolean) {
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
    expires_at: "2026-08-14T00:00:00Z",
    csrf_token: "c".repeat(43),
  })
  queryClient.setQueryData(workgroupKeys.mine(userId), {
    items: [
      {
        definition: { kind: "review" },
        membership: activeReviewer ? { status: "active" } : null,
      },
    ],
  })
  return queryClient
}

function renderReviewRoute(queryClient: QueryClient) {
  render(
    <MemoryRouter initialEntries={["/review?from=sidebar"]}>
      <QueryClientProvider client={queryClient}>
        <Routes>
          <Route path="/review" element={<LegacyReviewRoute />} />
          <Route path="*" element={<LocationProbe />} />
        </Routes>
      </QueryClientProvider>
    </MemoryRouter>
  )
}

function LocationProbe() {
  const location = useLocation()
  return (
    <output data-testid="location">
      {location.pathname + location.search}
    </output>
  )
}

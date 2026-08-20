import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import { MemoryRouter } from "react-router"
import { describe, expect, it } from "vitest"

import { sessionKeys } from "~/features/auth/api/session.mutations"
import { capabilityKeys } from "~/features/authz/api/capabilities.queries"
import {
  myNewcomerAssessmentQueryOptions,
  type MyNewcomerAssessmentStatus,
} from "~/features/newcomer/api/newcomer.queries"
import { NewcomerAssessmentPage } from "~/features/newcomer/pages/newcomer-assessment-page"

const userId = "0198f20a-6da8-7e51-9c64-111111111111"

describe("NewcomerAssessmentPage", () => {
  it("only renders the tasks enabled by the frozen policy", () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false, staleTime: Infinity } },
    })
    queryClient.setQueryData(sessionKeys.current(), {
      user: {
        id: userId,
        username: "newcomer",
        display_name: "新人",
        email_verified: true,
      },
      expires_at: "2026-08-20T08:00:00Z",
      csrf_token: "c".repeat(43),
    })
    queryClient.setQueryData(capabilityKeys.current(userId), {
      policy_version: "current",
      items: [
        {
          action: "newcomer.assessment.read.self",
          description: "查看自己的新人考核",
          scope: { type: "site", id: "peergo" },
          expires_at: "2026-08-20T08:00:00Z",
        },
      ],
    })
    queryClient.setQueryData(
      myNewcomerAssessmentQueryOptions(userId).queryKey,
      {
        observed_at: "2026-08-19T08:00:00Z",
        assessment: {
          status: "active",
          started_at: "2026-08-18T08:00:00Z",
          deadline_at: "2026-09-17T08:00:00Z",
          minimum_credited_upload_bytes: "53687091200",
          minimum_seeding_active_seconds: 0,
          current_credited_upload_bytes: "10737418240",
          current_seeding_active_seconds: 0,
          restriction_started_at: null,
          resolved_at: null,
          updated_at: "2026-08-19T08:00:00Z",
        },
      } satisfies MyNewcomerAssessmentStatus
    )

    render(
      <MemoryRouter>
        <QueryClientProvider client={queryClient}>
          <NewcomerAssessmentPage />
        </QueryClientProvider>
      </MemoryRouter>
    )

    expect(
      screen.getByText(
        "1 项任务均达标即可提前通过；受限后继续贡献也会自动恢复。"
      )
    ).toBeVisible()
    expect(screen.getByText("有效上传")).toBeVisible()
    expect(screen.queryByText("做种时长")).not.toBeInTheDocument()
  })
})

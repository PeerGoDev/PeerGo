import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter } from "react-router"
import { describe, expect, it } from "vitest"

import { sessionKeys } from "~/features/auth/api/session.mutations"
import {
  type MyWorkgroupOverview,
  workgroupKeys,
} from "~/features/workgroups/api/workgroups.queries"
import { WorkgroupsPage } from "~/features/workgroups/pages/workgroups-page"

const userId = "0198f20a-6da8-7e51-9c64-111111111111"

describe("WorkgroupsPage", () => {
  it("shows evidence-backed monthly progress and its history without implying automatic suspension", async () => {
    const user = userEvent.setup()
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false, staleTime: Infinity } },
    })
    queryClient.setQueryData(sessionKeys.current(), {
      user: {
        id: userId,
        username: "reviewer",
        display_name: "种审成员",
        email_verified: true,
      },
      expires_at: "2026-09-01T00:00:00Z",
      csrf_token: "c".repeat(43),
    })
    queryClient.setQueryData(workgroupKeys.mine(userId), {
      items: [
        workgroup("reseed", "转种组"),
        {
          ...workgroup("review", "种审组"),
          membership: {
            id: "0198f20a-6da8-7e51-9c64-222222222222",
            group_kind: "review",
            user_id: userId,
            user_numeric_id: 42,
            username: "reviewer",
            display_name: "种审成员",
            status: "active",
            source: "application",
            version: 1,
            started_at: "2026-08-01T00:00:00Z",
            updated_at: "2026-08-01T00:00:00Z",
            contribution: {
              group_kind: "review",
              metric: "torrent_review_votes",
              policy_revision: 1,
              period_kind: "calendar_month",
              period_starts_at: "2026-08-01T00:00:00Z",
              period_ends_at: "2026-09-01T00:00:00Z",
              observed_at: "2026-08-17T00:00:00Z",
              current_value: 12,
              target_value: 20,
              met: false,
              enforcement_mode: "observe",
              allowed_misses: 0,
              miss_count: 0,
            },
          },
        },
        workgroup("retention", "保种组"),
      ],
    } satisfies MyWorkgroupOverview)
    queryClient.setQueryData(
      workgroupKeys.contributionCycles(userId, "review"),
      {
        items: [
          {
            group_kind: "review",
            metric: "torrent_review_votes",
            policy_revision: 1,
            period_starts_at: "2026-08-01T00:00:00Z",
            period_ends_at: "2026-09-01T00:00:00Z",
            observed_at: "2026-08-17T00:00:00Z",
            evidence_through: "2026-08-17T00:00:00Z",
            evidence_state: "collecting",
            active_seconds: 16 * 86400,
            full_period_active: true,
            current_value: 12,
            target_value: 20,
            assessment_state: "in_progress",
            explanation_code: "period_in_progress",
            enforcement_mode: "observe",
            allowed_misses: 0,
            reminder: null,
            enforcement: null,
          },
        ],
        limit: 6,
      }
    )
    queryClient.setQueryData(workgroupKeys.tasks(userId), {
      items: [
        {
          id: "0198f20a-6da8-7e51-9c64-333333333333",
          user_numeric_id: 42,
          username: "reviewer",
          display_name: "种审成员",
          task: {
            id: "0198f20a-6da8-7e51-9c64-444444444444",
            group_kind: "review",
            task_type: "task",
            title: "完成首轮种子审核",
            description: "核对待审种子的媒体信息并提交审核结果。",
            starts_at: "2026-08-17T00:00:00Z",
            due_at: "2026-08-24T00:00:00Z",
            timeline_state: "open",
            assignment_count: 4,
            submitted_count: 1,
            pending_review_count: 1,
            accepted_count: 0,
            created_at: "2026-08-16T00:00:00Z",
            replayed: false,
          },
          state: "not_submitted",
          can_submit: true,
          latest_submission: null,
        },
      ],
      total: 1,
      limit: 50,
      offset: 0,
    })

    render(
      <MemoryRouter>
        <QueryClientProvider client={queryClient}>
          <WorkgroupsPage />
        </QueryClientProvider>
      </MemoryRouter>
    )

    expect(screen.getByText("本月有效审核")).toBeVisible()
    expect(screen.getByText("12 票 / 20 票")).toBeVisible()
    expect(screen.getByText("进行中")).toBeVisible()
    expect(
      screen.getByText("当前为观察目标，未达标不会自动变更工作组权益。")
    ).toBeVisible()
    expect(screen.getByText("完成首轮种子审核")).toBeVisible()
    expect(screen.getByRole("button", { name: "提交成果" })).toBeVisible()

    await user.click(screen.getByRole("button", { name: "查看贡献历史" }))
    expect(screen.getByRole("dialog", { name: "种审组贡献历史" })).toBeVisible()
    expect(screen.getByText("2026年8月")).toBeVisible()
    expect(screen.getByText("采集中")).toBeVisible()
    expect(
      screen.getByText("本月尚未结束，当前数值不是最终结果。")
    ).toBeVisible()
  })
})

function workgroup(
  kind: "reseed" | "review" | "retention",
  displayName: string
) {
  const entitlement =
    kind === "reseed"
      ? "torrent.publish.trusted"
      : kind === "review"
        ? "torrent.review.vote"
        : "traffic.download.charge_exempt"
  return {
    definition: {
      kind,
      display_name: displayName,
      description: `${displayName}说明`,
      join_mode: kind === "review" ? "application" : "staff_only",
      entitlement,
      enabled: true,
      sort_order: kind === "reseed" ? 10 : kind === "review" ? 20 : 30,
      version: 1,
    },
  } as MyWorkgroupOverview["items"][number]
}

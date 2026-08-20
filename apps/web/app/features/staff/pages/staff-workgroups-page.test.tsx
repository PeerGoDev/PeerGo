import { QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter } from "react-router"
import { describe, expect, it } from "vitest"

import {
  adminWorkgroupApplicationsQueryOptions,
  adminWorkgroupContributionCyclesQueryOptions,
  adminWorkgroupContributionPoliciesQueryOptions,
  adminWorkgroupMembershipsQueryOptions,
  adminWorkgroupOverviewQueryOptions,
  adminWorkgroupTaskAssignmentsQueryOptions,
  adminWorkgroupTasksQueryOptions,
} from "~/features/staff/api/workgroup-administration.queries"
import { StaffWorkgroupsPage } from "~/features/staff/pages/staff-workgroups-page"
import { createStaffPageQueryClient } from "~/features/staff/test-utils/create-staff-page-query-client"

describe("StaffWorkgroupsPage", () => {
  it("shows group contribution summaries and immutable target history", async () => {
    const user = userEvent.setup()
    const queryClient = createStaffPageQueryClient([
      "workgroup.manage.read",
      "workgroup.contribution.policy.issue",
      "workgroup.task.publish",
      "workgroup.task.review",
    ])
    queryClient.setQueryData(adminWorkgroupOverviewQueryOptions.queryKey, {
      definitions: [
        definition("reseed", "转种组", "torrent.publish.trusted"),
        definition("review", "种审组", "torrent.review.vote"),
        definition("retention", "保种组", "traffic.download.charge_exempt"),
      ],
      pending_applications: 2,
      active_reseed_members: 3,
      active_review_members: 4,
      active_retention_members: 5,
      contribution_summaries: [
        contributionSummary("reseed", "trusted_torrents_published", 6, 2),
        contributionSummary("review", "torrent_review_votes", 48, 20),
        contributionSummary(
          "retention",
          "seeding_active_seconds",
          864000,
          604800
        ),
      ],
    })
    queryClient.setQueryData(
      adminWorkgroupApplicationsQueryOptions("pending").queryKey,
      { items: [], total: 0, limit: 100, offset: 0 }
    )
    queryClient.setQueryData(
      adminWorkgroupMembershipsQueryOptions("review", "").queryKey,
      {
        items: [reviewMembership],
        total: 1,
        limit: 100,
        offset: 0,
      }
    )
    queryClient.setQueryData(
      adminWorkgroupContributionCyclesQueryOptions(
        "review",
        reviewMembership.id
      ).queryKey,
      {
        items: [
          {
            group_kind: "review",
            metric: "torrent_review_votes",
            policy_revision: 1,
            period_starts_at: "2026-07-01T00:00:00Z",
            period_ends_at: "2026-08-01T00:00:00Z",
            observed_at: "2026-08-17T00:00:00Z",
            evidence_through: "2026-07-20T00:00:00Z",
            evidence_state: "incomplete",
            active_seconds: 31 * 86400,
            full_period_active: true,
            current_value: 10,
            target_value: 20,
            assessment_state: "indeterminate",
            explanation_code: "evidence_incomplete",
            enforcement_mode: "observe",
            reminder: null,
          },
        ],
        limit: 12,
      }
    )
    queryClient.setQueryData(
      adminWorkgroupContributionPoliciesQueryOptions("review").queryKey,
      {
        items: [
          {
            group_kind: "review",
            revision: 1,
            metric: "torrent_review_votes",
            period_kind: "calendar_month",
            target_value: 20,
            enforcement_mode: "observe",
            effective_from: null,
            opening: true,
            reason: "PeerGo 首版种审组观察目标",
            created_at: "2026-08-17T00:00:00Z",
            timeline_state: "active",
            replayed: false,
          },
        ],
        total: 1,
        limit: 100,
        offset: 0,
        minimum_effective_from: "2026-09-01T00:00:00Z",
        current: {
          group_kind: "review",
          revision: 1,
          metric: "torrent_review_votes",
          period_kind: "calendar_month",
          target_value: 20,
          enforcement_mode: "observe",
          effective_from: null,
          opening: true,
          reason: "PeerGo 首版种审组观察目标",
          created_at: "2026-08-17T00:00:00Z",
          timeline_state: "active",
          replayed: false,
        },
      }
    )
    queryClient.setQueryData(
      adminWorkgroupTasksQueryOptions("review").queryKey,
      {
        items: [reviewTask],
        total: 1,
        limit: 50,
        offset: 0,
      }
    )
    queryClient.setQueryData(
      adminWorkgroupTaskAssignmentsQueryOptions("review", reviewTask.id)
        .queryKey,
      {
        items: [
          {
            id: "0198f20a-6da8-7e51-9c64-555555555555",
            user_numeric_id: 42,
            username: "reviewer",
            display_name: "种审成员",
            task: reviewTask,
            state: "pending_review",
            can_submit: false,
            latest_submission: {
              id: "0198f20a-6da8-7e51-9c64-666666666666",
              sequence: 1,
              statement: "已经核对种子媒体信息并完成首轮审核，请工作人员复核。",
              submitted_at: "2026-08-17T08:00:00Z",
              decision: null,
              review_reason: null,
              decided_at: null,
            },
          },
        ],
        total: 1,
        limit: 100,
        offset: 0,
      }
    )

    render(
      <MemoryRouter initialEntries={["/staff/workgroups"]}>
        <QueryClientProvider client={queryClient}>
          <StaffWorkgroupsPage />
        </QueryClientProvider>
      </MemoryRouter>
    )

    expect(await screen.findByText("种审组 · 本月贡献")).toBeVisible()
    expect(screen.getByText("贡献目标 · 种审组")).toBeVisible()
    expect(screen.getByText("PeerGo 首版种审组观察目标")).toBeVisible()
    expect(screen.getByText("2026 年 9 月")).toBeVisible()
    expect(screen.getByText("任务与活动 · 种审组")).toBeVisible()
    expect(screen.getByText("完成首轮种子审核")).toBeVisible()

    await user.click(screen.getByRole("button", { name: "查看成员" }))
    expect(
      screen.getByRole("dialog", { name: "完成首轮种子审核" })
    ).toBeVisible()
    expect(screen.getByRole("button", { name: "通过" })).toBeVisible()
    await user.keyboard("{Escape}")

    await user.click(screen.getByRole("button", { name: "设置后续目标" }))
    expect(
      screen.getByRole("dialog", { name: "设置种审组后续贡献目标" })
    ).toBeVisible()
    expect(screen.getByLabelText("生效月份")).toHaveValue("2026-09")

    await user.keyboard("{Escape}")
    await user.click(screen.getByRole("button", { name: "历史" }))
    expect(
      screen.getByRole("dialog", { name: "reviewer · 贡献历史" })
    ).toBeVisible()
    expect(screen.getByText("证据有缺口")).toBeVisible()
    expect(screen.getByText("待补证据")).toBeVisible()
    expect(screen.getByText("证据时间窗不连续，暂不能形成结论。")).toBeVisible()
  })
})

const reviewMembership = {
  id: "0198f20a-6da8-7e51-9c64-222222222222",
  group_kind: "review" as const,
  user_id: "0198f20a-6da8-7e51-9c64-111111111111",
  user_numeric_id: 42,
  username: "reviewer",
  display_name: "种审成员",
  status: "active" as const,
  source: "application" as const,
  version: 1,
  started_at: "2026-07-01T00:00:00Z",
  updated_at: "2026-07-01T00:00:00Z",
  contribution: {
    group_kind: "review" as const,
    metric: "torrent_review_votes" as const,
    policy_revision: 1,
    period_kind: "calendar_month" as const,
    period_starts_at: "2026-08-01T00:00:00Z",
    period_ends_at: "2026-09-01T00:00:00Z",
    observed_at: "2026-08-17T00:00:00Z",
    current_value: 12,
    target_value: 20,
    met: false,
    enforcement_mode: "observe" as const,
  },
}

const reviewTask = {
  id: "0198f20a-6da8-7e51-9c64-444444444444",
  group_kind: "review" as const,
  task_type: "task" as const,
  title: "完成首轮种子审核",
  description: "核对待审种子的媒体信息并提交审核结果。",
  starts_at: "2026-08-17T00:00:00Z",
  due_at: "2026-08-24T00:00:00Z",
  timeline_state: "open" as const,
  assignment_count: 4,
  submitted_count: 1,
  pending_review_count: 1,
  accepted_count: 0,
  created_at: "2026-08-16T00:00:00Z",
  replayed: false,
}

function definition(
  kind: "reseed" | "review" | "retention",
  displayName: string,
  entitlement:
    | "torrent.publish.trusted"
    | "torrent.review.vote"
    | "traffic.download.charge_exempt"
) {
  return {
    kind,
    display_name: displayName,
    description: displayName + "职责",
    join_mode:
      kind === "review" ? ("application" as const) : ("staff_only" as const),
    entitlement,
    enabled: true,
    sort_order: kind === "reseed" ? 10 : kind === "review" ? 20 : 30,
    version: 1,
  }
}

function contributionSummary(
  groupKind: "reseed" | "review" | "retention",
  metric:
    | "trusted_torrents_published"
    | "torrent_review_votes"
    | "seeding_active_seconds",
  totalValue: number,
  targetValue: number
) {
  return {
    group_kind: groupKind,
    metric,
    policy_revision: 1,
    period_starts_at: "2026-08-01T00:00:00Z",
    period_ends_at: "2026-09-01T00:00:00Z",
    observed_at: "2026-08-17T00:00:00Z",
    active_members: 3,
    contributing_members: 2,
    met_members: 1,
    total_value: totalValue,
    target_value: targetValue,
  }
}

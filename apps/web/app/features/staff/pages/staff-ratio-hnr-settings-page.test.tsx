import { QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import { MemoryRouter } from "react-router"
import { describe, expect, it } from "vitest"

import { hnrPolicyRevisionListQueryOptions } from "~/features/staff/api/hnr-policy-administration.queries"
import { hnrAppealListQueryOptions } from "~/features/staff/api/hnr-appeal-administration.queries"
import {
  ratioWatchAppealListQueryOptions,
  ratioWatchAssessmentListQueryOptions,
  ratioWatchPolicyListQueryOptions,
} from "~/features/staff/api/ratio-watch-administration.queries"
import { StaffRatioHNRSettingsPage } from "~/features/staff/pages/staff-ratio-hnr-settings-page"
import { createStaffPageQueryClient } from "~/features/staff/test-utils/create-staff-page-query-client"

describe("StaffRatioHNRSettingsPage", () => {
  it("separates per-torrent H&R from long-term account ratio monitoring", async () => {
    const queryClient = createStaffPageQueryClient([
      "hnr.policy.read",
      "hnr.policy.issue",
      "ratio.policy.read",
      "ratio.policy.issue",
      "hnr.assessment.manage",
    ])
    queryClient.setQueryData(hnrPolicyRevisionListQueryOptions().queryKey, {
      items: [],
      total: "0",
      limit: 30,
      offset: 0,
      minimum_effective_from: "2026-08-16T08:05:00Z",
      current: {
        configured: true,
        revision_id: "hnr-global-v1",
        effective_at: "2026-08-16T08:00:00Z",
        rule_id: "global-default",
        rule_version: 1,
        mode: "enforced" as const,
        required_seed_seconds: 259200,
        required_ratio_basis_points: 10000,
        assessment_window_seconds: 604800,
        grace_period_seconds: 86400,
        max_interval_credit_seconds: 3600,
      },
      global_ratio_watch_connected: false,
    })
    queryClient.setQueryData(ratioWatchPolicyListQueryOptions().queryKey, {
      items: [
        {
          id: "550e8400-e29b-41d4-a716-446655440000",
          rule_id: "global-default",
          rule_version: 1,
          enabled: true,
          download_threshold_bytes: "53687091200",
          minimum_ratio_basis_points: 4000,
          watch_period_seconds: 1209600,
          restriction_ratio_basis_points: 3000,
          vip_exempt: true,
          effective_at: "2026-08-16T08:00:00Z",
          reason: "按旧站规则签发首个长期分享率版本。",
          created_at: "2026-08-16T07:55:00Z",
          timeline_state: "active" as const,
          replayed: false,
        },
      ],
      total: "1",
      limit: 30,
      offset: 0,
      minimum_effective_from: "2026-08-16T08:05:00Z",
      current: {
        id: "550e8400-e29b-41d4-a716-446655440000",
        rule_id: "global-default",
        rule_version: 1,
        enabled: true,
        download_threshold_bytes: "53687091200",
        minimum_ratio_basis_points: 4000,
        watch_period_seconds: 1209600,
        restriction_ratio_basis_points: 3000,
        vip_exempt: true,
        effective_at: "2026-08-16T08:00:00Z",
        reason: "按旧站规则签发首个长期分享率版本。",
        created_at: "2026-08-16T07:55:00Z",
        timeline_state: "active" as const,
        replayed: false,
      },
      summary: {
        watching: "12",
        warning: "2",
        download_restricted: "1",
        satisfied: "30",
        manually_cleared: "0",
        vip_exempted: "3",
      },
      worker: {
        last_started_at: "2026-08-16T08:10:00Z",
        last_completed_at: "2026-08-16T08:10:01Z",
        last_error_code: "",
        last_examined: "48",
        last_created: "1",
        last_transitioned: "2",
        run_count: "18",
      },
    })
    queryClient.setQueryData(
      ratioWatchAssessmentListQueryOptions("active").queryKey,
      { items: [], total: "0", limit: 30, offset: 0 }
    )
    queryClient.setQueryData(ratioWatchAppealListQueryOptions("all").queryKey, {
      items: [],
      total: "0",
      limit: 30,
      offset: 0,
    })
    queryClient.setQueryData(hnrAppealListQueryOptions("all").queryKey, {
      items: [],
      total: "0",
      limit: 30,
      offset: 0,
    })

    render(
      <MemoryRouter initialEntries={["/staff/settings/ratio-hnr"]}>
        <QueryClientProvider client={queryClient}>
          <StaffRatioHNRSettingsPage />
        </QueryClientProvider>
      </MemoryRouter>
    )

    expect(
      await screen.findByRole("heading", { name: "分享率与 H&R" })
    ).toBeVisible()
    expect(screen.getAllByText("执行中").length).toBeGreaterThan(0)
    expect(screen.getAllByText("3 天").length).toBeGreaterThan(0)
    expect(screen.getAllByText("1").length).toBeGreaterThan(0)
    expect(screen.getByText("长期分享率当前规则")).toBeVisible()
    expect(screen.getByText("分享率申诉")).toBeVisible()
    expect(screen.getByText("H&R 申诉")).toBeVisible()
    expect(screen.getAllByText("50 GB").length).toBeGreaterThan(0)
    expect(screen.getByRole("button", { name: "调整分享率" })).toBeVisible()
    expect(screen.getByRole("button", { name: "调整 H&R" })).toBeVisible()
  })
})

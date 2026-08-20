import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import { MemoryRouter } from "react-router"
import { describe, expect, it } from "vitest"

import { sessionKeys } from "~/features/auth/api/session.mutations"
import { capabilityKeys } from "~/features/authz/api/capabilities.queries"
import {
  downloadRestrictionKeys,
  type DownloadRestrictionStatus,
} from "~/features/identity/api/download-restriction.queries"
import { DownloadRestrictionPage } from "~/features/identity/pages/download-restriction-page"

const userId = "0198f20a-6da8-7e51-9c64-111111111111"

describe("DownloadRestrictionPage", () => {
  it("separates manual, ratio and H&R sources without exposing internal evidence", () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    queryClient.setQueryData(sessionKeys.current(), {
      user: {
        id: userId,
        username: "demo",
        display_name: "PeerGo 演示用户",
        email_verified: true,
      },
      expires_at: "2026-08-17T08:00:00Z",
      csrf_token: "c".repeat(43),
    })
    queryClient.setQueryData(capabilityKeys.current(userId), {
      policy_version: "2026-08-17",
      items: [
        {
          action: "user.downloadrestriction.read.self",
          description: "查看自己的下载限制",
          scope: { type: "site", id: "peergo" },
          expires_at: "2026-08-17T08:00:00Z",
        },
        {
          action: "user.downloadrestriction.appeal.create.self",
          description: "提交自己的下载限制申诉",
          scope: { type: "site", id: "peergo" },
          expires_at: "2026-08-17T08:00:00Z",
        },
      ],
    })
    queryClient.setQueryData(downloadRestrictionKeys.current(userId), {
      restricted: true,
      sources: {
        manual_or_legacy: true,
        ratio_watch: false,
        hit_and_run: true,
      },
      restriction: {
        source_kind: "manual_download_restriction",
        reason_code: "legacy_download_restriction",
        reason_summary: "该下载限制从旧站当前账户状态迁入，需要单独复核。",
        starts_at: "2026-08-14T10:00:00Z",
        expires_at: null,
        source_version: 1,
      },
      appeal: null,
      can_appeal: true,
    } satisfies DownloadRestrictionStatus)

    const { container } = render(
      <MemoryRouter>
        <QueryClientProvider client={queryClient}>
          <DownloadRestrictionPage />
        </QueryClientProvider>
      </MemoryRouter>
    )

    expect(screen.getByRole("heading", { name: "下载限制" })).toBeVisible()
    expect(screen.getByText("当前新下载受限")).toBeVisible()
    expect(screen.getByText("旧站 / 人工")).toBeVisible()
    expect(screen.getByText("长期分享率")).toBeVisible()
    expect(screen.getByText("H&R")).toBeVisible()
    expect(screen.getAllByText("正在限制")).toHaveLength(2)
    expect(screen.getByRole("button", { name: "提交申诉" })).toBeVisible()
    expect(
      screen
        .getAllByRole("link", { name: "查看详情" })
        .map((link) => link.getAttribute("href"))
    ).toEqual(["/account/ratio", "/account/hnr"])
    expect(container.textContent).not.toMatch(
      /tracker|passkey|obligation_id|assessment_id|authorization/i
    )
  })
})

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import { MemoryRouter } from "react-router"
import { describe, expect, it } from "vitest"

import { sessionKeys } from "~/features/auth/api/session.mutations"
import { capabilityKeys } from "~/features/authz/api/capabilities.queries"
import { emailSettingsQueryOptions } from "~/features/staff/api/operations.queries"
import { staffSessionKeys } from "~/features/staff/api/staff-session.mutations"
import { StaffEmailSettingsPage } from "~/features/staff/pages/staff-email-settings-page"

const administratorId = "0198f20a-6da8-7e51-9c64-111111111111"

describe("StaffEmailSettingsPage", () => {
  it("renders only non-secret Vault delivery configuration and aggregates", async () => {
    const queryClient = createQueryClient()
    render(
      <MemoryRouter initialEntries={["/staff/settings/email"]}>
        <QueryClientProvider client={queryClient}>
          <StaffEmailSettingsPage />
        </QueryClientProvider>
      </MemoryRouter>
    )

    expect(
      await screen.findByRole("heading", { name: "邮件设置" })
    ).toBeVisible()
    expect(screen.getByText("本地信箱")).toBeVisible()
    expect(screen.getAllByText("30 分钟")).toHaveLength(2)
    expect(screen.getByText("2 分钟")).toBeVisible()
    expect(screen.getAllByText("邮箱验证")).toHaveLength(2)
    expect(screen.getAllByText("密码找回")).toHaveLength(2)
    expect(screen.getByLabelText("测试收件地址")).toBeEnabled()
    expect(screen.queryByText(/service-token|messages\.jsonl/)).toBeNull()
  })
})

function createQueryClient() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  })
  queryClient.setQueryData(sessionKeys.current(), {
    user: {
      id: administratorId,
      username: "admin",
      display_name: "管理员",
      email_verified: true,
    },
    expires_at: "2026-08-16T00:00:00Z",
    csrf_token: "c".repeat(43),
  })
  queryClient.setQueryData(capabilityKeys.current(administratorId), {
    policy_version: "policy-2026-08-15",
    items: [],
  })
  queryClient.setQueryData(staffSessionKeys.current(), {
    user: {
      id: administratorId,
      username: "admin",
      display_name: "管理员",
      email_verified: true,
    },
    expires_at: "2026-08-16T00:00:00Z",
    webauthn_authenticated_at: "2026-08-15T08:00:00Z",
    csrf_token: "s".repeat(43),
  })
  queryClient.setQueryData(staffSessionKeys.capabilities(administratorId), {
    policy_version: "policy-2026-08-15",
    items: ["operations.monitor.read", "operations.email.test"].map(
      (action) => ({
        action,
        description: action,
        scope: { type: "site", id: "peergo" },
        expires_at: "2026-08-16T00:00:00Z",
      })
    ),
  })
  queryClient.setQueryData(emailSettingsQueryOptions().queryKey, {
    generated_at: "2026-08-16T08:00:00Z",
    delivery_mode: "development_outbox",
    verification_public_origin: "http://localhost:5174",
    password_recovery_public_origin: "http://localhost:5174",
    verification_ttl_seconds: 1800,
    password_recovery_ttl_seconds: 1800,
    cooldown_seconds: 120,
    templates: ["peergo-email-verification-v1", "peergo-password-recovery-v1"],
    stats: {
      verification_pending: "1",
      verification_sent: "18",
      verification_failed: "2",
      verification_verified: "12",
      recovery_pending: "0",
      recovery_sent: "5",
      recovery_failed: "1",
      recovery_completed: "4",
    },
  })
  return queryClient
}

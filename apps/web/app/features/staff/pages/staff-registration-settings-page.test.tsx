import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { createMemoryRouter, RouterProvider } from "react-router"
import { describe, expect, it } from "vitest"

import { sessionKeys } from "~/features/auth/api/session.mutations"
import { capabilityKeys } from "~/features/authz/api/capabilities.queries"
import {
  registrationPolicySettingsKeys,
  type RegistrationPolicySettings,
} from "~/features/staff/api/registration-policy-settings.queries"
import { staffSessionKeys } from "~/features/staff/api/staff-session.mutations"
import { StaffRegistrationSettingsPage } from "~/features/staff/pages/staff-registration-settings-page"

const userId = "0198f20a-6da8-7e51-9c64-555555555555"

describe("StaffRegistrationSettingsPage", () => {
  it("reviews one identity-owned registration mode change", async () => {
    const user = userEvent.setup()
    const queryClient = createQueryClient()
    const router = createMemoryRouter(
      [
        {
          path: "/staff/settings/registration",
          element: <StaffRegistrationSettingsPage />,
        },
      ],
      { initialEntries: ["/staff/settings/registration"] }
    )

    render(
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    )

    expect(
      await screen.findByRole("heading", { name: "注册设置" })
    ).toBeVisible()
    expect(screen.getByText("当前第 3 版")).toBeVisible()
    expect(screen.getByLabelText("保留用户名")).toHaveValue("admin\nroot")
    expect(screen.getByLabelText("普通登录有效期（小时）")).toHaveValue(168)
    const save = screen.getByRole("button", { name: "保存修改" })
    expect(save).toBeDisabled()

    await user.click(screen.getByRole("button", { name: "关闭注册" }))
    await user.type(screen.getByLabelText("变更理由"), "调整注册制")
    expect(save).toBeEnabled()

    await user.click(save)
    expect(
      await screen.findByRole("heading", {
        name: "确认注册设置变更",
      })
    ).toBeVisible()
    expect(screen.getByText("邀请注册 → 关闭注册")).toBeVisible()
  })
})

function createQueryClient() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  })
  queryClient.setQueryData(sessionKeys.current(), {
    user: {
      id: userId,
      username: "admin",
      display_name: "管理员",
      email_verified: true,
    },
    expires_at: "2026-08-16T00:00:00Z",
    csrf_token: "c".repeat(43),
  })
  queryClient.setQueryData(capabilityKeys.current(userId), {
    policy_version: "2026-08-15",
    items: [],
  })
  queryClient.setQueryData(staffSessionKeys.current(), {
    user: {
      id: userId,
      username: "admin",
      display_name: "管理员",
      email_verified: true,
    },
    expires_at: "2026-08-16T00:00:00Z",
    webauthn_authenticated_at: "2026-08-15T08:00:00Z",
    csrf_token: "s".repeat(43),
  })
  queryClient.setQueryData(staffSessionKeys.capabilities(userId), {
    policy_version: "2026-08-15",
    items: [
      {
        action: "site.registration.manage.read",
        description: "读取注册设置",
        scope: { type: "site", id: "peergo" },
        expires_at: "2026-08-16T00:00:00Z",
      },
      {
        action: "site.registration.update",
        description: "更新注册设置",
        scope: { type: "site", id: "peergo" },
        expires_at: "2026-08-16T00:00:00Z",
      },
    ],
  })
  const settings: RegistrationPolicySettings = {
    mode: "invite",
    member_invites_enabled: true,
    invite_valid_days: 7,
    max_invites_per_member: 5,
    minimum_invite_account_age_days: 30,
    minimum_invite_level: 2,
    username_min_characters: 3,
    username_max_characters: 20,
    reserved_usernames: ["admin", "root"],
    email_domain_mode: "any",
    email_domains: [],
    session_valid_hours: 168,
    remember_session_valid_hours: 720,
    human_verification_provider: "disabled",
    human_verification_site_key: "",
    human_verification_registration_enabled: false,
    human_verification_login_enabled: false,
    human_verification_password_recovery_enabled: false,
    human_verification_secret_configured: false,
    version: 3,
    updated_at: "2026-08-15T00:00:00Z",
  }
  queryClient.setQueryData(registrationPolicySettingsKeys.detail(), settings)
  return queryClient
}

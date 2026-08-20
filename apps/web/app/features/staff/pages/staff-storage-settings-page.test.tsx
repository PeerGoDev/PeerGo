import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import { MemoryRouter } from "react-router"
import { describe, expect, it } from "vitest"

import { sessionKeys } from "~/features/auth/api/session.mutations"
import { capabilityKeys } from "~/features/authz/api/capabilities.queries"
import { storageOperationsQueryOptions } from "~/features/staff/api/operations.queries"
import { staffSessionKeys } from "~/features/staff/api/staff-session.mutations"
import { StaffStorageSettingsPage } from "~/features/staff/pages/staff-storage-settings-page"

const administratorId = "0198f20a-6da8-7e51-9c64-111111111111"

describe("StaffStorageSettingsPage", () => {
  it("renders the effective backend, limits and migration health", async () => {
    const queryClient = createQueryClient()
    render(
      <MemoryRouter initialEntries={["/staff/settings/storage"]}>
        <QueryClientProvider client={queryClient}>
          <StaffStorageSettingsPage />
        </QueryClientProvider>
      </MemoryRouter>
    )

    expect(
      await screen.findByRole("heading", { name: "图片与存储" })
    ).toBeVisible()
    expect(screen.getByText("本地存储")).toBeVisible()
    expect(screen.getAllByText("local-primary")[0]).toBeVisible()
    expect(screen.getByText("4 MB")).toBeVisible()
    expect(screen.getByText("2 MB")).toBeVisible()
    expect(screen.getByText("1 MB")).toBeVisible()
    expect(screen.getByText("图片派生处理")).toBeVisible()
    expect(screen.getByText("统一存储迁移")).toBeVisible()
    expect(screen.getByText("源端保留期")).toBeVisible()
    expect(screen.getByText("原图与派生图相互独立")).toBeVisible()
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
    items: ["operations.monitor.read"].map((action) => ({
      action,
      description: action,
      scope: { type: "site", id: "peergo" },
      expires_at: "2026-08-16T00:00:00Z",
    })),
  })
  queryClient.setQueryData(storageOperationsQueryOptions().queryKey, {
    generated_at: "2026-08-16T08:00:00Z",
    runtime: {
      backend_id: "local-primary",
      driver: "filesystem",
      torrent_upload_max_bytes: "4194304",
      screenshot_max_bytes: "2097152",
      avatar_max_bytes: "1048576",
    },
    inventory: {
      torrent_objects: "1000",
      torrent_bytes: "4194304000",
      screenshot_objects: "200",
      screenshot_bytes: "104857600",
      avatar_objects: "20",
      avatar_bytes: "5242880",
      preferred_on_active_backend: "1000",
      verified_on_other_backends: "0",
      active_migrations: "0",
      failed_migration_items: "0",
    },
    image_derivatives: {
      policy_version: "webp-v1",
      pending: "0",
      processing: "0",
      retrying: "0",
      ready: "600",
      dead: "0",
      source_objects: "220",
      output_objects: "570",
      output_bytes: "52428800",
      last_error_code: "",
    },
    migrations: [
      {
        id: "28fb9097-8256-4517-9060-21821793678d",
        mode: "move",
        status: "retaining",
        object_kinds: [
          "torrent",
          "torrent_screenshot",
          "avatar",
          "image_derivative",
        ],
        source_backend_id: "local-primary",
        destination_backend_id: "minio-primary",
        total_items: "1220",
        pending_items: "0",
        verified_items: "1220",
        failed_items: "0",
        deleted_items: "0",
        last_error_code: "",
        created_at: "2026-08-15T06:00:00Z",
        cutover_at: "2026-08-15T07:00:00Z",
        retention_until: "2026-08-22T07:00:00Z",
      },
    ],
  })
  return queryClient
}

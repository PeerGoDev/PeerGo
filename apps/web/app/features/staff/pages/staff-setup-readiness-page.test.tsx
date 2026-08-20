import { QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import { MemoryRouter } from "react-router"
import { describe, expect, it } from "vitest"

import {
  emailSettingsQueryOptions,
  settlementSettingsQueryOptions,
  storageOperationsQueryOptions,
  torrentSettingsQueryOptions,
  workerOperationsQueryOptions,
} from "~/features/staff/api/operations.queries"
import { rssSettingsQueryOptions } from "~/features/staff/api/rss-settings.queries"
import { StaffSetupReadinessPage } from "~/features/staff/pages/staff-setup-readiness-page"
import { createStaffPageQueryClient } from "~/features/staff/test-utils/create-staff-page-query-client"
import { siteKeys } from "~/features/site/api/site.queries"

describe("StaffSetupReadinessPage", () => {
  it("summarizes real readable settings without claiming the CLI gate passed", async () => {
    const queryClient = createStaffPageQueryClient(["operations.monitor.read"])
    queryClient.setQueryData(siteKeys.info(), {
      name: "PeerGo",
      description: "测试站点",
      registration_mode: "closed",
      registration_username_min_characters: 3,
      registration_username_max_characters: 20,
      registration_email_domain_mode: "any",
      human_verification: disabledHumanVerification,
      online_users: 0,
      default_torrent_view: "list",
      show_latest_announcement: true,
    })
    queryClient.setQueryData(emailSettingsQueryOptions().queryKey, {
      generated_at: "2026-08-19T08:00:00Z",
      delivery_mode: "development_outbox",
      verification_public_origin: "http://localhost:5174",
      password_recovery_public_origin: "http://localhost:5174",
      verification_ttl_seconds: 1800,
      password_recovery_ttl_seconds: 1800,
      cooldown_seconds: 120,
      templates: [
        "peergo-email-verification-v1",
        "peergo-password-recovery-v1",
      ],
      stats: {
        verification_pending: "0",
        verification_sent: "0",
        verification_failed: "0",
        verification_verified: "0",
        recovery_pending: "0",
        recovery_sent: "0",
        recovery_failed: "0",
        recovery_completed: "0",
      },
    })
    queryClient.setQueryData(storageOperationsQueryOptions().queryKey, {
      generated_at: "2026-08-19T08:00:00Z",
      runtime: {
        backend_id: "local-primary",
        driver: "filesystem",
        torrent_upload_max_bytes: "4194304",
        screenshot_max_bytes: "2097152",
        avatar_max_bytes: "1048576",
      },
      inventory: {
        torrent_objects: "0",
        torrent_bytes: "0",
        screenshot_objects: "0",
        screenshot_bytes: "0",
        avatar_objects: "0",
        avatar_bytes: "0",
        preferred_on_active_backend: "0",
        verified_on_other_backends: "0",
        active_migrations: "0",
        failed_migration_items: "0",
      },
      image_derivatives: {
        policy_version: "webp-v1",
        pending: "0",
        processing: "0",
        retrying: "0",
        ready: "0",
        dead: "0",
        source_objects: "0",
        output_objects: "0",
        output_bytes: "0",
        last_error_code: "",
      },
      migrations: [],
    })
    queryClient.setQueryData(settlementSettingsQueryOptions().queryKey, {
      generated_at: "2026-08-19T08:00:00Z",
      hnr: {
        configured: false,
        revision_id: "",
        effective_at: null,
        rule_id: "",
        rule_version: 0,
        mode: "",
        required_seed_seconds: 0,
        required_ratio_basis_points: 0,
        assessment_window_seconds: 0,
        grace_period_seconds: 0,
        max_interval_credit_seconds: 0,
      },
      seedbox: {
        settlement_primitive_supported: true,
        global_policy_configured: false,
        upload_factor_basis_points: 10000,
        download_factor_basis_points: 10000,
        classification_connected: true,
        registry_connected: true,
        speed_observation_connected: true,
      },
      global_ratio_watch_connected: true,
    })
    queryClient.setQueryData(workerOperationsQueryOptions().queryKey, {
      generated_at: "2026-08-19T08:00:00Z",
      queues: [healthyWorkerQueue],
    })

    render(
      <MemoryRouter initialEntries={["/staff/setup"]}>
        <QueryClientProvider client={queryClient}>
          <StaffSetupReadinessPage />
        </QueryClientProvider>
      </MemoryRouter>
    )

    expect(await screen.findByText("首次上线准备")).toBeVisible()
    expect(screen.getByText("1 项阻塞")).toBeVisible()
    expect(screen.getByRole("heading", { name: "基础与身份" })).toBeVisible()
    expect(screen.getByRole("heading", { name: "内容与存储" })).toBeVisible()
    expect(
      screen.getByRole("heading", { name: "Tracker 与结算" })
    ).toBeVisible()
    expect(screen.getByText("1 类后台任务均无死信或重试积压。")).toBeVisible()
    expect(
      screen.getByText("当前是本地开发信箱，不能用于正式站点发信。")
    ).toBeVisible()
    expect(screen.getByText("尚未配置 H&R 规则。")).toBeVisible()
    expect(screen.getByText(/production-activation-check/)).toBeVisible()
  })

  it("blocks production email readiness when public action origins are not HTTPS", async () => {
    const queryClient = createStaffPageQueryClient(["operations.monitor.read"])
    queryClient.setQueryData(siteKeys.info(), {
      name: "PeerGo",
      description: "测试站点",
      registration_mode: "invite",
      registration_username_min_characters: 3,
      registration_username_max_characters: 20,
      registration_email_domain_mode: "any",
      human_verification: disabledHumanVerification,
      online_users: 0,
      default_torrent_view: "list",
      show_latest_announcement: true,
    })
    queryClient.setQueryData(emailSettingsQueryOptions().queryKey, {
      generated_at: "2026-08-19T08:00:00Z",
      delivery_mode: "https_relay",
      verification_public_origin: "http://peergo.example",
      password_recovery_public_origin: "https://peergo.example",
      verification_ttl_seconds: 1800,
      password_recovery_ttl_seconds: 1800,
      cooldown_seconds: 120,
      templates: [
        "peergo-email-verification-v1",
        "peergo-password-recovery-v1",
      ],
      stats: {
        verification_pending: "0",
        verification_sent: "0",
        verification_failed: "0",
        verification_verified: "0",
        recovery_pending: "0",
        recovery_sent: "0",
        recovery_failed: "0",
        recovery_completed: "0",
      },
    })
    queryClient.setQueryData(workerOperationsQueryOptions().queryKey, {
      generated_at: "2026-08-19T08:00:00Z",
      queues: [healthyWorkerQueue],
    })

    render(
      <MemoryRouter initialEntries={["/staff/setup"]}>
        <QueryClientProvider client={queryClient}>
          <StaffSetupReadinessPage />
        </QueryClientProvider>
      </MemoryRouter>
    )

    expect(
      await screen.findByText(
        "Relay 已接入，但邮箱验证或密码找回链接仍不是公开 HTTPS 来源。"
      )
    ).toBeVisible()
  })

  it("surfaces storage failures, worker dead letters and the effective upload limit", async () => {
    const queryClient = createStaffPageQueryClient([
      "operations.monitor.read",
      "torrent.manage.read",
      "rss.settings.manage.read",
    ])
    queryClient.setQueryData(siteKeys.info(), {
      name: "PeerGo",
      description: "测试站点",
      registration_mode: "invite",
      registration_username_min_characters: 3,
      registration_username_max_characters: 20,
      registration_email_domain_mode: "any",
      human_verification: disabledHumanVerification,
      online_users: 0,
      default_torrent_view: "list",
      show_latest_announcement: true,
    })
    queryClient.setQueryData(emailSettingsQueryOptions().queryKey, {
      generated_at: "2026-08-19T08:00:00Z",
      delivery_mode: "https_relay",
      verification_public_origin: "https://peergo.example",
      password_recovery_public_origin: "https://peergo.example",
      verification_ttl_seconds: 1800,
      password_recovery_ttl_seconds: 1800,
      cooldown_seconds: 120,
      templates: [
        "peergo-email-verification-v1",
        "peergo-password-recovery-v1",
      ],
      stats: {
        verification_pending: "0",
        verification_sent: "0",
        verification_failed: "0",
        verification_verified: "0",
        recovery_pending: "0",
        recovery_sent: "0",
        recovery_failed: "0",
        recovery_completed: "0",
      },
    })
    queryClient.setQueryData(settlementSettingsQueryOptions().queryKey, {
      generated_at: "2026-08-19T08:00:00Z",
      hnr: {
        configured: true,
        revision_id: "hnr-v1",
        effective_at: "2026-08-19T00:00:00Z",
        rule_id: "default",
        rule_version: 1,
        mode: "enforced",
        required_seed_seconds: 259200,
        required_ratio_basis_points: 10000,
        assessment_window_seconds: 604800,
        grace_period_seconds: 86400,
        max_interval_credit_seconds: 7200,
      },
      seedbox: {
        settlement_primitive_supported: true,
        global_policy_configured: true,
        upload_factor_basis_points: 10000,
        download_factor_basis_points: 10000,
        classification_connected: true,
        registry_connected: true,
        speed_observation_connected: true,
      },
      global_ratio_watch_connected: true,
    })
    queryClient.setQueryData(storageOperationsQueryOptions().queryKey, {
      generated_at: "2026-08-19T08:00:00Z",
      runtime: {
        backend_id: "local-primary",
        driver: "filesystem",
        torrent_upload_max_bytes: "4194304",
        screenshot_max_bytes: "3145728",
        avatar_max_bytes: "1048576",
      },
      inventory: {
        torrent_objects: "8722",
        torrent_bytes: "1",
        screenshot_objects: "1",
        screenshot_bytes: "1",
        avatar_objects: "0",
        avatar_bytes: "0",
        preferred_on_active_backend: "8723",
        verified_on_other_backends: "0",
        active_migrations: "0",
        failed_migration_items: "2",
      },
      image_derivatives: {
        policy_version: "webp-v1",
        pending: "0",
        processing: "0",
        retrying: "0",
        ready: "1",
        dead: "1",
        source_objects: "1",
        output_objects: "1",
        output_bytes: "1",
        last_error_code: "decode_failed",
      },
      migrations: [],
    })
    queryClient.setQueryData(workerOperationsQueryOptions().queryKey, {
      generated_at: "2026-08-19T08:00:00Z",
      queues: [{ ...healthyWorkerQueue, dead: "3" }],
    })
    queryClient.setQueryData(torrentSettingsQueryOptions().queryKey, {
      generated_at: "2026-08-19T08:00:00Z",
      active_upload_policy: {
        id: "00000000-0000-0000-0000-000000000013",
        sequence: 1,
        effective_at: "1970-01-01T00:00:00Z",
        created_at: "1970-01-01T00:00:00Z",
        reason: "测试策略",
        settings: {
          metainfo_max_bytes: 4194304,
          max_files: 100000,
          screenshot_max_count: 6,
          screenshot_max_bytes: 3145728,
          screenshot_max_pixels: 25000000,
          screenshot_formats: ["jpeg", "png", "webp"],
        },
      },
      scheduled_upload_policies: [],
      upload: {
        metainfo_max_bytes: "4194304",
        max_files: 100000,
        required_private: true,
        supported_protocol: "bittorrent-v1",
        duplicate_swarm_rejected: true,
        initial_state: "pending_review",
      },
      screenshots: {
        max_count: 6,
        max_bytes_per_file: "3145728",
        formats: ["jpeg", "png", "webp"],
        first_is_cover: true,
      },
      object: {
        original_stored_immutable: true,
        announce_rewritten_on_download: true,
        legacy_import_profile: "legacy_import",
        new_upload_profile: "strict_upload",
      },
      purchase: {
        enabled: true,
        currency_name: "魔力值",
        whole_units_only: true,
        tax_basis_points: 1000,
        policy_revision: "torrent-purchase-v1",
        policy_effective_from: "2026-08-19T00:00:00Z",
        priced_torrents: "0",
        legacy_entitlements: "0",
        live_entitlements: "0",
        permanent_access: true,
        atomic_settlement: true,
        refund_connected: true,
      },
    })
    queryClient.setQueryData(rssSettingsQueryOptions.queryKey, {
      enabled: true,
      cache_ttl_seconds: 30,
      max_items_per_feed: 100,
      max_subscriptions_per_user: 10,
      requests_per_minute: 20,
      version: 1,
      effective_at: "2026-08-19T00:00:00Z",
      updated_at: "2026-08-19T00:00:00Z",
    })

    render(
      <MemoryRouter initialEntries={["/staff/setup"]}>
        <QueryClientProvider client={queryClient}>
          <StaffSetupReadinessPage />
        </QueryClientProvider>
      </MemoryRouter>
    )

    expect(
      await screen.findByText(
        "存在 2 个迁移失败项、1 个图片派生死信，需先处理。"
      )
    ).toBeVisible()
    expect(
      screen.getByText("存在 3 个停止自动重试的任务，需人工核对后处理。")
    ).toBeVisible()
    expect(
      screen.getByText("单张原图当前允许 3 MB，高于建议的 2 MB。")
    ).toBeVisible()
    expect(
      screen.getByText(
        "RSS 已启用：每分钟 20 次、每份最多 100 项，缓存 30 秒。"
      )
    ).toBeVisible()
  })
})

const disabledHumanVerification = {
  provider: "disabled" as const,
  site_key: "",
  registration_enabled: false,
  login_enabled: false,
  password_recovery_enabled: false,
}

const healthyWorkerQueue = {
  id: "tracker_control" as const,
  label: "Tracker 控制",
  pending: "0",
  processing: "0",
  retrying: "0",
  dead: "0",
  completed: "12",
  oldest_pending_at: null,
  last_error_code: "",
  last_error_at: null,
}

import { QueryClient } from "@tanstack/react-query"

import { sessionKeys } from "~/features/auth/api/session.mutations"
import { capabilityKeys } from "~/features/authz/api/capabilities.queries"
import type { WorkerOperationsOverview } from "~/features/staff/api/operations.queries"
import { staffSessionKeys } from "~/features/staff/api/staff-session.mutations"

export const staffTestAdministratorId = "0198f20a-6da8-7e51-9c64-111111111111"

/**
 * Seeds the two deliberately separate member/staff sessions used by staff
 * pages. Page tests add only the permission and domain query data they need.
 */
export function createStaffPageQueryClient(actions: string[]) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  })
  const user = {
    id: staffTestAdministratorId,
    username: "admin",
    display_name: "管理员",
    email_verified: true,
  }
  queryClient.setQueryData(sessionKeys.current(), {
    user,
    expires_at: "2026-08-16T00:00:00Z",
    csrf_token: "c".repeat(43),
  })
  queryClient.setQueryData(capabilityKeys.current(staffTestAdministratorId), {
    policy_version: "policy-2026-08-15",
    items: [],
  })
  queryClient.setQueryData(staffSessionKeys.current(), {
    user,
    expires_at: "2026-08-16T00:00:00Z",
    webauthn_authenticated_at: "2026-08-15T08:00:00Z",
    csrf_token: "s".repeat(43),
  })
  queryClient.setQueryData(
    staffSessionKeys.capabilities(staffTestAdministratorId),
    {
      policy_version: "policy-2026-08-15",
      items: actions.map((action) => ({
        action,
        description: action,
        scope: { type: "site", id: "peergo" },
        expires_at: "2026-08-16T00:00:00Z",
      })),
    }
  )
  return queryClient
}

export function economySettingsFixture() {
  return {
    generated_at: "2026-08-16T08:00:00Z",
    activity: {
      ledger_supported: true,
      daily_attendance_connected: false,
      random_attendance_connected: false,
      streak_reward_connected: false,
      retroactive_connected: false,
      torrent_publish_connected: false,
      invite_reward_connected: false,
    },
    usage: {
      currency_name: "魔力值",
      whole_units_only: true,
      pt_coin_enabled: false,
      member_overdraft_allowed: false,
      append_only_ledger: true,
      torrent_purchase_supported: true,
      torrent_purchase_connected: false,
      member_gift_connected: false,
      content_tip_connected: false,
      refund_supported: true,
    },
    transactions: {
      legacy_opening: "12327",
      seeding_reward: "42",
      activity_reward: "12",
      torrent_purchase: "0",
      member_gift: "0",
      tip: "0",
      refund: "0",
      adjustment: "1",
    },
  }
}

export function workerOperationsFixture(): WorkerOperationsOverview {
  return {
    generated_at: "2026-08-16T08:00:00Z",
    queues: [
      workerQueue({
        id: "seeding_reward",
        label: "做种奖励结算",
        completed: "20",
      }),
      workerQueue({
        id: "promotion_delivery",
        label: "优惠政策投递",
        retrying: "2",
        last_error_code: "settlement_unavailable",
        last_error_at: "2026-08-16T07:59:00Z",
      }),
      workerQueue({
        id: "hnr_policy_delivery",
        label: "H&R 政策投递",
        pending: "1",
      }),
      workerQueue({
        id: "tracker_control",
        label: "Tracker 控制投影",
        pending: "3",
      }),
      workerQueue({
        id: "audit_delivery",
        label: "审计事件投递",
        processing: "1",
      }),
    ],
  }
}

function workerQueue(
  override: Partial<WorkerOperationsOverview["queues"][number]>
): WorkerOperationsOverview["queues"][number] {
  return {
    id: "seeding_reward",
    label: "任务",
    pending: "0",
    processing: "0",
    retrying: "0",
    dead: "0",
    completed: "0",
    oldest_pending_at: null,
    last_error_code: "",
    last_error_at: null,
    ...override,
  }
}

export function trackerOperationsFixture() {
  return {
    generated_at: "2026-08-16T08:00:00Z",
    control: {
      last_sequence: "8722",
      pending_events: "0",
      retrying_events: "0",
      enabled_torrents: "8722",
      disabled_torrents: "0",
      oldest_pending_at: null,
      updated_at: "2026-08-16T07:58:00Z",
    },
    swarm: {
      source_id: "tracker-primary",
      routing_epoch: "1",
      snapshot_sequence: "6",
      observed_at: "2026-08-16T07:58:00Z",
      applied_at: "2026-08-16T07:58:01Z",
      collecting_runs: "0",
      latest_run_progress: "",
    },
    evidence: {
      collecting_windows: "0",
      complete_windows: "24",
      latest_window_start: "2026-08-16T07:00:00Z",
      latest_window_end: "2026-08-16T08:00:00Z",
      latest_status: "complete" as const,
      latest_item_count: "1200",
      latest_chunks: 3,
      latest_received: 3,
      month_starts_at: "2026-08-01T00:00:00Z",
      expected_through: "2026-08-16T08:00:00Z",
      expected_windows: "368",
      missing_windows: "0",
      oldest_incomplete: null,
      health: "healthy" as const,
    },
    consumers: {
      traffic_entries: "2000",
      traffic_applied_at: "2026-08-16T07:59:00Z",
      hnr_events: "300",
      hnr_applied_at: "2026-08-16T07:59:00Z",
    },
  }
}

import { describe, expect, it } from "vitest"

import type { WorkerOperationsOverview } from "~/features/staff/api/operations.queries"
import {
  summarizeOperationIncidents,
  workerQueueHasIncident,
  workerQueueState,
} from "~/features/staff/model/operation-incidents"

const queues: WorkerOperationsOverview["queues"] = [
  queue({ id: "seeding_reward", label: "做种奖励结算", dead: "2" }),
  queue({
    id: "promotion_delivery",
    label: "优惠政策投递",
    retrying: "3",
    last_error_code: "settlement_unavailable",
  }),
  queue({ id: "tracker_control", label: "Tracker 控制投影", pending: "4" }),
  queue({ id: "audit_delivery", label: "审计事件投递", processing: "1" }),
]

describe("operation incident projection", () => {
  it("separates incidents from ordinary backlog and processing", () => {
    expect(queues.map(workerQueueState)).toEqual([
      "dead",
      "retrying",
      "backlogged",
      "processing",
    ])
    expect(queues.map(workerQueueHasIncident)).toEqual([
      true,
      true,
      false,
      false,
    ])
  })

  it("summarizes exception and audit delivery counts", () => {
    expect(summarizeOperationIncidents(queues)).toMatchObject({
      incidentQueueCount: 2,
      retrying: 3n,
      dead: 2n,
      auditOutstanding: 1n,
      audit: { id: "audit_delivery" },
    })
  })
})

function queue(
  override: Partial<WorkerOperationsOverview["queues"][number]>
): WorkerOperationsOverview["queues"][number] {
  return {
    id: "seeding_reward",
    label: "任务",
    pending: "0",
    processing: "0",
    retrying: "0",
    dead: "0",
    completed: "10",
    oldest_pending_at: null,
    last_error_code: "",
    last_error_at: null,
    ...override,
  }
}

import type { WorkerOperationsOverview } from "~/features/staff/api/operations.queries"

export type WorkerQueue = WorkerOperationsOverview["queues"][number]
export type WorkerQueueState =
  | "dead"
  | "retrying"
  | "backlogged"
  | "processing"
  | "healthy"

export function workerQueueState(queue: WorkerQueue): WorkerQueueState {
  if (BigInt(queue.dead) > 0n) return "dead"
  if (BigInt(queue.retrying) > 0n || queue.last_error_code) return "retrying"
  if (BigInt(queue.pending) > 0n) return "backlogged"
  if (BigInt(queue.processing) > 0n) return "processing"
  return "healthy"
}

export function workerQueueHasIncident(queue: WorkerQueue) {
  const state = workerQueueState(queue)
  return state === "dead" || state === "retrying"
}

export function summarizeOperationIncidents(
  queues: WorkerOperationsOverview["queues"]
) {
  const audit = queues.find((queue) => queue.id === "audit_delivery")
  return {
    incidentQueueCount: queues.filter(workerQueueHasIncident).length,
    retrying: queues.reduce(
      (total, queue) => total + BigInt(queue.retrying),
      0n
    ),
    dead: queues.reduce((total, queue) => total + BigInt(queue.dead), 0n),
    auditOutstanding: audit
      ? BigInt(audit.pending) +
        BigInt(audit.processing) +
        BigInt(audit.retrying)
      : 0n,
    audit,
  }
}

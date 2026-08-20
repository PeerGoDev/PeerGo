import {
  queryOptions,
  useMutation,
  useQueryClient,
} from "@tanstack/react-query"

import type { components } from "~/generated/api"
import { apiClient } from "~/shared/api/client"
import { ApiProblemError } from "~/shared/api/problem"

export type AdminWorkgroupOverview =
  components["schemas"]["AdminWorkgroupOverview"]
export type WorkgroupApplication = components["schemas"]["WorkgroupApplication"]
export type WorkgroupApplicationPage =
  components["schemas"]["WorkgroupApplicationPage"]
export type WorkgroupApplicationStatus =
  components["schemas"]["WorkgroupApplicationStatus"]
export type WorkgroupMembership = components["schemas"]["WorkgroupMembership"]
export type WorkgroupMembershipPage =
  components["schemas"]["WorkgroupMembershipPage"]
export type WorkgroupMembershipStatus =
  components["schemas"]["WorkgroupMembershipStatus"]
export type WorkgroupKind = components["schemas"]["WorkgroupKind"]
export type MembershipTransition =
  components["schemas"]["ChangeWorkgroupMembershipRequest"]["transition"]
export type WorkgroupContributionPolicyRevision =
  components["schemas"]["WorkgroupContributionPolicyRevision"]
export type WorkgroupContributionPolicyPage =
  components["schemas"]["WorkgroupContributionPolicyPage"]
export type WorkgroupContributionSummary =
  components["schemas"]["WorkgroupContributionSummary"]
export type WorkgroupContributionCycle =
  components["schemas"]["WorkgroupContributionCycle"]
export type WorkgroupContributionCyclePage =
  components["schemas"]["WorkgroupContributionCyclePage"]
export type WorkgroupContributionReminder =
  components["schemas"]["WorkgroupContributionReminder"]
export type WorkgroupTask = components["schemas"]["WorkgroupTask"]
export type WorkgroupTaskType = components["schemas"]["WorkgroupTaskType"]
export type WorkgroupTaskPage = components["schemas"]["WorkgroupTaskPage"]
export type WorkgroupTaskAssignment =
  components["schemas"]["WorkgroupTaskAssignment"]
export type WorkgroupTaskAssignmentPage =
  components["schemas"]["WorkgroupTaskAssignmentPage"]
export type WorkgroupTaskReviewDecision =
  components["schemas"]["WorkgroupTaskReviewDecision"]

export const workgroupAdministrationKeys = {
  all: ["staff", "workgroups"] as const,
  overview: () => [...workgroupAdministrationKeys.all, "overview"] as const,
  applications: (status: WorkgroupApplicationStatus | "") =>
    [...workgroupAdministrationKeys.all, "applications", status] as const,
  memberships: (kind: WorkgroupKind, status: WorkgroupMembershipStatus | "") =>
    [...workgroupAdministrationKeys.all, "memberships", kind, status] as const,
  contributionPolicies: (kind: WorkgroupKind) =>
    [
      ...workgroupAdministrationKeys.all,
      "contribution-policies",
      kind,
    ] as const,
  contributionCycles: (kind: WorkgroupKind, membershipId: string) =>
    [
      ...workgroupAdministrationKeys.all,
      "contribution-cycles",
      kind,
      membershipId,
    ] as const,
  tasks: (kind: WorkgroupKind) =>
    [...workgroupAdministrationKeys.all, "tasks", kind] as const,
  taskAssignments: (kind: WorkgroupKind, taskId: string) =>
    [
      ...workgroupAdministrationKeys.all,
      "tasks",
      kind,
      taskId,
      "assignments",
    ] as const,
}

export function adminWorkgroupTasksQueryOptions(kind: WorkgroupKind) {
  return queryOptions({
    queryKey: workgroupAdministrationKeys.tasks(kind),
    queryFn: async (): Promise<WorkgroupTaskPage> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/admin/workgroups/{group_kind}/tasks",
        {
          params: {
            path: { group_kind: kind },
            query: { limit: 50, offset: 0 },
          },
        }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    staleTime: 10_000,
    retry: false,
  })
}

export function adminWorkgroupTaskAssignmentsQueryOptions(
  kind: WorkgroupKind,
  taskId: string
) {
  return queryOptions({
    queryKey: workgroupAdministrationKeys.taskAssignments(kind, taskId),
    queryFn: async (): Promise<WorkgroupTaskAssignmentPage> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/admin/workgroups/{group_kind}/tasks/{task_id}/assignments",
        {
          params: {
            path: { group_kind: kind, task_id: taskId },
            query: { limit: 100, offset: 0 },
          },
        }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    staleTime: 10_000,
    retry: false,
  })
}

export function adminWorkgroupContributionCyclesQueryOptions(
  kind: WorkgroupKind,
  membershipId: string
) {
  return queryOptions({
    queryKey: workgroupAdministrationKeys.contributionCycles(
      kind,
      membershipId
    ),
    queryFn: async (): Promise<WorkgroupContributionCyclePage> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/admin/workgroups/{group_kind}/memberships/{membership_id}/contributions",
        {
          params: {
            path: {
              group_kind: kind,
              membership_id: membershipId,
            },
            query: { limit: 12 },
          },
        }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    staleTime: 10_000,
    retry: false,
  })
}

export function adminWorkgroupContributionPoliciesQueryOptions(
  kind: WorkgroupKind
) {
  return queryOptions({
    queryKey: workgroupAdministrationKeys.contributionPolicies(kind),
    queryFn: async (): Promise<WorkgroupContributionPolicyPage> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/admin/workgroups/{group_kind}/contribution-policies",
        {
          params: {
            path: { group_kind: kind },
            query: { limit: 100, offset: 0 },
          },
        }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    staleTime: 10_000,
    retry: false,
  })
}

export const adminWorkgroupOverviewQueryOptions = queryOptions({
  queryKey: workgroupAdministrationKeys.overview(),
  queryFn: async (): Promise<AdminWorkgroupOverview> => {
    const { data, error, response } = await apiClient.GET(
      "/api/v1/admin/workgroups"
    )
    if (!response.ok || !data) {
      throw new ApiProblemError(response.status, error)
    }
    return data
  },
  staleTime: 10_000,
  retry: false,
})

export function adminWorkgroupApplicationsQueryOptions(
  status: WorkgroupApplicationStatus | ""
) {
  return queryOptions({
    queryKey: workgroupAdministrationKeys.applications(status),
    queryFn: async (): Promise<WorkgroupApplicationPage> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/admin/workgroups/applications",
        {
          params: {
            query: { status: status || undefined, limit: 100, offset: 0 },
          },
        }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    staleTime: 10_000,
    retry: false,
  })
}

export function adminWorkgroupMembershipsQueryOptions(
  kind: WorkgroupKind,
  status: WorkgroupMembershipStatus | ""
) {
  return queryOptions({
    queryKey: workgroupAdministrationKeys.memberships(kind, status),
    queryFn: async (): Promise<WorkgroupMembershipPage> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/admin/workgroups/{group_kind}/memberships",
        {
          params: {
            path: { group_kind: kind },
            query: { status: status || undefined, limit: 100, offset: 0 },
          },
        }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    staleTime: 10_000,
    retry: false,
  })
}

export function useDecideWorkgroupApplication() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      csrfToken: string
      idempotencyKey: string
      application: WorkgroupApplication
      decision: "approve" | "reject"
      reason: string
    }): Promise<WorkgroupApplication> => {
      const { data, error, response } = await apiClient.POST(
        "/api/v1/admin/workgroups/applications/{application_id}/decision",
        {
          params: {
            path: { application_id: input.application.id },
            header: {
              "X-CSRF-Token": input.csrfToken,
              "Idempotency-Key": input.idempotencyKey,
            },
          },
          body: {
            expected_version: input.application.version,
            decision: input.decision,
            reason: input.reason,
          },
        }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    onSuccess: invalidateWorkgroups(queryClient),
    onError: invalidateWorkgroups(queryClient),
  })
}

export function useGrantWorkgroupMembership() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      csrfToken: string
      idempotencyKey: string
      groupKind: WorkgroupKind
      userNumericId: number
      reason: string
    }): Promise<WorkgroupMembership> => {
      const { data, error, response } = await apiClient.POST(
        "/api/v1/admin/workgroups/{group_kind}/memberships",
        {
          params: {
            path: { group_kind: input.groupKind },
            header: {
              "X-CSRF-Token": input.csrfToken,
              "Idempotency-Key": input.idempotencyKey,
            },
          },
          body: {
            user_numeric_id: input.userNumericId,
            reason: input.reason,
          },
        }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    onSuccess: invalidateWorkgroups(queryClient),
    onError: invalidateWorkgroups(queryClient),
  })
}

export function useChangeWorkgroupMembership() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      csrfToken: string
      idempotencyKey: string
      membership: WorkgroupMembership
      transition: MembershipTransition
      reason: string
    }): Promise<WorkgroupMembership> => {
      const { data, error, response } = await apiClient.POST(
        "/api/v1/admin/workgroups/{group_kind}/memberships/{membership_id}/transition",
        {
          params: {
            path: {
              group_kind: input.membership.group_kind,
              membership_id: input.membership.id,
            },
            header: {
              "X-CSRF-Token": input.csrfToken,
              "Idempotency-Key": input.idempotencyKey,
            },
          },
          body: {
            expected_version: input.membership.version,
            transition: input.transition,
            reason: input.reason,
          },
        }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    onSuccess: invalidateWorkgroups(queryClient),
    onError: invalidateWorkgroups(queryClient),
  })
}

export function useIssueWorkgroupContributionPolicy() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      csrfToken: string
      idempotencyKey: string
      groupKind: WorkgroupKind
      targetValue: number
      effectiveFrom: string
      reason: string
    }): Promise<WorkgroupContributionPolicyRevision> => {
      const { data, error, response } = await apiClient.POST(
        "/api/v1/admin/workgroups/{group_kind}/contribution-policies",
        {
          params: {
            path: { group_kind: input.groupKind },
            header: {
              "X-CSRF-Token": input.csrfToken,
              "Idempotency-Key": input.idempotencyKey,
            },
          },
          body: {
            target_value: input.targetValue,
            effective_from: input.effectiveFrom,
            reason: input.reason,
          },
        }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    onSuccess: invalidateWorkgroups(queryClient),
    onError: invalidateWorkgroups(queryClient),
  })
}

export function useIssueWorkgroupContributionReminder() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      csrfToken: string
      idempotencyKey: string
      groupKind: WorkgroupKind
      membershipId: string
      periodStartsAt: string
      reason: string
    }): Promise<WorkgroupContributionReminder> => {
      const { data, error, response } = await apiClient.POST(
        "/api/v1/admin/workgroups/{group_kind}/memberships/{membership_id}/contributions",
        {
          params: {
            path: {
              group_kind: input.groupKind,
              membership_id: input.membershipId,
            },
            header: {
              "X-CSRF-Token": input.csrfToken,
              "Idempotency-Key": input.idempotencyKey,
            },
          },
          body: {
            period_starts_at: input.periodStartsAt,
            reason: input.reason,
          },
        }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    onSuccess: invalidateWorkgroups(queryClient),
    onError: invalidateWorkgroups(queryClient),
  })
}

export function usePublishWorkgroupTask() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      csrfToken: string
      idempotencyKey: string
      groupKind: WorkgroupKind
      taskType: WorkgroupTaskType
      title: string
      description: string
      startsAt: string
      dueAt: string
    }): Promise<WorkgroupTask> => {
      const { data, error, response } = await apiClient.POST(
        "/api/v1/admin/workgroups/{group_kind}/tasks",
        {
          params: {
            path: { group_kind: input.groupKind },
            header: {
              "X-CSRF-Token": input.csrfToken,
              "Idempotency-Key": input.idempotencyKey,
            },
          },
          body: {
            task_type: input.taskType,
            title: input.title,
            description: input.description,
            starts_at: input.startsAt,
            due_at: input.dueAt,
          },
        }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    onSuccess: invalidateWorkgroups(queryClient),
    onError: invalidateWorkgroups(queryClient),
  })
}

export function useReviewWorkgroupTaskSubmission() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      csrfToken: string
      idempotencyKey: string
      submissionId: string
      decision: WorkgroupTaskReviewDecision
      reason: string
    }): Promise<WorkgroupTaskAssignment> => {
      const { data, error, response } = await apiClient.POST(
        "/api/v1/admin/workgroups/task-submissions/{submission_id}/decision",
        {
          params: {
            path: { submission_id: input.submissionId },
            header: {
              "X-CSRF-Token": input.csrfToken,
              "Idempotency-Key": input.idempotencyKey,
            },
          },
          body: { decision: input.decision, reason: input.reason },
        }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    onSuccess: invalidateWorkgroups(queryClient),
    onError: invalidateWorkgroups(queryClient),
  })
}

function invalidateWorkgroups(queryClient: ReturnType<typeof useQueryClient>) {
  return async () => {
    await queryClient.invalidateQueries({
      queryKey: workgroupAdministrationKeys.all,
    })
  }
}

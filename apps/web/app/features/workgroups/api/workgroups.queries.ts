import {
  queryOptions,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query"

import type { components } from "~/generated/api"
import { apiClient } from "~/shared/api/client"
import { ApiProblemError } from "~/shared/api/problem"

export type WorkgroupKind = components["schemas"]["WorkgroupKind"]
export type MyWorkgroupOverview = components["schemas"]["MyWorkgroupOverview"]
export type WorkgroupApplication = components["schemas"]["WorkgroupApplication"]
export type WorkgroupContributionCycle =
  components["schemas"]["WorkgroupContributionCycle"]
export type WorkgroupContributionCyclePage =
  components["schemas"]["WorkgroupContributionCyclePage"]
export type WorkgroupTaskAssignment =
  components["schemas"]["WorkgroupTaskAssignment"]
export type WorkgroupTaskAssignmentPage =
  components["schemas"]["WorkgroupTaskAssignmentPage"]

export const workgroupKeys = {
  all: ["workgroups"] as const,
  mine: (userId: string) => [...workgroupKeys.all, "mine", userId] as const,
  contributionCycles: (userId: string, kind: WorkgroupKind) =>
    [...workgroupKeys.mine(userId), "contribution-cycles", kind] as const,
  tasks: (userId: string) => [...workgroupKeys.mine(userId), "tasks"] as const,
}

export function myWorkgroupTasksQueryOptions(userId: string) {
  return queryOptions({
    queryKey: workgroupKeys.tasks(userId),
    queryFn: async (): Promise<WorkgroupTaskAssignmentPage> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/me/workgroups/tasks",
        { params: { query: { limit: 50, offset: 0 } } }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    staleTime: 15_000,
    retry: false,
  })
}

export function myWorkgroupContributionCyclesQueryOptions(
  userId: string,
  kind: WorkgroupKind
) {
  return queryOptions({
    queryKey: workgroupKeys.contributionCycles(userId, kind),
    queryFn: async (): Promise<WorkgroupContributionCyclePage> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/me/workgroups/{group_kind}/contributions",
        {
          params: {
            path: { group_kind: kind },
            query: { limit: 6 },
          },
        }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    staleTime: 15_000,
    retry: false,
  })
}

export function myWorkgroupsQueryOptions(userId: string) {
  return queryOptions({
    queryKey: workgroupKeys.mine(userId),
    queryFn: async (): Promise<MyWorkgroupOverview> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/me/workgroups"
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    staleTime: 15_000,
    retry: false,
  })
}

export function useMyWorkgroups(userId?: string) {
  return useQuery({
    ...myWorkgroupsQueryOptions(userId ?? "anonymous"),
    enabled: Boolean(userId),
  })
}

export function useCreateWorkgroupApplication(userId?: string) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      csrfToken: string
      idempotencyKey: string
      groupKind: WorkgroupKind
      statement: string
    }): Promise<WorkgroupApplication> => {
      const { data, error, response } = await apiClient.POST(
        "/api/v1/me/workgroups/{group_kind}/applications",
        {
          params: {
            path: { group_kind: input.groupKind },
            header: {
              "X-CSRF-Token": input.csrfToken,
              "Idempotency-Key": input.idempotencyKey,
            },
          },
          body: { statement: input.statement },
        }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: workgroupKeys.all })
    },
  })
}

export function useSubmitWorkgroupTask(userId?: string) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      csrfToken: string
      idempotencyKey: string
      assignmentId: string
      statement: string
    }): Promise<WorkgroupTaskAssignment> => {
      const { data, error, response } = await apiClient.POST(
        "/api/v1/me/workgroups/tasks/{assignment_id}/submissions",
        {
          params: {
            path: { assignment_id: input.assignmentId },
            header: {
              "X-CSRF-Token": input.csrfToken,
              "Idempotency-Key": input.idempotencyKey,
            },
          },
          body: { statement: input.statement },
        }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({
        queryKey: userId ? workgroupKeys.tasks(userId) : workgroupKeys.all,
      })
    },
    onError: async () => {
      await queryClient.invalidateQueries({
        queryKey: userId ? workgroupKeys.tasks(userId) : workgroupKeys.all,
      })
    },
  })
}

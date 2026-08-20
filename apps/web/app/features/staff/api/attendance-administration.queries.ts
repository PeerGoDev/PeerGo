import {
  queryOptions,
  useMutation,
  useQueryClient,
} from "@tanstack/react-query"

import type { components } from "~/generated/api"
import { apiClient } from "~/shared/api/client"
import { ApiProblemError } from "~/shared/api/problem"

export type AttendancePolicy = components["schemas"]["AttendancePolicy"]
export type AttendancePolicyPage = components["schemas"]["AttendancePolicyPage"]
export type AttendancePolicySettings =
  components["schemas"]["AttendancePolicySettings"]

export const attendanceAdministrationKeys = {
  all: ["staff", "attendance-policies"] as const,
  list: () => [...attendanceAdministrationKeys.all, "list"] as const,
}

export function attendancePolicyListQueryOptions() {
  return queryOptions({
    queryKey: attendanceAdministrationKeys.list(),
    queryFn: async (): Promise<AttendancePolicyPage> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/admin/settings/attendance",
        { params: { query: { limit: 30, offset: 0 } } }
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

export function useIssueAttendancePolicy() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      csrfToken: string
      settings: AttendancePolicySettings
      reason: string
      idempotencyKey: string
    }): Promise<AttendancePolicy> => {
      const { data, error, response } = await apiClient.POST(
        "/api/v1/admin/settings/attendance",
        {
          params: {
            header: {
              "X-CSRF-Token": input.csrfToken,
              "Idempotency-Key": input.idempotencyKey,
            },
          },
          body: { settings: input.settings, reason: input.reason },
        }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({
        queryKey: attendanceAdministrationKeys.all,
      })
    },
  })
}

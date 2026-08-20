import {
  queryOptions,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query"

import { economyKeys } from "~/features/economy/api/economy.queries"
import type { components } from "~/generated/api"
import { apiClient } from "~/shared/api/client"
import { ApiProblemError } from "~/shared/api/problem"

export type AttendanceOverview = components["schemas"]["AttendanceOverview"]
export type AttendanceRecord = components["schemas"]["AttendanceRecord"]
export type AttendanceMode = components["schemas"]["AttendanceMode"]

export const attendanceKeys = {
  all: ["attendance"] as const,
  current: (userId: string) =>
    [...attendanceKeys.all, "current", userId] as const,
}

export function attendanceOverviewQueryOptions(userId: string) {
  return queryOptions({
    queryKey: attendanceKeys.current(userId),
    queryFn: async (): Promise<AttendanceOverview> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/me/attendance"
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

export function useAttendanceOverview(
  userId: string | undefined,
  enabled = true
) {
  return useQuery({
    ...attendanceOverviewQueryOptions(userId ?? "anonymous"),
    enabled: enabled && Boolean(userId),
  })
}

export function useClaimAttendance(userId: string | undefined) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      csrfToken: string
      mode: AttendanceMode
      idempotencyKey: string
    }): Promise<AttendanceRecord> => {
      const { data, error, response } = await apiClient.POST(
        "/api/v1/me/attendance",
        {
          params: {
            header: {
              "X-CSRF-Token": input.csrfToken,
              "Idempotency-Key": input.idempotencyKey,
            },
          },
          body: { mode: input.mode },
        }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: attendanceKeys.all }),
        queryClient.invalidateQueries({ queryKey: economyKeys.all }),
      ])
    },
  })
}

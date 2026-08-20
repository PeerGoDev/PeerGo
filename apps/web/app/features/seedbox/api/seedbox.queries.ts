import {
  queryOptions,
  useMutation,
  useQueryClient,
} from "@tanstack/react-query"

import type { components } from "~/generated/api"
import { apiClient } from "~/shared/api/client"
import { ApiProblemError } from "~/shared/api/problem"

export type SeedboxReport = components["schemas"]["SeedboxReport"]
export type SeedboxReportPage = components["schemas"]["SeedboxReportPage"]
export type SeedboxReportStatus = components["schemas"]["SeedboxReportStatus"]
export type CreateSeedboxReportRequest =
  components["schemas"]["CreateSeedboxReportRequest"]
export type DecideSeedboxReportRequest =
  components["schemas"]["DecideSeedboxReportRequest"]

export const seedboxKeys = {
  all: ["seedbox"] as const,
  mine: () => [...seedboxKeys.all, "mine"] as const,
  admin: (status: SeedboxReportStatus | "") =>
    [...seedboxKeys.all, "admin", status] as const,
}

export function mySeedboxReportsQueryOptions() {
  return queryOptions({
    queryKey: seedboxKeys.mine(),
    queryFn: async (): Promise<SeedboxReportPage> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/me/seedboxes",
        { params: { query: { limit: 50, offset: 0 } } }
      )
      if (!response.ok || !data)
        throw new ApiProblemError(response.status, error)
      return data
    },
    staleTime: 10_000,
    retry: false,
  })
}

export function adminSeedboxReportsQueryOptions(
  status: SeedboxReportStatus | "" = ""
) {
  return queryOptions({
    queryKey: seedboxKeys.admin(status),
    queryFn: async (): Promise<SeedboxReportPage> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/admin/seedboxes",
        {
          params: {
            query: { status: status || undefined, limit: 50, offset: 0 },
          },
        }
      )
      if (!response.ok || !data)
        throw new ApiProblemError(response.status, error)
      return data
    },
    staleTime: 10_000,
    retry: false,
  })
}

export function useCreateSeedboxReport() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      csrfToken: string
      idempotencyKey: string
      body: CreateSeedboxReportRequest
    }): Promise<SeedboxReport> => {
      const { data, error, response } = await apiClient.POST(
        "/api/v1/me/seedboxes",
        {
          params: {
            header: {
              "X-CSRF-Token": input.csrfToken,
              "Idempotency-Key": input.idempotencyKey,
            },
          },
          body: input.body,
        }
      )
      if (!response.ok || !data)
        throw new ApiProblemError(response.status, error)
      return data
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: seedboxKeys.mine() })
    },
  })
}

export function useDecideSeedboxReport() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      csrfToken: string
      idempotencyKey: string
      reportId: string
      body: DecideSeedboxReportRequest
    }): Promise<SeedboxReport> => {
      const { data, error, response } = await apiClient.POST(
        "/api/v1/admin/seedboxes/{report_id}/decision",
        {
          params: {
            path: { report_id: input.reportId },
            header: {
              "X-CSRF-Token": input.csrfToken,
              "Idempotency-Key": input.idempotencyKey,
            },
          },
          body: input.body,
        }
      )
      if (!response.ok || !data)
        throw new ApiProblemError(response.status, error)
      return data
    },
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: seedboxKeys.all }),
        // The approval transaction also appends a runtime-policy revision.
        // Refresh the policy summary so staff immediately see the new trusted
        // network instead of waiting for its regular stale-time window.
        queryClient.invalidateQueries({
          queryKey: ["staff", "operations", "tracker-settings"],
        }),
      ])
    },
  })
}

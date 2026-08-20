import {
  queryOptions,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query"

import type { components } from "~/generated/api"
import { apiClient } from "~/shared/api/client"
import { ApiProblemError } from "~/shared/api/problem"

export type DownloadRestrictionStatus =
  components["schemas"]["DownloadRestrictionStatus"]
export type DownloadRestrictionAppeal =
  components["schemas"]["AccountAccessAppeal"]

export const downloadRestrictionKeys = {
  all: ["identity", "download-restriction"] as const,
  current: (userId: string) =>
    [...downloadRestrictionKeys.all, "current-user", userId] as const,
}

export function downloadRestrictionQueryOptions(userId: string) {
  return queryOptions({
    queryKey: downloadRestrictionKeys.current(userId),
    queryFn: async (): Promise<DownloadRestrictionStatus> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/me/download-restriction"
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

export function useDownloadRestriction(
  userId: string | undefined,
  enabled: boolean
) {
  return useQuery({
    ...downloadRestrictionQueryOptions(userId ?? "anonymous"),
    enabled: Boolean(userId && enabled),
  })
}

export function useSubmitDownloadRestrictionAppeal(userId: string) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      csrfToken: string
      idempotencyKey: string
      statement: string
    }): Promise<DownloadRestrictionAppeal> => {
      const { data, error, response } = await apiClient.POST(
        "/api/v1/me/download-restriction/appeals",
        {
          params: {
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
        queryKey: downloadRestrictionKeys.current(userId),
      })
    },
  })
}

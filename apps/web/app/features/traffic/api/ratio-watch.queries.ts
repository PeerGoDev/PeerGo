import {
  queryOptions,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query"

import type { components } from "~/generated/api"
import { apiClient } from "~/shared/api/client"
import { ApiProblemError } from "~/shared/api/problem"

export type MyRatioWatch = components["schemas"]["MyRatioWatch"]
export type MyRatioWatchAppeal = components["schemas"]["MyRatioWatchAppeal"]

export const ratioWatchKeys = {
  all: ["ratio-watch"] as const,
  current: (userId: string) =>
    [...ratioWatchKeys.all, "current-user", userId] as const,
}

export function myRatioWatchQueryOptions(userId: string) {
  return queryOptions({
    queryKey: ratioWatchKeys.current(userId),
    queryFn: async (): Promise<MyRatioWatch> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/me/ratio-watch"
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

export function useMyRatioWatch(userId: string | undefined, enabled: boolean) {
  return useQuery({
    ...myRatioWatchQueryOptions(userId ?? "anonymous"),
    enabled: Boolean(userId && enabled),
  })
}

export function useSubmitMyRatioWatchAppeal(userId: string) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      csrfToken: string
      idempotencyKey: string
      statement: string
    }): Promise<MyRatioWatchAppeal> => {
      const { data, error, response } = await apiClient.POST(
        "/api/v1/me/ratio-watch/appeals",
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
        queryKey: ratioWatchKeys.current(userId),
      })
    },
  })
}

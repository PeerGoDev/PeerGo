import {
  queryOptions,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query"

import type { components, operations } from "~/generated/api"
import { apiClient } from "~/shared/api/client"
import { ApiProblemError } from "~/shared/api/problem"

export type HitAndRunPageData = components["schemas"]["HNRPage"]
export type MyHNRAppeal = components["schemas"]["MyHNRAppeal"]
type HitAndRunQuery = NonNullable<
  operations["listMyHitAndRuns"]["parameters"]["query"]
>
export type HitAndRunFilter = NonNullable<HitAndRunQuery["status"]>

export const hitAndRunKeys = {
  all: ["hit-and-runs"] as const,
  page: (userId: string, status: HitAndRunFilter, cursor: string | undefined) =>
    [
      ...hitAndRunKeys.all,
      "current-user",
      userId,
      status,
      cursor ?? "first",
    ] as const,
}

export function hitAndRunPageQueryOptions(
  userId: string,
  status: HitAndRunFilter,
  cursor: string | undefined
) {
  return queryOptions({
    queryKey: hitAndRunKeys.page(userId, status, cursor),
    queryFn: async (): Promise<HitAndRunPageData> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/me/hit-and-runs",
        { params: { query: { status, limit: 20, cursor } } }
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

export function useHitAndRunPage(
  userId: string | undefined,
  status: HitAndRunFilter,
  cursor: string | undefined,
  enabled: boolean
) {
  return useQuery({
    ...hitAndRunPageQueryOptions(userId ?? "anonymous", status, cursor),
    enabled: Boolean(userId && enabled),
  })
}

export function useSubmitMyHNRAppeal(userId: string) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      obligationId: string
      csrfToken: string
      idempotencyKey: string
      statement: string
    }): Promise<MyHNRAppeal> => {
      const { data, error, response } = await apiClient.POST(
        "/api/v1/me/hit-and-runs/{obligation_id}/appeals",
        {
          params: {
            path: { obligation_id: input.obligationId },
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
      await queryClient.invalidateQueries({ queryKey: hitAndRunKeys.all })
    },
  })
}

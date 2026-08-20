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

export type MemberMedal = components["schemas"]["MemberMedal"]
export type MemberMedalHolding = components["schemas"]["MemberMedalHolding"]
export type MemberMedalOverview = components["schemas"]["MemberMedalOverview"]

export const medalKeys = {
  all: ["member-medals"] as const,
  current: (userId: string) => [...medalKeys.all, userId] as const,
}

export function memberMedalOverviewQueryOptions(userId: string) {
  return queryOptions({
    queryKey: medalKeys.current(userId),
    queryFn: async (): Promise<MemberMedalOverview> => {
      const { data, error, response } = await apiClient.GET("/api/v1/me/medals")
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    staleTime: 10_000,
    retry: false,
  })
}

export function useMemberMedals(userId: string | undefined) {
  return useQuery({
    ...memberMedalOverviewQueryOptions(userId ?? "anonymous"),
    enabled: Boolean(userId),
  })
}

function useInvalidateMemberMedals() {
  const queryClient = useQueryClient()
  return async () => {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: medalKeys.all }),
      queryClient.invalidateQueries({ queryKey: economyKeys.all }),
    ])
  }
}

export function usePurchaseMedal() {
  const invalidate = useInvalidateMemberMedals()
  return useMutation({
    mutationFn: async (input: {
      medalId: number
      csrfToken: string
      idempotencyKey: string
    }) => {
      const { data, error, response } = await apiClient.POST(
        "/api/v1/me/medals/{medal_id}/purchase",
        {
          params: {
            path: { medal_id: input.medalId },
            header: {
              "X-CSRF-Token": input.csrfToken,
              "Idempotency-Key": input.idempotencyKey,
            },
          },
        }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    onSuccess: invalidate,
  })
}

export function useUpdateMedalWearing() {
  const invalidate = useInvalidateMemberMedals()
  return useMutation({
    mutationFn: async (input: {
      medalId: number
      expectedVersion: number
      wearing: boolean
      csrfToken: string
    }): Promise<MemberMedalHolding> => {
      const { data, error, response } = await apiClient.PUT(
        "/api/v1/me/medals/{medal_id}/wearing",
        {
          params: {
            path: { medal_id: input.medalId },
            header: { "X-CSRF-Token": input.csrfToken },
          },
          body: {
            expected_version: input.expectedVersion,
            wearing: input.wearing,
          },
        }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    onSuccess: invalidate,
  })
}

export function useUpdateMedalPriority() {
  const invalidate = useInvalidateMemberMedals()
  return useMutation({
    mutationFn: async (input: {
      medalId: number
      expectedVersion: number
      direction: "up" | "down"
      csrfToken: string
    }): Promise<MemberMedalHolding> => {
      const { data, error, response } = await apiClient.PUT(
        "/api/v1/me/medals/{medal_id}/priority",
        {
          params: {
            path: { medal_id: input.medalId },
            header: { "X-CSRF-Token": input.csrfToken },
          },
          body: {
            expected_version: input.expectedVersion,
            direction: input.direction,
          },
        }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    onSuccess: invalidate,
  })
}

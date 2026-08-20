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

export type MemberGiftOverview = components["schemas"]["MemberGiftOverview"]
export type MemberGiftRecord = components["schemas"]["MemberGiftRecord"]

export const memberGiftKeys = {
  all: ["member-gifts"] as const,
  current: (userId: string) => [...memberGiftKeys.all, userId] as const,
}

export function memberGiftOverviewQueryOptions(userId: string) {
  return queryOptions({
    queryKey: memberGiftKeys.current(userId),
    queryFn: async (): Promise<MemberGiftOverview> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/me/member-gifts",
        { params: { query: { limit: 30 } } }
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

export function useMemberGiftOverview(userId: string | undefined) {
  return useQuery({
    ...memberGiftOverviewQueryOptions(userId ?? "anonymous"),
    enabled: Boolean(userId),
  })
}

export function useCreateMemberGift(userId: string | undefined) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      csrfToken: string
      idempotencyKey: string
      recipientNumericId: string
      amount: string
      message: string
    }): Promise<MemberGiftRecord> => {
      const { data, error, response } = await apiClient.POST(
        "/api/v1/me/member-gifts",
        {
          params: {
            header: {
              "X-CSRF-Token": input.csrfToken,
              "Idempotency-Key": input.idempotencyKey,
            },
          },
          body: {
            recipient_numeric_id: input.recipientNumericId,
            amount: input.amount,
            message: input.message,
          },
        }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: memberGiftKeys.all }),
        queryClient.invalidateQueries({ queryKey: economyKeys.all }),
      ])
    },
    meta: { userId },
  })
}

import {
  queryOptions,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query"

import { attendanceKeys } from "~/features/economy/api/attendance.queries"
import { economyKeys } from "~/features/economy/api/economy.queries"
import { invitationKeys } from "~/features/invitation/api/invitations.queries"
import type { components } from "~/generated/api"
import { apiClient } from "~/shared/api/client"
import { ApiProblemError } from "~/shared/api/problem"

type Schemas = components["schemas"]
export type MagicShopOverview = Schemas["MagicShopOverview"]
export type MagicShopItemKey = Schemas["MagicShopItemKey"]
export type MagicShopOrder = Schemas["MagicShopOrder"]
export type MemberNameStyle = Schemas["MemberNameStyle"]
export type MagicGamesOverview = Schemas["MagicGamesOverview"]
export type MagicStoreSettings = Schemas["MagicStoreSettings"]
export type MagicStoreAdminOverview = Schemas["MagicStoreAdminOverview"]
export type LotteryRound = Schemas["LotteryRound"]
export type PvPChallenge = Schemas["PvPChallenge"]
export type PvPMode = Schemas["PvPMode"]
export type RPSChoice = Schemas["RPSChoice"]
export type ScratchCard = Schemas["ScratchCard"]
export type ScratchCardReceipt = Schemas["ScratchCardReceipt"]

export const magicStoreKeys = {
  all: ["magic-store"] as const,
  shop: (userId: string) => [...magicStoreKeys.all, "shop", userId] as const,
  games: (userId: string) => [...magicStoreKeys.all, "games", userId] as const,
  nameStyles: () => [...magicStoreKeys.all, "name-styles"] as const,
  admin: () => [...magicStoreKeys.all, "admin"] as const,
}

function unwrap<T>(result: {
  data?: T
  error?: unknown
  response: Response
}): T {
  if (!result.response.ok || result.data === undefined) {
    throw new ApiProblemError(result.response.status, result.error)
  }
  return result.data
}

export function isMagicRequestUncertain(error: unknown) {
  return (
    (error instanceof ApiProblemError && error.status >= 500) ||
    error instanceof TypeError ||
    error instanceof SyntaxError
  )
}

function retryMagicRequest(failureCount: number, error: Error) {
  return failureCount === 0 && isMagicRequestUncertain(error)
}

export function useMagicShop(userId: string | undefined) {
  return useQuery({
    queryKey: magicStoreKeys.shop(userId ?? "anonymous"),
    queryFn: async () => unwrap(await apiClient.GET("/api/v1/me/magic-shop")),
    enabled: Boolean(userId),
    staleTime: 10_000,
    retry: false,
  })
}

export function useMagicGames(userId: string | undefined) {
  return useQuery({
    queryKey: magicStoreKeys.games(userId ?? "anonymous"),
    queryFn: async () => unwrap(await apiClient.GET("/api/v1/me/magic-games")),
    enabled: Boolean(userId),
    staleTime: 5_000,
    refetchInterval: 30_000,
    retry: false,
  })
}

export function nameStylesQueryOptions() {
  return queryOptions({
    queryKey: magicStoreKeys.nameStyles(),
    queryFn: async () => unwrap(await apiClient.GET("/api/v1/name-styles")),
    staleTime: 5 * 60_000,
    retry: false,
  })
}

export function useNameStyles(enabled: boolean) {
  return useQuery({ ...nameStylesQueryOptions(), enabled })
}

function useInvalidateMagic() {
  const queryClient = useQueryClient()
  return () =>
    Promise.all([
      queryClient.invalidateQueries({ queryKey: magicStoreKeys.all }),
      queryClient.invalidateQueries({ queryKey: economyKeys.all }),
    ])
}

export function usePurchaseMagicShopItem() {
  const invalidate = useInvalidateMagic()
  const queryClient = useQueryClient()
  return useMutation({
    retry: retryMagicRequest,
    mutationFn: async (input: {
      csrfToken: string
      idempotencyKey: string
      itemKey: MagicShopItemKey
      quantity: number
      unitPrice: number
      styleKey?: string
    }) =>
      unwrap(
        await apiClient.POST("/api/v1/me/magic-shop/orders", {
          params: {
            header: {
              "X-CSRF-Token": input.csrfToken,
              "Idempotency-Key": input.idempotencyKey,
            },
          },
          body: {
            item_key: input.itemKey,
            quantity: input.quantity,
            expected_unit_price: input.unitPrice,
            ...(input.styleKey ? { style_key: input.styleKey } : {}),
          },
        })
      ),
    onSettled: async () => {
      await Promise.all([
        invalidate(),
        queryClient.invalidateQueries({ queryKey: attendanceKeys.all }),
        queryClient.invalidateQueries({ queryKey: invitationKeys.all }),
      ])
    },
  })
}

export function useSetNameStyle() {
  const invalidate = useInvalidateMagic()
  return useMutation({
    mutationFn: async (input: { csrfToken: string; styleKey: string }) =>
      unwrap(
        await apiClient.PUT("/api/v1/me/name-style", {
          params: { header: { "X-CSRF-Token": input.csrfToken } },
          body: { style_key: input.styleKey },
        })
      ),
    onSuccess: invalidate,
  })
}

export function usePlayScratchCard() {
  const invalidate = useInvalidateMagic()
  return useMutation({
    retry: retryMagicRequest,
    mutationFn: async (input: { csrfToken: string; idempotencyKey: string }) =>
      unwrap(
        await apiClient.POST("/api/v1/me/magic-games/scratch-cards", {
          params: {
            header: {
              "X-CSRF-Token": input.csrfToken,
              "Idempotency-Key": input.idempotencyKey,
            },
          },
        })
      ),
    onSettled: invalidate,
  })
}

export function useBuyLotteryTickets() {
  const invalidate = useInvalidateMagic()
  return useMutation({
    retry: retryMagicRequest,
    mutationFn: async (input: {
      csrfToken: string
      idempotencyKey: string
      quantity: number
      roundId: string
    }) =>
      unwrap(
        await apiClient.POST("/api/v1/me/magic-games/lottery/tickets", {
          params: {
            header: {
              "X-CSRF-Token": input.csrfToken,
              "Idempotency-Key": input.idempotencyKey,
            },
          },
          body: { quantity: input.quantity, round_id: input.roundId },
        })
      ),
    onSettled: invalidate,
  })
}

export function useCreatePvPChallenge() {
  const invalidate = useInvalidateMagic()
  return useMutation({
    retry: retryMagicRequest,
    mutationFn: async (input: {
      csrfToken: string
      idempotencyKey: string
      mode: PvPMode
      stake: number
      choice?: RPSChoice
      message?: string
    }) =>
      unwrap(
        await apiClient.POST("/api/v1/me/magic-games/pvp/challenges", {
          params: {
            header: {
              "X-CSRF-Token": input.csrfToken,
              "Idempotency-Key": input.idempotencyKey,
            },
          },
          body: {
            mode: input.mode,
            stake: input.stake,
            ...(input.choice ? { choice: input.choice } : {}),
            ...(input.message ? { message: input.message } : {}),
          },
        })
      ),
    onSettled: invalidate,
  })
}

export function useAcceptPvPChallenge() {
  const invalidate = useInvalidateMagic()
  return useMutation({
    retry: retryMagicRequest,
    mutationFn: async (input: {
      csrfToken: string
      challengeId: string
      choice?: RPSChoice
    }) =>
      unwrap(
        await apiClient.POST(
          "/api/v1/me/magic-games/pvp/challenges/{challenge_id}/accept",
          {
            params: {
              path: { challenge_id: input.challengeId },
              header: { "X-CSRF-Token": input.csrfToken },
            },
            body: input.choice ? { choice: input.choice } : {},
          }
        )
      ),
    onSettled: invalidate,
  })
}

export function useCancelPvPChallenge() {
  const invalidate = useInvalidateMagic()
  return useMutation({
    retry: retryMagicRequest,
    mutationFn: async (input: { csrfToken: string; challengeId: string }) =>
      unwrap(
        await apiClient.POST(
          "/api/v1/me/magic-games/pvp/challenges/{challenge_id}/cancel",
          {
            params: {
              path: { challenge_id: input.challengeId },
              header: { "X-CSRF-Token": input.csrfToken },
            },
          }
        )
      ),
    onSettled: invalidate,
  })
}

export function useMakeupAttendance() {
  const queryClient = useQueryClient()
  return useMutation({
    retry: retryMagicRequest,
    mutationFn: async (input: { csrfToken: string; idempotencyKey: string }) =>
      unwrap(
        await apiClient.POST("/api/v1/me/attendance/makeup", {
          params: {
            header: {
              "X-CSRF-Token": input.csrfToken,
              "Idempotency-Key": input.idempotencyKey,
            },
          },
        })
      ),
    onSettled: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: attendanceKeys.all }),
        queryClient.invalidateQueries({ queryKey: economyKeys.all }),
        queryClient.invalidateQueries({ queryKey: magicStoreKeys.all }),
      ])
    },
  })
}

export function useMagicStoreAdmin(enabled = true) {
  return useQuery({
    queryKey: magicStoreKeys.admin(),
    queryFn: async () =>
      unwrap(await apiClient.GET("/api/v1/admin/settings/magic-store")),
    enabled,
    retry: false,
  })
}

export function useUpdateMagicStoreSettings() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      csrfToken: string
      settings: MagicStoreSettings
      expectedVersion: number
      reason: string
    }) =>
      unwrap(
        await apiClient.PUT("/api/v1/admin/settings/magic-store", {
          params: { header: { "X-CSRF-Token": input.csrfToken } },
          body: {
            settings: input.settings,
            expected_version: input.expectedVersion,
            reason: input.reason,
          },
        })
      ),
    onSuccess: (data) => {
      queryClient.setQueryData(magicStoreKeys.admin(), data)
      void queryClient.invalidateQueries({ queryKey: magicStoreKeys.all })
    },
  })
}

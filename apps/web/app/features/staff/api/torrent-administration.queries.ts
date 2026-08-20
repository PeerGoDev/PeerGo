import {
  queryOptions,
  useMutation,
  useQueryClient,
} from "@tanstack/react-query"

import type { components } from "~/generated/api"
import type { ManagedTorrentFilters } from "~/features/staff/model/torrent-administration"
import { torrentKeys } from "~/features/torrent/api/torrent.queries"
import { apiClient } from "~/shared/api/client"
import { ApiProblemError } from "~/shared/api/problem"

export type ManagedTorrent = components["schemas"]["ManagedTorrent"]
export type ManagedTorrentPage = components["schemas"]["ManagedTorrentPage"]
export type ChangeTorrentAvailabilityRequest =
  components["schemas"]["ChangeTorrentAvailabilityRequest"]
export type TorrentAvailabilityResult =
  components["schemas"]["TorrentAvailabilityResult"]
export type UpdateTorrentPurchasePriceRequest =
  components["schemas"]["UpdateTorrentPurchasePriceRequest"]
export type TorrentPurchasePriceChange =
  components["schemas"]["TorrentPurchasePriceChange"]
export type ManagedPublishedTorrentContentChange =
  components["schemas"]["ManagedPublishedTorrentContentChange"]
export type ManagedPublishedTorrentContentChangePage =
  components["schemas"]["ManagedPublishedTorrentContentChangePage"]
export type DecidePublishedTorrentContentChangeRequest =
  components["schemas"]["DecidePublishedTorrentContentChangeRequest"]
export type PublishedTorrentContentChangeDecisionResult =
  components["schemas"]["PublishedTorrentContentChangeDecisionResult"]
export type ManagedPublishedTorrentScreenshotChange =
  components["schemas"]["ManagedPublishedTorrentScreenshotChange"]
export type ManagedPublishedTorrentScreenshotChangePage =
  components["schemas"]["ManagedPublishedTorrentScreenshotChangePage"]
export type DecidePublishedTorrentScreenshotChangeRequest =
  components["schemas"]["DecidePublishedTorrentScreenshotChangeRequest"]
export type PublishedTorrentScreenshotChangeDecisionResult =
  components["schemas"]["PublishedTorrentScreenshotChangeDecisionResult"]
export type ManagedTorrentWithdrawalRequest =
  components["schemas"]["ManagedTorrentWithdrawalRequest"]
export type ManagedTorrentWithdrawalPage =
  components["schemas"]["ManagedTorrentWithdrawalPage"]
export type DecideTorrentWithdrawalRequest =
  components["schemas"]["DecideTorrentWithdrawalRequest"]
export type TorrentWithdrawalDecisionResult =
  components["schemas"]["TorrentWithdrawalDecisionResult"]
export type ManagedTorrentReportCase =
  components["schemas"]["ManagedTorrentReportCase"]
export type ManagedTorrentReportCasePage =
  components["schemas"]["ManagedTorrentReportCasePage"]
export type CreateTorrentReportDecisionRequest =
  components["schemas"]["CreateTorrentReportDecisionRequest"]
export type TorrentReportDecisionResult =
  components["schemas"]["TorrentReportDecisionResult"]

export const torrentAdministrationKeys = {
  all: ["staff", "torrents", "administration"] as const,
  list: (filters: ManagedTorrentFilters) =>
    [...torrentAdministrationKeys.all, "list", filters] as const,
  contentChanges: () =>
    [...torrentAdministrationKeys.all, "content-changes"] as const,
  screenshotChanges: () =>
    [...torrentAdministrationKeys.all, "screenshot-changes"] as const,
  withdrawals: () => [...torrentAdministrationKeys.all, "withdrawals"] as const,
  reportCases: () =>
    [...torrentAdministrationKeys.all, "report-cases"] as const,
}

export const managedPublishedTorrentContentChangesQueryOptions = queryOptions({
  queryKey: torrentAdministrationKeys.contentChanges(),
  queryFn: async (): Promise<ManagedPublishedTorrentContentChangePage> => {
    const { data, error, response } = await apiClient.GET(
      "/api/v1/admin/torrents/content-changes",
      { params: { query: { status: "pending", limit: 20, offset: 0 } } }
    )
    if (!response.ok || !data) {
      throw new ApiProblemError(response.status, error)
    }
    return data
  },
  staleTime: 10_000,
  retry: false,
})

export const managedPublishedTorrentScreenshotChangesQueryOptions =
  queryOptions({
    queryKey: torrentAdministrationKeys.screenshotChanges(),
    queryFn: async (): Promise<ManagedPublishedTorrentScreenshotChangePage> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/admin/torrents/screenshot-changes",
        { params: { query: { status: "pending", limit: 20, offset: 0 } } }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    staleTime: 10_000,
    retry: false,
  })

export const managedTorrentWithdrawalsQueryOptions = queryOptions({
  queryKey: torrentAdministrationKeys.withdrawals(),
  queryFn: async (): Promise<ManagedTorrentWithdrawalPage> => {
    const { data, error, response } = await apiClient.GET(
      "/api/v1/admin/torrents/withdrawals",
      { params: { query: { status: "pending", limit: 20, offset: 0 } } }
    )
    if (!response.ok || !data) {
      throw new ApiProblemError(response.status, error)
    }
    return data
  },
  staleTime: 10_000,
  retry: false,
})

export const managedTorrentReportCasesQueryOptions = queryOptions({
  queryKey: torrentAdministrationKeys.reportCases(),
  queryFn: async (): Promise<ManagedTorrentReportCasePage> => {
    const { data, error, response } = await apiClient.GET(
      "/api/v1/admin/torrents/report-cases",
      { params: { query: { state: "open", limit: 20, offset: 0 } } }
    )
    if (!response.ok || !data) {
      throw new ApiProblemError(response.status, error)
    }
    return data
  },
  staleTime: 10_000,
  retry: false,
})

export function managedTorrentListQueryOptions(filters: ManagedTorrentFilters) {
  return queryOptions({
    queryKey: torrentAdministrationKeys.list(filters),
    queryFn: async (): Promise<ManagedTorrentPage> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/admin/torrents",
        {
          params: {
            query: {
              query: filters.query || undefined,
              state: filters.state === "all" ? undefined : filters.state,
              category_id: filters.categoryId || undefined,
              limit: filters.pageSize,
              offset: (filters.page - 1) * filters.pageSize,
            },
          },
        }
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

export function useChangeTorrentAvailability() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      torrentId: number
      csrfToken: string
      idempotencyKey: string
      body: ChangeTorrentAvailabilityRequest
    }): Promise<TorrentAvailabilityResult> => {
      const { data, error, response } = await apiClient.POST(
        "/api/v1/admin/torrents/{torrent_id}/availability",
        {
          params: {
            path: { torrent_id: input.torrentId },
            header: {
              "X-CSRF-Token": input.csrfToken,
              "Idempotency-Key": input.idempotencyKey,
            },
          },
          body: input.body,
        }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({
        queryKey: torrentAdministrationKeys.all,
      })
    },
  })
}

export function useUpdateTorrentPurchasePrice() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      torrentId: number
      csrfToken: string
      idempotencyKey: string
      body: UpdateTorrentPurchasePriceRequest
    }): Promise<TorrentPurchasePriceChange> => {
      const { data, error, response } = await apiClient.PUT(
        "/api/v1/admin/torrents/{torrent_id}/purchase-price",
        {
          params: {
            path: { torrent_id: input.torrentId },
            header: {
              "X-CSRF-Token": input.csrfToken,
              "Idempotency-Key": input.idempotencyKey,
            },
          },
          body: input.body,
        }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({
        queryKey: torrentAdministrationKeys.all,
      })
    },
  })
}

export function useDecidePublishedTorrentContentChange() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      requestId: string
      csrfToken: string
      idempotencyKey: string
      body: DecidePublishedTorrentContentChangeRequest
    }): Promise<PublishedTorrentContentChangeDecisionResult> => {
      const { data, error, response } = await apiClient.POST(
        "/api/v1/admin/torrents/content-changes/{request_id}/decision",
        {
          params: {
            path: { request_id: input.requestId },
            header: {
              "X-CSRF-Token": input.csrfToken,
              "Idempotency-Key": input.idempotencyKey,
            },
          },
          body: input.body,
        }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({
          queryKey: torrentAdministrationKeys.all,
        }),
        queryClient.invalidateQueries({ queryKey: torrentKeys.all }),
      ])
    },
  })
}

export function useDecidePublishedTorrentScreenshotChange() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      requestId: string
      csrfToken: string
      idempotencyKey: string
      body: DecidePublishedTorrentScreenshotChangeRequest
    }): Promise<PublishedTorrentScreenshotChangeDecisionResult> => {
      const { data, error, response } = await apiClient.POST(
        "/api/v1/admin/torrents/screenshot-changes/{request_id}/decision",
        {
          params: {
            path: { request_id: input.requestId },
            header: {
              "X-CSRF-Token": input.csrfToken,
              "Idempotency-Key": input.idempotencyKey,
            },
          },
          body: input.body,
        }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({
          queryKey: torrentAdministrationKeys.all,
        }),
        queryClient.invalidateQueries({ queryKey: torrentKeys.all }),
      ])
    },
  })
}

export function useDecideTorrentWithdrawal() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      requestId: string
      csrfToken: string
      idempotencyKey: string
      body: DecideTorrentWithdrawalRequest
    }): Promise<TorrentWithdrawalDecisionResult> => {
      const { data, error, response } = await apiClient.POST(
        "/api/v1/admin/torrents/withdrawals/{request_id}/decision",
        {
          params: {
            path: { request_id: input.requestId },
            header: {
              "X-CSRF-Token": input.csrfToken,
              "Idempotency-Key": input.idempotencyKey,
            },
          },
          body: input.body,
        }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({
          queryKey: torrentAdministrationKeys.all,
        }),
        queryClient.invalidateQueries({ queryKey: torrentKeys.all }),
      ])
    },
  })
}

export function useDecideTorrentReportCase() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      caseId: string
      csrfToken: string
      idempotencyKey: string
      body: CreateTorrentReportDecisionRequest
    }): Promise<TorrentReportDecisionResult> => {
      const { data, error, response } = await apiClient.POST(
        "/api/v1/admin/torrents/report-cases/{case_id}/decisions",
        {
          params: {
            path: { case_id: input.caseId },
            header: {
              "X-CSRF-Token": input.csrfToken,
              "Idempotency-Key": input.idempotencyKey,
            },
          },
          body: input.body,
        }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({
          queryKey: torrentAdministrationKeys.all,
        }),
        queryClient.invalidateQueries({ queryKey: torrentKeys.all }),
      ])
    },
    onError: async () => {
      await queryClient.invalidateQueries({
        queryKey: torrentAdministrationKeys.reportCases(),
      })
    },
  })
}

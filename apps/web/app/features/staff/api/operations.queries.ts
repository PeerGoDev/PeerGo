import {
  queryOptions,
  useMutation,
  useQueryClient,
} from "@tanstack/react-query"

import type { components } from "~/generated/api"
import { apiClient } from "~/shared/api/client"
import { ApiProblemError } from "~/shared/api/problem"

export type TrackerOperationsOverview =
  components["schemas"]["TrackerOperationsOverview"]
export type TrackerSettingsOverview =
  components["schemas"]["TrackerSettingsOverview"]
export type TrackerPolicyRevision =
  components["schemas"]["TrackerPolicyRevision"]
export type TrackerPolicySettings =
  components["schemas"]["TrackerPolicySettings"]
export type IssueTrackerPolicyRequest =
  components["schemas"]["IssueTrackerPolicyRequest"]
export type TorrentSettingsOverview =
  components["schemas"]["TorrentSettingsOverview"]
export type SettlementSettingsOverview =
  components["schemas"]["SettlementSettingsOverview"]
export type EconomySettingsOverview =
  components["schemas"]["EconomySettingsOverview"]
export type WorkerOperationsOverview =
  components["schemas"]["WorkerOperationsOverview"]
export type StorageOperationsOverview =
  components["schemas"]["StorageOperationsOverview"]
export type VIPProfileSettingsOverview =
  components["schemas"]["VIPProfileSettingsOverview"]
export type EmailSettingsOverview =
  components["schemas"]["EmailSettingsOverview"]
export type TestEmailDeliveryRequest =
  components["schemas"]["TestEmailDeliveryRequest"]
export type EmailDeliveryTestReceipt =
  components["schemas"]["EmailDeliveryTestReceipt"]
export type TorrentPurchasePolicySettings =
  components["schemas"]["TorrentPurchasePolicySettings"]
export type UpdateTorrentPurchasePolicyRequest =
  components["schemas"]["UpdateTorrentPurchasePolicyRequest"]
export type IssueTorrentUploadPolicyRequest =
  components["schemas"]["IssueTorrentUploadPolicyRequest"]
export type TorrentUploadPolicyRevision =
  components["schemas"]["TorrentUploadPolicyRevision"]

export const operationsKeys = {
  all: ["staff", "operations"] as const,
  tracker: () => [...operationsKeys.all, "tracker"] as const,
  trackerSettings: () => [...operationsKeys.all, "tracker-settings"] as const,
  torrentSettings: () => [...operationsKeys.all, "torrent-settings"] as const,
  settlementSettings: () =>
    [...operationsKeys.all, "settlement-settings"] as const,
  economySettings: () => [...operationsKeys.all, "economy-settings"] as const,
  workers: () => [...operationsKeys.all, "workers"] as const,
  storage: () => [...operationsKeys.all, "storage"] as const,
  vipProfile: () => [...operationsKeys.all, "vip-profile"] as const,
  email: () => [...operationsKeys.all, "email"] as const,
}

export function economySettingsQueryOptions() {
  return queryOptions({
    queryKey: operationsKeys.economySettings(),
    queryFn: async (): Promise<EconomySettingsOverview> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/admin/settings/economy"
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

export function settlementSettingsQueryOptions() {
  return queryOptions({
    queryKey: operationsKeys.settlementSettings(),
    queryFn: async (): Promise<SettlementSettingsOverview> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/admin/settings/settlement"
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

export function torrentSettingsQueryOptions() {
  return queryOptions({
    queryKey: operationsKeys.torrentSettings(),
    queryFn: async (): Promise<TorrentSettingsOverview> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/admin/settings/torrents"
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

export function useUpdateTorrentPurchasePolicySettings() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      csrfToken: string
      idempotencyKey: string
      body: UpdateTorrentPurchasePolicyRequest
    }): Promise<TorrentPurchasePolicySettings> => {
      const { data, error, response } = await apiClient.PUT(
        "/api/v1/admin/settings/torrents",
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
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({
        queryKey: operationsKeys.torrentSettings(),
      })
    },
  })
}

export function useIssueTorrentUploadPolicyRevision() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      csrfToken: string
      idempotencyKey: string
      body: IssueTorrentUploadPolicyRequest
    }): Promise<TorrentUploadPolicyRevision> => {
      const { data, error, response } = await apiClient.POST(
        "/api/v1/admin/settings/torrents/upload-policy-revisions",
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
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({
        queryKey: operationsKeys.torrentSettings(),
      })
    },
  })
}

export function trackerSettingsQueryOptions() {
  return queryOptions({
    queryKey: operationsKeys.trackerSettings(),
    queryFn: async (): Promise<TrackerSettingsOverview> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/admin/settings/tracker"
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

export function useIssueTrackerPolicy() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      csrfToken: string
      idempotencyKey: string
      body: IssueTrackerPolicyRequest
    }): Promise<TrackerPolicyRevision> => {
      const { data, error, response } = await apiClient.PUT(
        "/api/v1/admin/settings/tracker",
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
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({
        queryKey: operationsKeys.trackerSettings(),
      })
    },
  })
}

export function emailSettingsQueryOptions() {
  return queryOptions({
    queryKey: operationsKeys.email(),
    queryFn: async (): Promise<EmailSettingsOverview> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/admin/settings/email"
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

export function useTestEmailDelivery() {
  return useMutation({
    mutationFn: async (input: {
      csrfToken: string
      body: TestEmailDeliveryRequest
    }): Promise<EmailDeliveryTestReceipt> => {
      const { data, error, response } = await apiClient.POST(
        "/api/v1/admin/settings/email",
        {
          params: { header: { "X-CSRF-Token": input.csrfToken } },
          body: input.body,
        }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
  })
}

export function vipProfileSettingsQueryOptions() {
  return queryOptions({
    queryKey: operationsKeys.vipProfile(),
    queryFn: async (): Promise<VIPProfileSettingsOverview> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/admin/settings/vip-profile"
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

export function storageOperationsQueryOptions() {
  return queryOptions({
    queryKey: operationsKeys.storage(),
    queryFn: async (): Promise<StorageOperationsOverview> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/admin/operations/storage"
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

export function trackerOperationsQueryOptions() {
  return queryOptions({
    queryKey: operationsKeys.tracker(),
    queryFn: async (): Promise<TrackerOperationsOverview> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/admin/operations/tracker"
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    staleTime: 5_000,
    refetchInterval: 15_000,
    retry: false,
  })
}

export function workerOperationsQueryOptions() {
  return queryOptions({
    queryKey: operationsKeys.workers(),
    queryFn: async (): Promise<WorkerOperationsOverview> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/admin/operations/workers"
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    staleTime: 5_000,
    refetchInterval: 15_000,
    retry: false,
  })
}

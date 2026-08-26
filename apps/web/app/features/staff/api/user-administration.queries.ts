import {
  queryOptions,
  useMutation,
  useQueryClient,
} from "@tanstack/react-query"

import type { components } from "~/generated/api"
import type { ManagedUserFilters } from "~/features/staff/model/user-administration"
import { apiClient } from "~/shared/api/client"
import { ApiProblemError } from "~/shared/api/problem"

export type ManagedUserSummary = components["schemas"]["ManagedUserSummary"]
export type ManagedUserPage = components["schemas"]["ManagedUserPage"]
export type ManagedUserDetail = components["schemas"]["ManagedUserDetail"]
export type CreateAccountRestrictionRequest =
  components["schemas"]["CreateAccountRestrictionRequest"]
export type RevokeAccountRestrictionRequest =
  components["schemas"]["RevokeAccountRestrictionRequest"]
export type ChangeManualDownloadRestrictionRequest =
  components["schemas"]["ChangeManualDownloadRestrictionRequest"]
export type RevokeManualDownloadRestrictionRequest =
  components["schemas"]["RevokeManualDownloadRestrictionRequest"]
export type ChangeVIPRequest = components["schemas"]["ChangeVIPRequest"]
export type ReactivateManagedUserRequest =
  components["schemas"]["ReactivateManagedUserRequest"]

export const userAdministrationKeys = {
  all: ["staff", "identity", "users"] as const,
  list: (filters: ManagedUserFilters) =>
    [...userAdministrationKeys.all, "list", filters] as const,
  detail: (userId: string) =>
    [...userAdministrationKeys.all, "detail", userId] as const,
}

export function managedUserListQueryOptions(filters: ManagedUserFilters) {
  return queryOptions({
    queryKey: userAdministrationKeys.list(filters),
    queryFn: async (): Promise<ManagedUserPage> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/admin/users",
        {
          params: {
            query: {
              query: filters.query || undefined,
              filter: filters.status === "all" ? undefined : filters.status,
              page: filters.page,
              page_size: filters.pageSize,
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

export function managedUserDetailQueryOptions(userId: string) {
  return queryOptions({
    queryKey: userAdministrationKeys.detail(userId),
    queryFn: async (): Promise<ManagedUserDetail> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/admin/users/{user_id}",
        { params: { path: { user_id: userId } } }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    enabled: Boolean(userId),
    staleTime: 10_000,
    retry: false,
  })
}

export function useCreateAccountRestriction() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      userId: string
      csrfToken: string
      body: CreateAccountRestrictionRequest
    }): Promise<ManagedUserDetail> => {
      const { data, error, response } = await apiClient.POST(
        "/api/v1/admin/users/{user_id}/account-restrictions",
        {
          params: {
            path: { user_id: input.userId },
            header: { "X-CSRF-Token": input.csrfToken },
          },
          body: input.body,
        }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    onSuccess: async (detail) => {
      queryClient.setQueryData(userAdministrationKeys.detail(detail.id), detail)
      await queryClient.invalidateQueries({
        queryKey: userAdministrationKeys.all,
      })
    },
  })
}

export function useRevokeAccountRestriction() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      userId: string
      restrictionId: string
      csrfToken: string
      body: RevokeAccountRestrictionRequest
    }): Promise<ManagedUserDetail> => {
      const { data, error, response } = await apiClient.POST(
        "/api/v1/admin/users/{user_id}/account-restrictions/{restriction_id}/revocations",
        {
          params: {
            path: {
              user_id: input.userId,
              restriction_id: input.restrictionId,
            },
            header: { "X-CSRF-Token": input.csrfToken },
          },
          body: input.body,
        }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    onSuccess: async (detail) => {
      queryClient.setQueryData(userAdministrationKeys.detail(detail.id), detail)
      await queryClient.invalidateQueries({
        queryKey: userAdministrationKeys.all,
      })
    },
  })
}

export function useReactivateManagedUser() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      userId: string
      csrfToken: string
      idempotencyKey: string
      body: ReactivateManagedUserRequest
    }): Promise<ManagedUserDetail> => {
      const { data, error, response } = await apiClient.POST(
        "/api/v1/admin/users/{user_id}/reactivations",
        {
          params: {
            path: { user_id: input.userId },
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
    onSuccess: async (detail) => {
      queryClient.setQueryData(userAdministrationKeys.detail(detail.id), detail)
      await queryClient.invalidateQueries({
        queryKey: userAdministrationKeys.all,
      })
    },
  })
}

function useManualDownloadRestrictionMutation(method: "POST" | "PUT") {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      userId: string
      csrfToken: string
      body: ChangeManualDownloadRestrictionRequest
    }): Promise<ManagedUserDetail> => {
      const request = {
        params: {
          path: { user_id: input.userId },
          header: { "X-CSRF-Token": input.csrfToken },
        },
        body: input.body,
      }
      const { data, error, response } =
        method === "POST"
          ? await apiClient.POST(
              "/api/v1/admin/users/{user_id}/manual-download-restriction",
              request
            )
          : await apiClient.PUT(
              "/api/v1/admin/users/{user_id}/manual-download-restriction",
              request
            )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    onSuccess: async (detail) => {
      queryClient.setQueryData(userAdministrationKeys.detail(detail.id), detail)
      await queryClient.invalidateQueries({
        queryKey: userAdministrationKeys.all,
      })
    },
  })
}

export function useCreateManualDownloadRestriction() {
  return useManualDownloadRestrictionMutation("POST")
}

export function useUpdateManualDownloadRestriction() {
  return useManualDownloadRestrictionMutation("PUT")
}

export function useRevokeManualDownloadRestriction() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      userId: string
      csrfToken: string
      body: RevokeManualDownloadRestrictionRequest
    }): Promise<ManagedUserDetail> => {
      const { data, error, response } = await apiClient.POST(
        "/api/v1/admin/users/{user_id}/manual-download-restriction/revocations",
        {
          params: {
            path: { user_id: input.userId },
            header: { "X-CSRF-Token": input.csrfToken },
          },
          body: input.body,
        }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    onSuccess: async (detail) => {
      queryClient.setQueryData(userAdministrationKeys.detail(detail.id), detail)
      await queryClient.invalidateQueries({
        queryKey: userAdministrationKeys.all,
      })
    },
  })
}

export function useChangeVIP() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      userId: string
      csrfToken: string
      body: ChangeVIPRequest
    }): Promise<ManagedUserDetail> => {
      const { data, error, response } = await apiClient.PUT(
        "/api/v1/admin/users/{user_id}/vip",
        {
          params: {
            path: { user_id: input.userId },
            header: { "X-CSRF-Token": input.csrfToken },
          },
          body: input.body,
        }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    onSuccess: async (detail) => {
      queryClient.setQueryData(userAdministrationKeys.detail(detail.id), detail)
      await queryClient.invalidateQueries({
        queryKey: userAdministrationKeys.all,
      })
    },
  })
}

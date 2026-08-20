import {
  queryOptions,
  useMutation,
  useQueryClient,
} from "@tanstack/react-query"

import type { components } from "~/generated/api"
import { apiClient } from "~/shared/api/client"
import { ApiProblemError } from "~/shared/api/problem"

export type MedalDefinition = components["schemas"]["MedalDefinition"]
export type MedalDefinitionOverview =
  components["schemas"]["MedalDefinitionOverview"]
export type MedalDefinitionWriteRequest =
  components["schemas"]["MedalDefinitionWriteRequest"]
export type MedalSettings = components["schemas"]["MedalSettings"]
export type MedalSettingsWriteRequest =
  components["schemas"]["MedalSettingsWriteRequest"]

export const medalAdministrationKeys = {
  all: ["staff", "medals"] as const,
  overview: () => [...medalAdministrationKeys.all, "overview"] as const,
}

export function medalDefinitionOverviewQueryOptions() {
  return queryOptions({
    queryKey: medalAdministrationKeys.overview(),
    queryFn: async (): Promise<MedalDefinitionOverview> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/admin/settings/medals"
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

export function useCreateMedalDefinition() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      csrfToken: string
      body: MedalDefinitionWriteRequest
    }): Promise<MedalDefinition> => {
      const { data, error, response } = await apiClient.POST(
        "/api/v1/admin/settings/medals",
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
    onSuccess: async () => {
      await queryClient.invalidateQueries({
        queryKey: medalAdministrationKeys.all,
      })
    },
  })
}

export function useUpdateMedalSettings() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      csrfToken: string
      body: MedalSettingsWriteRequest
    }): Promise<MedalSettings> => {
      const { data, error, response } = await apiClient.PUT(
        "/api/v1/admin/settings/medals",
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
    onSuccess: async () => {
      await queryClient.invalidateQueries({
        queryKey: medalAdministrationKeys.all,
      })
    },
  })
}

export function useUpdateMedalDefinition() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      csrfToken: string
      medalId: number
      body: MedalDefinitionWriteRequest
    }): Promise<MedalDefinition> => {
      const { data, error, response } = await apiClient.PUT(
        "/api/v1/admin/settings/medals/{medal_id}",
        {
          params: {
            path: { medal_id: input.medalId },
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
    onSuccess: async () => {
      await queryClient.invalidateQueries({
        queryKey: medalAdministrationKeys.all,
      })
    },
  })
}

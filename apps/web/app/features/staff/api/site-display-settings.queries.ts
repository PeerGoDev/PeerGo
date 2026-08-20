import {
  type QueryClient,
  queryOptions,
  useMutation,
  useQueryClient,
} from "@tanstack/react-query"

import { siteKeys } from "~/features/site/api/site.queries"
import type { components } from "~/generated/api"
import { apiClient } from "~/shared/api/client"
import { ApiProblemError } from "~/shared/api/problem"

export type SiteDisplaySettings = components["schemas"]["SiteDisplaySettings"]
export type UpdateSiteDisplaySettingsRequest =
  components["schemas"]["UpdateSiteDisplaySettingsRequest"]

export const siteDisplaySettingsKeys = {
  all: ["staff", "settings", "site-display"] as const,
  detail: () => [...siteDisplaySettingsKeys.all, "detail"] as const,
}

export const siteDisplaySettingsQueryOptions = queryOptions({
  queryKey: siteDisplaySettingsKeys.detail(),
  queryFn: async (): Promise<SiteDisplaySettings> => {
    const { data, error, response } = await apiClient.GET(
      "/api/v1/admin/settings/site"
    )
    if (!response.ok || !data) {
      throw new ApiProblemError(response.status, error)
    }
    return data
  },
  staleTime: 10_000,
  retry: false,
})

export function useUpdateSiteDisplaySettings() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      csrfToken: string
      body: UpdateSiteDisplaySettingsRequest
    }): Promise<SiteDisplaySettings> => {
      const { data, error, response } = await apiClient.PUT(
        "/api/v1/admin/settings/site",
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
    onSuccess: invalidateSiteDisplaySettingsQueries(queryClient),
    onError: async () => {
      await queryClient.invalidateQueries({
        queryKey: siteDisplaySettingsKeys.all,
      })
    },
  })
}

function invalidateSiteDisplaySettingsQueries(queryClient: QueryClient) {
  return async () => {
    await Promise.all([
      queryClient.invalidateQueries({
        queryKey: siteDisplaySettingsKeys.all,
      }),
      queryClient.invalidateQueries({ queryKey: siteKeys.all }),
    ])
  }
}

import {
  queryOptions,
  useMutation,
  useQueryClient,
} from "@tanstack/react-query"

import type { components } from "~/generated/api"
import { apiClient } from "~/shared/api/client"
import { ApiProblemError } from "~/shared/api/problem"

export type RSSSettings = components["schemas"]["RSSSettings"]
export type UpdateRSSSettingsRequest =
  components["schemas"]["UpdateRSSSettingsRequest"]

export const rssSettingsKeys = {
  all: ["staff", "settings", "rss"] as const,
}

export const rssSettingsQueryOptions = queryOptions({
  queryKey: rssSettingsKeys.all,
  queryFn: async (): Promise<RSSSettings> => {
    const { data, error, response } = await apiClient.GET(
      "/api/v1/admin/settings/rss"
    )
    if (!response.ok || !data) {
      throw new ApiProblemError(response.status, error)
    }
    return data
  },
  staleTime: 10_000,
  retry: false,
})

export function useUpdateRSSSettings() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      csrfToken: string
      body: UpdateRSSSettingsRequest
    }): Promise<RSSSettings> => {
      const { data, error, response } = await apiClient.PUT(
        "/api/v1/admin/settings/rss",
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
      await queryClient.invalidateQueries({ queryKey: rssSettingsKeys.all })
    },
  })
}

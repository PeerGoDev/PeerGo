import {
  queryOptions,
  useMutation,
  useQueryClient,
} from "@tanstack/react-query"

import { siteKeys } from "~/features/site/api/site.queries"
import type { components } from "~/generated/api"
import { apiClient } from "~/shared/api/client"
import { ApiProblemError } from "~/shared/api/problem"

export type RegistrationPolicySettings =
  components["schemas"]["RegistrationPolicySettings"]
export type UpdateRegistrationPolicySettingsRequest =
  components["schemas"]["UpdateRegistrationPolicySettingsRequest"]

export const registrationPolicySettingsKeys = {
  all: ["staff", "settings", "registration-policy"] as const,
  detail: () => [...registrationPolicySettingsKeys.all, "detail"] as const,
}

export const registrationPolicySettingsQueryOptions = queryOptions({
  queryKey: registrationPolicySettingsKeys.detail(),
  queryFn: async (): Promise<RegistrationPolicySettings> => {
    const { data, error, response } = await apiClient.GET(
      "/api/v1/admin/settings/registration"
    )
    if (!response.ok || !data) {
      throw new ApiProblemError(response.status, error)
    }
    return data
  },
  staleTime: 10_000,
  retry: false,
})

export function useUpdateRegistrationPolicySettings() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      csrfToken: string
      body: UpdateRegistrationPolicySettingsRequest
    }): Promise<RegistrationPolicySettings> => {
      const { data, error, response } = await apiClient.PUT(
        "/api/v1/admin/settings/registration",
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
      await Promise.all([
        queryClient.invalidateQueries({
          queryKey: registrationPolicySettingsKeys.all,
        }),
        queryClient.invalidateQueries({ queryKey: siteKeys.all }),
      ])
    },
    onError: async () => {
      await queryClient.invalidateQueries({
        queryKey: registrationPolicySettingsKeys.all,
      })
    },
  })
}

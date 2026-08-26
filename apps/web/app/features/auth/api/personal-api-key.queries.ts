import {
  queryOptions,
  useMutation,
  useQueryClient,
} from "@tanstack/react-query"

import type { components } from "~/generated/api"
import { apiClient } from "~/shared/api/client"
import { ApiProblemError } from "~/shared/api/problem"

export type PersonalAPIKeyScope = components["schemas"]["PersonalAPIKeyScope"]
export type PersonalAPIKeyStatus = components["schemas"]["PersonalAPIKeyStatus"]
export type IssuedPersonalAPIKey = components["schemas"]["IssuedPersonalAPIKey"]

export const personalAPIKeyKeys = {
  all: ["personal-api-key"] as const,
  status: (userId: string | undefined) =>
    [...personalAPIKeyKeys.all, userId] as const,
}

export function personalAPIKeyQueryOptions(userId: string | undefined) {
  return queryOptions({
    queryKey: personalAPIKeyKeys.status(userId),
    queryFn: async (): Promise<PersonalAPIKeyStatus> => {
      const { data, error, response } =
        await apiClient.GET("/api/v1/me/api-key")
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

export function useRotatePersonalAPIKey() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      csrfToken: string
      scopes: PersonalAPIKeyScope[]
      expectedVersion?: number
    }): Promise<IssuedPersonalAPIKey> => {
      const { data, error, response } = await apiClient.POST(
        "/api/v1/me/api-key/rotations",
        {
          params: { header: { "X-CSRF-Token": input.csrfToken } },
          body: {
            scopes: input.scopes,
            ...(input.expectedVersion === undefined
              ? {}
              : { expected_version: input.expectedVersion }),
          },
        }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: personalAPIKeyKeys.all })
    },
  })
}

export function useRevokePersonalAPIKey() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      csrfToken: string
      expectedVersion: number
    }): Promise<void> => {
      const { error, response } = await apiClient.DELETE("/api/v1/me/api-key", {
        params: {
          query: { expected_version: input.expectedVersion },
          header: { "X-CSRF-Token": input.csrfToken },
        },
      })
      if (!response.ok) {
        throw new ApiProblemError(response.status, error)
      }
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: personalAPIKeyKeys.all })
    },
  })
}

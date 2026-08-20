import {
  queryOptions,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query"

import type { components } from "~/generated/api"
import { apiClient } from "~/shared/api/client"
import { ApiProblemError } from "~/shared/api/problem"

export type CreateSessionInput = components["schemas"]["CreateSessionRequest"]
export type WebSession = components["schemas"]["WebSession"]

export const sessionKeys = {
  all: ["web-session"] as const,
  current: () => [...sessionKeys.all, "current"] as const,
}

export const webSessionQueryOptions = queryOptions({
  queryKey: sessionKeys.current(),
  queryFn: async (): Promise<WebSession | null> => {
    const { data, error, response } = await apiClient.GET("/api/v1/session")
    if (response.status === 204) {
      return null
    }
    if (!response.ok || !data) {
      throw new ApiProblemError(response.status, error)
    }
    return data
  },
  staleTime: 60_000,
  retry: false,
})

export function useWebSession() {
  return useQuery(webSessionQueryOptions)
}

export function useCreateWebSession() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: CreateSessionInput): Promise<WebSession> => {
      const { data, error, response } = await apiClient.POST(
        "/api/v1/session",
        { body: input }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    onSuccess: (session) => {
      queryClient.setQueryData(sessionKeys.current(), session)
    },
  })
}

export function useDeleteWebSession() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (csrfToken: string): Promise<void> => {
      const { error, response } = await apiClient.DELETE("/api/v1/session", {
        params: { header: { "X-CSRF-Token": csrfToken } },
      })
      if (!response.ok) {
        throw new ApiProblemError(response.status, error)
      }
    },
    onSuccess: () => {
      queryClient.setQueryData(sessionKeys.current(), null)
    },
  })
}

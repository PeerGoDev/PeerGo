import {
  queryOptions,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query"

import type { components } from "~/generated/api"
import {
  sessionKeys,
  type WebSession,
} from "~/features/auth/api/session.mutations"
import { apiClient } from "~/shared/api/client"
import { ApiProblemError } from "~/shared/api/problem"

export type AccountSecurityOverview =
  components["schemas"]["AccountSecurityOverview"]
export type UserWebSession = components["schemas"]["UserWebSession"]
export type UserWebSessionList = components["schemas"]["UserWebSessionList"]

export const sessionSecurityKeys = {
  all: ["account-security"] as const,
  overview: (userId: string) =>
    [...sessionSecurityKeys.all, userId, "overview"] as const,
  sessions: (userId: string) =>
    [...sessionSecurityKeys.all, userId, "sessions"] as const,
}

export function accountSecurityQueryOptions(userId: string) {
  return queryOptions({
    queryKey: sessionSecurityKeys.overview(userId),
    queryFn: async (): Promise<AccountSecurityOverview> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/me/security"
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    enabled: Boolean(userId),
  })
}

export function userWebSessionsQueryOptions(userId: string) {
  return queryOptions({
    queryKey: sessionSecurityKeys.sessions(userId),
    queryFn: async (): Promise<UserWebSessionList> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/me/sessions"
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    enabled: Boolean(userId),
  })
}

export function useAccountSecurity(userId?: string) {
  return useQuery(accountSecurityQueryOptions(userId ?? ""))
}

export function useUserWebSessions(userId?: string) {
  return useQuery(userWebSessionsQueryOptions(userId ?? ""))
}

type RevokeSessionInput = {
  sessionId: string
  current: boolean
  csrfToken: string
}

export function useRevokeUserWebSession(userId?: string) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: RevokeSessionInput): Promise<void> => {
      const { error, response } = await apiClient.DELETE(
        "/api/v1/me/sessions/{session_id}",
        {
          params: {
            path: { session_id: input.sessionId },
            header: { "X-CSRF-Token": input.csrfToken },
          },
        }
      )
      if (!response.ok) {
        throw new ApiProblemError(response.status, error)
      }
    },
    onSuccess: (_, input) => {
      if (input.current) {
        queryClient.setQueryData<WebSession | null>(sessionKeys.current(), null)
        queryClient.removeQueries({ queryKey: sessionSecurityKeys.all })
        return
      }
      if (userId) {
        void queryClient.invalidateQueries({
          queryKey: sessionSecurityKeys.sessions(userId),
        })
      }
    },
  })
}

export function useRevokeOtherWebSessions(userId?: string) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (csrfToken: string): Promise<void> => {
      const { error, response } = await apiClient.DELETE(
        "/api/v1/me/sessions",
        { params: { header: { "X-CSRF-Token": csrfToken } } }
      )
      if (!response.ok) {
        throw new ApiProblemError(response.status, error)
      }
    },
    onSuccess: () => {
      if (userId) {
        void queryClient.invalidateQueries({
          queryKey: sessionSecurityKeys.sessions(userId),
        })
      }
    },
  })
}

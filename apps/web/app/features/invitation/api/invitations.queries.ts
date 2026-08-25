import {
  queryOptions,
  useMutation,
  useQueryClient,
} from "@tanstack/react-query"

import type { components } from "~/generated/api"
import { apiClient } from "~/shared/api/client"
import { ApiProblemError } from "~/shared/api/problem"

export type InvitationOverview = components["schemas"]["InvitationOverview"]
export type InvitationIssueResult =
  components["schemas"]["InvitationIssueResult"]
export type MemberInvitation = components["schemas"]["MemberInvitation"]

export const invitationKeys = {
  all: ["my-invitations"] as const,
  page: (userId: string | undefined, limit: number, offset: number) =>
    [...invitationKeys.all, userId, limit, offset] as const,
}

export function invitationOverviewQueryOptions(
  userId: string | undefined,
  limit: number,
  offset: number
) {
  return queryOptions({
    queryKey: invitationKeys.page(userId, limit, offset),
    queryFn: async (): Promise<InvitationOverview> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/me/invitations",
        { params: { query: { limit, offset } } }
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

export function useIssueInvitation() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      csrfToken: string
      email: string
    }): Promise<InvitationIssueResult> => {
      const { data, error, response } = await apiClient.POST(
        "/api/v1/me/invitations",
        {
          params: { header: { "X-CSRF-Token": input.csrfToken } },
          body: { email: input.email },
        }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: invitationKeys.all })
    },
  })
}

export function useRevokeInvitation() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      csrfToken: string
      invitationId: string
    }): Promise<MemberInvitation> => {
      const { data, error, response } = await apiClient.POST(
        "/api/v1/me/invitations/{invitation_id}/revocation",
        {
          params: {
            path: { invitation_id: input.invitationId },
            header: { "X-CSRF-Token": input.csrfToken },
          },
        }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: invitationKeys.all })
    },
  })
}

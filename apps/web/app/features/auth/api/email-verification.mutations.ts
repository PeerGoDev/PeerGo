import { useMutation, useQueryClient } from "@tanstack/react-query"

import type { components } from "~/generated/api"
import {
  sessionKeys,
  type WebSession,
} from "~/features/auth/api/session.mutations"
import { apiClient } from "~/shared/api/client"
import { ApiProblemError } from "~/shared/api/problem"

type EmailVerificationDispatch =
  components["schemas"]["EmailVerificationDispatch"]
type EmailVerificationResult = components["schemas"]["EmailVerificationResult"]

export function useRequestEmailVerification() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async ({
      email,
      csrfToken,
    }: {
      email: string
      csrfToken: string
    }): Promise<EmailVerificationDispatch> => {
      const { data, error, response } = await apiClient.POST(
        "/api/v1/email-verifications",
        {
          params: { header: { "X-CSRF-Token": csrfToken } },
          body: { email },
        }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    onSuccess: (result) => {
      if (!result.already_verified) {
        return
      }
      queryClient.setQueryData<WebSession | null>(
        sessionKeys.current(),
        (session) =>
          session
            ? {
                ...session,
                user: { ...session.user, email_verified: true },
              }
            : session
      )
    },
  })
}

export function useConfirmEmailVerification() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (token: string): Promise<EmailVerificationResult> => {
      const { data, error, response } = await apiClient.POST(
        "/api/v1/email-verifications/confirm",
        { body: { token } }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    onSuccess: (result) => {
      queryClient.setQueryData<WebSession | null>(
        sessionKeys.current(),
        (session) =>
          session && session.user.id === result.user.id
            ? { ...session, user: result.user }
            : session
      )
    },
  })
}

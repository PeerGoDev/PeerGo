import { useMutation, useQueryClient } from "@tanstack/react-query"

import type { components } from "~/generated/api"
import { sessionKeys } from "~/features/auth/api/session.mutations"
import { staffSessionKeys } from "~/features/staff/api/staff-session.mutations"
import { apiClient } from "~/shared/api/client"
import { ApiProblemError } from "~/shared/api/problem"

type PasswordRecoveryDispatch =
  components["schemas"]["PasswordRecoveryDispatch"]
type PasswordRecoveryResult = components["schemas"]["PasswordRecoveryResult"]
type PasswordRecoveryRequest =
  components["schemas"]["RequestPasswordRecoveryRequest"]

export function useRequestPasswordRecovery() {
  return useMutation({
    mutationFn: async (
      input: PasswordRecoveryRequest
    ): Promise<PasswordRecoveryDispatch> => {
      const { data, error, response } = await apiClient.POST(
        "/api/v1/password-recovery-requests",
        { body: input }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
  })
}

export function useConfirmPasswordRecovery() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async ({
      token,
      newPassword,
    }: {
      token: string
      newPassword: string
    }): Promise<PasswordRecoveryResult> => {
      const { data, error, response } = await apiClient.POST(
        "/api/v1/password-recoveries/confirm",
        { body: { token, new_password: newPassword } }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    onSuccess: async () => {
      // The Core transaction revoked every Web and staff session for this
      // account. Mirror that fact locally instead of showing stale identity.
      queryClient.setQueryData(sessionKeys.current(), null)
      queryClient.setQueryData(staffSessionKeys.current(), null)
      await queryClient.invalidateQueries({ queryKey: staffSessionKeys.all })
    },
  })
}

import { useMutation, useQueryClient } from "@tanstack/react-query"

import type { components } from "~/generated/api"
import { sessionSecurityKeys } from "~/features/auth/api/session-security.queries"
import { apiClient } from "~/shared/api/client"
import { ApiProblemError } from "~/shared/api/problem"

export type TOTPEnrollmentStart = components["schemas"]["TOTPEnrollmentStart"]
export type TOTPEnrollmentResult = components["schemas"]["TOTPEnrollmentResult"]
export type RecoveryCodeRotationResult =
  components["schemas"]["RecoveryCodeRotationResult"]
export type TwoFactorDisableResult =
  components["schemas"]["TwoFactorDisableResult"]

type SessionWrite = {
  csrfToken: string
}

export function useStartTOTPEnrollment() {
  return useMutation({
    gcTime: 0,
    mutationFn: async (
      input: SessionWrite & { password: string }
    ): Promise<TOTPEnrollmentStart> => {
      const { data, error, response } = await apiClient.POST(
        "/api/v1/me/totp/enrollments",
        {
          params: { header: { "X-CSRF-Token": input.csrfToken } },
          body: { password: input.password },
        }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
  })
}

export function useConfirmTOTPEnrollment() {
  return useMutation({
    gcTime: 0,
    mutationFn: async (
      input: SessionWrite & { enrollmentId: string; code: string }
    ): Promise<TOTPEnrollmentResult> => {
      const { data, error, response } = await apiClient.POST(
        "/api/v1/me/totp/enrollments/{enrollment_id}/confirm",
        {
          params: {
            path: { enrollment_id: input.enrollmentId },
            header: { "X-CSRF-Token": input.csrfToken },
          },
          body: { code: input.code },
        }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
  })
}

type ReauthenticateTwoFactor = SessionWrite & {
  idempotencyKey: string
  password: string
  secondFactorCode: string
}

export function useRotateTOTPRecoveryCodes(userId: string) {
  const queryClient = useQueryClient()
  return useMutation({
    gcTime: 0,
    mutationFn: async (
      input: ReauthenticateTwoFactor
    ): Promise<RecoveryCodeRotationResult> => {
      const { data, error, response } = await apiClient.POST(
        "/api/v1/me/totp/recovery-code-rotations",
        {
          params: {
            header: {
              "X-CSRF-Token": input.csrfToken,
              "Idempotency-Key": input.idempotencyKey,
            },
          },
          body: {
            password: input.password,
            second_factor_code: input.secondFactorCode,
          },
        }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    onSuccess: () =>
      queryClient.invalidateQueries({
        queryKey: sessionSecurityKeys.overview(userId),
      }),
  })
}

export function useDisableTOTP(userId: string) {
  const queryClient = useQueryClient()
  return useMutation({
    gcTime: 0,
    mutationFn: async (
      input: ReauthenticateTwoFactor
    ): Promise<TwoFactorDisableResult> => {
      const { data, error, response } = await apiClient.DELETE(
        "/api/v1/me/totp",
        {
          params: {
            header: {
              "X-CSRF-Token": input.csrfToken,
              "Idempotency-Key": input.idempotencyKey,
            },
          },
          body: {
            password: input.password,
            second_factor_code: input.secondFactorCode,
          },
        }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    onSuccess: () =>
      queryClient.invalidateQueries({
        queryKey: sessionSecurityKeys.overview(userId),
      }),
  })
}

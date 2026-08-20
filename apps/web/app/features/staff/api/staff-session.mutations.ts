import {
  queryOptions,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query"

import type { components } from "~/generated/api"
import {
  createStaffCredential,
  requestStaffAssertion,
} from "~/features/staff/model/webauthn"
import { apiClient } from "~/shared/api/client"
import { ApiProblemError } from "~/shared/api/problem"

export type StaffSession = components["schemas"]["StaffSession"]
export type StaffCredentialEnrollment =
  components["schemas"]["StaffCredentialEnrollment"]

export const staffSessionKeys = {
  all: ["staff"] as const,
  current: () => [...staffSessionKeys.all, "session"] as const,
  capabilities: (userId: string) =>
    [...staffSessionKeys.all, "capabilities", userId] as const,
}

export const staffSessionQueryOptions = queryOptions({
  queryKey: staffSessionKeys.current(),
  queryFn: async (): Promise<StaffSession | null> => {
    const { data, error, response } = await apiClient.GET(
      "/api/v1/admin/session"
    )
    if (response.status === 204) {
      return null
    }
    if (!response.ok || !data) {
      throw new ApiProblemError(response.status, error)
    }
    return data
  },
  staleTime: 15_000,
  retry: false,
})

export function useStaffSession() {
  return useQuery(staffSessionQueryOptions)
}

export function useElevateStaffSession() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: elevateStaffSession,
    onSuccess: (session) => {
      queryClient.setQueryData(staffSessionKeys.current(), session)
    },
  })
}

export function useDeleteStaffSession() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (csrfToken: string): Promise<void> => {
      const { error, response } = await apiClient.DELETE(
        "/api/v1/admin/session",
        { params: { header: { "X-CSRF-Token": csrfToken } } }
      )
      if (!response.ok) {
        throw new ApiProblemError(response.status, error)
      }
    },
    onSuccess: async () => {
      queryClient.setQueryData(staffSessionKeys.current(), null)
      await queryClient.invalidateQueries({ queryKey: staffSessionKeys.all })
    },
  })
}

export function staffCapabilityQueryOptions(userId: string) {
  return queryOptions({
    queryKey: staffSessionKeys.capabilities(userId),
    queryFn: async () => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/admin/me/capabilities"
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    staleTime: 15_000,
    retry: false,
  })
}

export function useStaffCapabilities(userId: string | undefined) {
  return useQuery({
    ...staffCapabilityQueryOptions(userId ?? "anonymous"),
    enabled: Boolean(userId),
  })
}

export async function enrollStaffCredential(input: {
  bootstrapToken: string
  label: string
  csrfToken: string
}): Promise<StaffCredentialEnrollment> {
  const begin = await apiClient.POST("/api/v1/staff/enrollment/options", {
    params: { header: { "X-CSRF-Token": input.csrfToken } },
    body: {
      bootstrap_token: input.bootstrapToken,
      label: input.label,
    },
  })
  if (!begin.response.ok || !begin.data) {
    throw new ApiProblemError(begin.response.status, begin.error)
  }

  const credential = await createStaffCredential(begin.data.public_key)
  const complete = await apiClient.POST("/api/v1/staff/enrollment", {
    params: { header: { "X-CSRF-Token": input.csrfToken } },
    body: {
      bootstrap_token: input.bootstrapToken,
      challenge_id: begin.data.challenge_id,
      credential,
    },
  })
  if (!complete.response.ok || !complete.data) {
    throw new ApiProblemError(complete.response.status, complete.error)
  }
  return complete.data
}

async function elevateStaffSession(csrfToken: string): Promise<StaffSession> {
  const begin = await apiClient.POST("/api/v1/staff/elevation/options", {
    params: { header: { "X-CSRF-Token": csrfToken } },
  })
  if (!begin.response.ok || !begin.data) {
    throw new ApiProblemError(begin.response.status, begin.error)
  }

  const credential = await requestStaffAssertion(begin.data.public_key)
  const complete = await apiClient.POST("/api/v1/staff/elevation", {
    params: { header: { "X-CSRF-Token": csrfToken } },
    body: {
      challenge_id: begin.data.challenge_id,
      credential,
    },
  })
  if (!complete.response.ok || !complete.data) {
    throw new ApiProblemError(complete.response.status, complete.error)
  }
  return complete.data
}

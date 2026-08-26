import {
  queryOptions,
  useMutation,
  useQueryClient,
} from "@tanstack/react-query"

import type { components } from "~/generated/api"
import { apiClient } from "~/shared/api/client"
import { ApiProblemError } from "~/shared/api/problem"

export type MoviePilotCredentialStatus =
  components["schemas"]["MoviePilotCredentialStatus"]
export type IssuedMoviePilotCredential =
  components["schemas"]["IssuedMoviePilotCredential"]

export const moviePilotCredentialKeys = {
  all: ["moviepilot-credential"] as const,
  status: (userId: string | undefined) =>
    [...moviePilotCredentialKeys.all, userId] as const,
}

export function moviePilotCredentialQueryOptions(userId: string | undefined) {
  return queryOptions({
    queryKey: moviePilotCredentialKeys.status(userId),
    queryFn: async (): Promise<MoviePilotCredentialStatus> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/me/moviepilot-credential"
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

export function useRotateMoviePilotCredential() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      csrfToken: string
      expectedVersion?: number
    }): Promise<IssuedMoviePilotCredential> => {
      const { data, error, response } = await apiClient.POST(
        "/api/v1/me/moviepilot-credential/rotations",
        {
          params: { header: { "X-CSRF-Token": input.csrfToken } },
          body:
            input.expectedVersion === undefined
              ? {}
              : { expected_version: input.expectedVersion },
        }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({
        queryKey: moviePilotCredentialKeys.all,
      })
    },
  })
}

export function useRevokeMoviePilotCredential() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: {
      csrfToken: string
      expectedVersion: number
    }): Promise<void> => {
      const { error, response } = await apiClient.DELETE(
        "/api/v1/me/moviepilot-credential",
        {
          params: {
            query: { expected_version: input.expectedVersion },
            header: { "X-CSRF-Token": input.csrfToken },
          },
        }
      )
      if (!response.ok) {
        throw new ApiProblemError(response.status, error)
      }
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({
        queryKey: moviePilotCredentialKeys.all,
      })
    },
  })
}

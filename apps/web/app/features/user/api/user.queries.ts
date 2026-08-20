import { queryOptions, useQuery } from "@tanstack/react-query"

import type { components } from "~/generated/api"
import { apiClient } from "~/shared/api/client"
import { ApiProblemError } from "~/shared/api/problem"

export type PublicUserProfile = components["schemas"]["PublicUserProfile"]

export const userKeys = {
  all: ["users"] as const,
  profile: (username: string) =>
    [...userKeys.all, "profile", username.toLocaleLowerCase()] as const,
}

export function publicUserProfileQueryOptions(
  username: string,
  enabled = true
) {
  return queryOptions({
    queryKey: userKeys.profile(username),
    queryFn: async (): Promise<PublicUserProfile> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/users/{username}",
        { params: { path: { username } } }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    enabled: enabled && username.length > 0,
    staleTime: 60_000,
    retry: false,
  })
}

export function usePublicUserProfile(username: string, enabled = true) {
  return useQuery(publicUserProfileQueryOptions(username, enabled))
}

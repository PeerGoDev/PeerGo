import { useMutation, useQueryClient } from "@tanstack/react-query"

import type { components } from "~/generated/api"
import {
  sessionKeys,
  type WebSession,
} from "~/features/auth/api/session.mutations"
import { userKeys } from "~/features/user/api/user.queries"
import { apiClient } from "~/shared/api/client"
import { ApiProblemError } from "~/shared/api/problem"

type SessionUser = components["schemas"]["SessionUser"]

export function useUpdateMyProfile() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async ({
      displayName,
      csrfToken,
    }: {
      displayName: string
      csrfToken: string
    }): Promise<SessionUser> => {
      const { data, error, response } = await apiClient.PATCH(
        "/api/v1/me/profile",
        {
          params: { header: { "X-CSRF-Token": csrfToken } },
          body: { display_name: displayName },
        }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
    onSuccess: (user) => {
      queryClient.setQueryData<WebSession | null>(
        sessionKeys.current(),
        (session) => (session ? { ...session, user } : session)
      )
      void queryClient.invalidateQueries({ queryKey: userKeys.all })
    },
  })
}

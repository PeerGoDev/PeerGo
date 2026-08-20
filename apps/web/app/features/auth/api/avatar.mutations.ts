import { useMutation } from "@tanstack/react-query"

import type { components } from "~/generated/api"
import { resolveApiUrl } from "~/shared/api/client"
import { ApiProblemError } from "~/shared/api/problem"
import { announceAvatarRevision } from "~/shared/components/avatar-revision"

type AvatarRevision = components["schemas"]["AvatarRevision"]

export function useUpdateMyAvatar(username: string) {
  return useMutation({
    mutationFn: async ({
      image,
      csrfToken,
    }: {
      image: Blob
      csrfToken: string
    }): Promise<AvatarRevision> => {
      const response = await fetch(resolveApiUrl("/api/v1/me/avatar"), {
        method: "PUT",
        credentials: "include",
        headers: {
          "Content-Type": "image/jpeg",
          "X-CSRF-Token": csrfToken,
        },
        body: image,
      })
      const payload = await response.json().catch(() => undefined)
      if (!response.ok || !payload) {
        throw new ApiProblemError(response.status, payload)
      }
      return payload as AvatarRevision
    },
    onSuccess: (revision) => {
      announceAvatarRevision(username, revision.revision)
    },
  })
}

import { useMutation } from "@tanstack/react-query"

import type { components } from "~/generated/api"
import { apiClient } from "~/shared/api/client"
import { ApiProblemError } from "~/shared/api/problem"

export type CreateRegistrationInput =
  components["schemas"]["CreateRegistrationRequest"]
export type RegistrationResult = components["schemas"]["RegistrationResult"]

export function useCreateRegistration() {
  return useMutation({
    mutationFn: async ({
      idempotencyKey,
      input,
    }: {
      idempotencyKey: string
      input: CreateRegistrationInput
    }): Promise<RegistrationResult> => {
      const { data, error, response } = await apiClient.POST(
        "/api/v1/registrations",
        {
          params: { header: { "Idempotency-Key": idempotencyKey } },
          body: input,
        }
      )
      if (!response.ok || !data) {
        throw new ApiProblemError(response.status, error)
      }
      return data
    },
  })
}

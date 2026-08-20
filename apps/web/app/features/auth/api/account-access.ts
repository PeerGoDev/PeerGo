import type { components } from "~/generated/api"
import { apiClient } from "~/shared/api/client"
import { ApiProblemError } from "~/shared/api/problem"

export type AccountAccessCredentialProof =
  components["schemas"]["AccountAccessCredentialProof"]
export type AccountAccessStatus = components["schemas"]["AccountAccessStatus"]
export type AccountAccessAppeal = components["schemas"]["AccountAccessAppeal"]

// These calls intentionally do not use TanStack Query. Password and second
// factor values therefore never become a query key, cached query result or
// retained mutation variable; the page reads them from the form per request.
export async function inspectAccountAccess(
  credentials: AccountAccessCredentialProof
): Promise<AccountAccessStatus> {
  const { data, error, response } = await apiClient.POST(
    "/api/v1/account-access/status",
    { body: { credentials } }
  )
  if (!response.ok || !data) {
    throw new ApiProblemError(response.status, error)
  }
  return data
}

export async function submitAccountAccessAppeal(input: {
  credentials: AccountAccessCredentialProof
  statement: string
  idempotencyKey: string
}): Promise<AccountAccessAppeal> {
  const { data, error, response } = await apiClient.POST(
    "/api/v1/account-access/appeals",
    {
      params: { header: { "Idempotency-Key": input.idempotencyKey } },
      body: { credentials: input.credentials, statement: input.statement },
    }
  )
  if (!response.ok || !data) {
    throw new ApiProblemError(response.status, error)
  }
  return data
}

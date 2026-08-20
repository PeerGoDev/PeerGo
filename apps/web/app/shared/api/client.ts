import createClient from "openapi-fetch"

import type { paths } from "~/generated/api"

export const apiBaseUrl =
  import.meta.env.VITE_API_BASE_URL ??
  (typeof window === "undefined" ? "" : window.location.origin)

export const apiClient = createClient<paths>({
  baseUrl: apiBaseUrl,
  credentials: "include",
  // Resolve fetch at request time so tests and future observability wrappers
  // can replace the transport without creating a second API client.
  fetch: (...args) => globalThis.fetch(...args),
})

/**
 * Resolves an API path with the exact base used by the generated client.
 * XMLHttpRequest is reserved for the multipart upload boundary because Fetch
 * does not expose browser upload progress events.
 */
export function resolveApiUrl(path: string) {
  const normalizedPath = path.startsWith("/") ? path : `/${path}`
  return apiBaseUrl ? `${apiBaseUrl.replace(/\/$/, "")}${normalizedPath}` : path
}

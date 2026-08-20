/**
 * Accept only an origin that is safe to place in a production email action.
 * Paths, embedded credentials, query strings and fragments are rejected so a
 * caller cannot accidentally present a full action URL as an origin.
 */
export function isSecurePublicOrigin(raw: string) {
  try {
    const url = new URL(raw)
    return (
      url.protocol === "https:" &&
      Boolean(url.hostname) &&
      url.username === "" &&
      url.password === "" &&
      url.search === "" &&
      url.hash === "" &&
      (url.pathname === "" || url.pathname === "/")
    )
  } catch {
    return false
  }
}

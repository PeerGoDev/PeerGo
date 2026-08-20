type RandomSource = Pick<Crypto, "getRandomValues">

/**
 * Generate an RFC 4122 UUID v4 with Web Crypto entropy.
 *
 * Browsers expose getRandomValues() on non-secure LAN origins even though
 * randomUUID() is restricted to secure contexts. Keeping the fallback here
 * prevents every request-id caller from carrying its own weaker generator.
 */
export function createCryptoUuidV4(
  source: RandomSource
): ReturnType<Crypto["randomUUID"]> {
  const bytes = source.getRandomValues(new Uint8Array(16))
  bytes[6] = (bytes[6] & 0x0f) | 0x40
  bytes[8] = (bytes[8] & 0x3f) | 0x80

  const hexadecimal = Array.from(bytes, (value) =>
    value.toString(16).padStart(2, "0")
  ).join("")

  return `${hexadecimal.slice(0, 8)}-${hexadecimal.slice(8, 12)}-${hexadecimal.slice(12, 16)}-${hexadecimal.slice(16, 20)}-${hexadecimal.slice(20)}`
}

export function installCryptoRandomUuid(
  source: Crypto | undefined = globalThis.crypto
) {
  if (!source || typeof source.randomUUID === "function") {
    return false
  }

  Object.defineProperty(source, "randomUUID", {
    configurable: true,
    value: () => createCryptoUuidV4(source),
  })
  return true
}

installCryptoRandomUuid()

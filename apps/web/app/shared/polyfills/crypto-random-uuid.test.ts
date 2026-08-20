import { describe, expect, it } from "vitest"

import {
  createCryptoUuidV4,
  installCryptoRandomUuid,
} from "~/shared/polyfills/crypto-random-uuid"

describe("crypto.randomUUID compatibility", () => {
  it("creates an RFC 4122 version 4 UUID from Web Crypto bytes", () => {
    const source = deterministicCryptoSource()

    expect(createCryptoUuidV4(source)).toMatch(
      /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/
    )
  })

  it("installs the fallback only when randomUUID is unavailable", () => {
    const source = deterministicCryptoSource() as Crypto

    expect(installCryptoRandomUuid(source)).toBe(true)
    expect(source.randomUUID()).toMatch(
      /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/
    )
    expect(installCryptoRandomUuid(source)).toBe(false)
  })
})

function deterministicCryptoSource(): Pick<Crypto, "getRandomValues"> {
  return {
    getRandomValues: <T extends ArrayBufferView | null>(array: T) => {
      if (!(array instanceof Uint8Array)) {
        throw new TypeError("test source only accepts Uint8Array")
      }
      array.forEach((_, index) => {
        array[index] = index
      })
      return array as T
    },
  }
}

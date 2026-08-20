import type { components } from "~/generated/api"

type RequestOptions = components["schemas"]["WebAuthnRequestOptions"]
type CreationOptions = components["schemas"]["WebAuthnCreationOptions"]
type AssertionCredential = components["schemas"]["WebAuthnCredentialAssertion"]
type CreationCredential = components["schemas"]["WebAuthnCredentialCreation"]

export class WebAuthnClientError extends Error {
  constructor(message: string) {
    super(message)
    this.name = "WebAuthnClientError"
  }
}

export async function requestStaffAssertion(
  options: RequestOptions
): Promise<AssertionCredential> {
  assertWebAuthnAvailable()
  let credential: Credential | null
  try {
    credential = await navigator.credentials.get({
      publicKey: requestOptions(options),
    })
  } catch (error) {
    throw browserCeremonyError(error, "后台安全验证")
  }
  if (!(credential instanceof PublicKeyCredential)) {
    throw new WebAuthnClientError("浏览器没有返回有效的安全凭据。")
  }
  const response = credential.response
  if (!(response instanceof AuthenticatorAssertionResponse)) {
    throw new WebAuthnClientError("浏览器返回了错误类型的安全凭据。")
  }
  return {
    id: credential.id,
    rawId: encodeBase64URL(credential.rawId),
    type: "public-key",
    authenticatorAttachment: credential.authenticatorAttachment as
      | "platform"
      | "cross-platform"
      | null,
    response: {
      clientDataJSON: encodeBase64URL(response.clientDataJSON),
      authenticatorData: encodeBase64URL(response.authenticatorData),
      signature: encodeBase64URL(response.signature),
      userHandle: response.userHandle
        ? encodeBase64URL(response.userHandle)
        : null,
    },
    clientExtensionResults:
      credential.getClientExtensionResults() as unknown as Record<
        string,
        unknown
      >,
  }
}

export async function createStaffCredential(
  options: CreationOptions
): Promise<CreationCredential> {
  assertWebAuthnAvailable()
  let credential: Credential | null
  try {
    credential = await navigator.credentials.create({
      publicKey: creationOptions(options),
    })
  } catch (error) {
    throw browserCeremonyError(error, "后台安全凭据登记")
  }
  if (!(credential instanceof PublicKeyCredential)) {
    throw new WebAuthnClientError("浏览器没有返回有效的登记凭据。")
  }
  const response = credential.response
  if (!(response instanceof AuthenticatorAttestationResponse)) {
    throw new WebAuthnClientError("浏览器返回了错误类型的登记凭据。")
  }
  return {
    id: credential.id,
    rawId: encodeBase64URL(credential.rawId),
    type: "public-key",
    authenticatorAttachment: credential.authenticatorAttachment as
      | "platform"
      | "cross-platform"
      | null,
    response: {
      clientDataJSON: encodeBase64URL(response.clientDataJSON),
      attestationObject: encodeBase64URL(response.attestationObject),
      transports: response.getTransports?.(),
    },
    clientExtensionResults:
      credential.getClientExtensionResults() as unknown as Record<
        string,
        unknown
      >,
  }
}

function requestOptions(
  options: RequestOptions
): PublicKeyCredentialRequestOptions {
  return {
    challenge: decodeBase64URL(options.challenge),
    timeout: options.timeout,
    rpId: options.rpId,
    allowCredentials: options.allowCredentials.map((credential) => ({
      type: "public-key",
      id: decodeBase64URL(credential.id),
      transports: credential.transports as AuthenticatorTransport[] | undefined,
    })),
    userVerification: options.userVerification,
    extensions: options.extensions as
      | AuthenticationExtensionsClientInputs
      | undefined,
  }
}

function creationOptions(
  options: CreationOptions
): PublicKeyCredentialCreationOptions {
  return {
    rp: options.rp,
    user: {
      ...options.user,
      id: decodeBase64URL(options.user.id),
    },
    challenge: decodeBase64URL(options.challenge),
    pubKeyCredParams: options.pubKeyCredParams,
    timeout: options.timeout,
    excludeCredentials: options.excludeCredentials?.map((credential) => ({
      type: "public-key",
      id: decodeBase64URL(credential.id),
      transports: credential.transports as AuthenticatorTransport[] | undefined,
    })),
    authenticatorSelection: options.authenticatorSelection,
    attestation: options.attestation,
    extensions: options.extensions as
      | AuthenticationExtensionsClientInputs
      | undefined,
  }
}

function assertWebAuthnAvailable() {
  if (
    typeof window === "undefined" ||
    !window.isSecureContext ||
    !("PublicKeyCredential" in window) ||
    !navigator.credentials
  ) {
    throw new WebAuthnClientError(
      "当前浏览器环境不支持 WebAuthn，请使用 HTTPS 或 localhost 后重试。"
    )
  }
}

function browserCeremonyError(error: unknown, action: string) {
  if (error instanceof DOMException && error.name === "NotAllowedError") {
    return new WebAuthnClientError(`${action}已取消或超时，请重新开始。`)
  }
  if (error instanceof DOMException && error.name === "InvalidStateError") {
    return new WebAuthnClientError("该安全凭据已经登记，请更换设备或凭据。")
  }
  return new WebAuthnClientError(`${action}未完成，请检查浏览器或设备。`)
}

function decodeBase64URL(value: string): Uint8Array<ArrayBuffer> {
  const base64 = value.replace(/-/g, "+").replace(/_/g, "/")
  const padding = "=".repeat((4 - (base64.length % 4)) % 4)
  const binary = window.atob(base64 + padding)
  return Uint8Array.from(binary, (character) => character.charCodeAt(0))
}

function encodeBase64URL(value: ArrayBuffer): string {
  const bytes = new Uint8Array(value)
  let binary = ""
  for (const byte of bytes) {
    binary += String.fromCharCode(byte)
  }
  return window
    .btoa(binary)
    .replace(/\+/g, "-")
    .replace(/\//g, "_")
    .replace(/=+$/g, "")
}

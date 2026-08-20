// PeerGo currently issues 32 random bytes and encodes them without Base64
// padding for invitation, verification, and recovery credentials.
export const opaqueTokenPattern = /^[A-Za-z0-9_-]{43}$/

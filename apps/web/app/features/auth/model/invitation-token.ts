// PeerGo issues 43-character base64url credentials.  During the Rousi
// cutover, an unexpired PtYes invitation can still carry its 64-character
// lowercase hexadecimal credential.
export const invitationTokenPattern = /^(?:[A-Za-z0-9_-]{43}|[0-9a-f]{64})$/

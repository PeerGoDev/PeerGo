import { InvitationsPage } from "~/features/invitation/pages/invitations-page"

export function meta() {
  return [{ title: "邀请 · PeerGo" }]
}

export default function AccountInvitationsRoute() {
  return <InvitationsPage />
}

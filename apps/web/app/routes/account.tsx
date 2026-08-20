import { AccountProfilePage } from "~/features/auth/pages/account-profile-page"

export function meta() {
  return [{ title: "个人资料 · PeerGo" }]
}

export default function AccountRoute() {
  return <AccountProfilePage />
}

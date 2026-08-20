import { AccountAccessPage } from "~/features/auth/pages/account-access-page"

export function meta() {
  return [{ title: "封禁记录与申诉 · PeerGo" }]
}

export default function RestrictionsRoute() {
  return <AccountAccessPage />
}

import { AccountSecurityPage } from "~/features/auth/pages/account-security-page"

export function meta() {
  return [{ title: "账户安全 · PeerGo" }]
}

export default function AccountSecurityRoute() {
  return <AccountSecurityPage />
}

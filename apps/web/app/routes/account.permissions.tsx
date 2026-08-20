import { PermissionsPage } from "~/features/authz/pages/permissions-page"

export function meta() {
  return [{ title: "我的权限 · PeerGo" }]
}

export default function AccountPermissionsRoute() {
  return <PermissionsPage />
}

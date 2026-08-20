import { StaffUsersPage } from "~/features/staff/pages/staff-users-page"

export function meta() {
  return [{ title: "用户 · 账户 · PeerGo Staff" }]
}

export default function StaffUsersRoute() {
  return <StaffUsersPage />
}

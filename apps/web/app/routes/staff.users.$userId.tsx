import { StaffUserDetailPage } from "~/features/staff/pages/staff-user-detail-page"

export function meta() {
  return [{ title: "用户详情 · 账户 · PeerGo Staff" }]
}

export default function StaffUserDetailRoute() {
  return <StaffUserDetailPage />
}

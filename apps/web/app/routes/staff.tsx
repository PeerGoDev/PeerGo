import { StaffAccessPage } from "~/features/staff/pages/staff-access-page"

export function meta() {
  return [{ title: "仪表盘 · PeerGo Staff" }]
}

export default function StaffRoute() {
  return <StaffAccessPage />
}

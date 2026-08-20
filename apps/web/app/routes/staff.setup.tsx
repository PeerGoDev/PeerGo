import { StaffSetupReadinessPage } from "~/features/staff/pages/staff-setup-readiness-page"

export function meta() {
  return [{ title: "上线检查 · PeerGo Staff" }]
}

export default function StaffSetupRoute() {
  return <StaffSetupReadinessPage />
}

import { StaffEnrollmentPage } from "~/features/staff/pages/staff-enrollment-page"

export function meta() {
  return [{ title: "安全凭据登记 · PeerGo Staff" }]
}

export default function StaffEnrollmentRoute() {
  return <StaffEnrollmentPage />
}

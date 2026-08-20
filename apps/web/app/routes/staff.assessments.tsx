import { StaffNewcomerAssessmentsPage } from "~/features/staff/pages/staff-newcomer-assessments-page"

export function meta() {
  return [{ title: "新人考核 · PeerGo Staff" }]
}

export default function StaffAssessmentsRoute() {
  return <StaffNewcomerAssessmentsPage />
}

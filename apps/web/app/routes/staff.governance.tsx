import { StaffGovernancePage } from "~/features/staff/pages/staff-governance-page"

export function meta() {
  return [{ title: "权限与任期 · PeerGo Staff" }]
}

export default function StaffGovernanceRoute() {
  return <StaffGovernancePage />
}

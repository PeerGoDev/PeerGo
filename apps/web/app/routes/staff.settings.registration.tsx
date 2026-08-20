import { StaffRegistrationSettingsPage } from "~/features/staff/pages/staff-registration-settings-page"

export function meta() {
  return [{ title: "注册设置 · PeerGo Staff" }]
}

export default function StaffSettingsRegistrationRoute() {
  return <StaffRegistrationSettingsPage />
}

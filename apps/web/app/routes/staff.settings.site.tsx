import { StaffSiteSettingsPage } from "~/features/staff/pages/staff-site-settings-page"

export function meta() {
  return [{ title: "站点与展示 · PeerGo Staff" }]
}

export default function StaffSettingsSiteRoute() {
  return <StaffSiteSettingsPage />
}

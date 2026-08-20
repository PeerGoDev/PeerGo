import { StaffStorageSettingsPage } from "~/features/staff/pages/staff-storage-settings-page"

export function meta() {
  return [{ title: "图片与存储 · PeerGo" }]
}

export default function StaffStorageSettingsRoute() {
  return <StaffStorageSettingsPage />
}

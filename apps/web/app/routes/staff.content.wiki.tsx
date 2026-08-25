import { StaffWikiPage } from "~/features/staff/pages/staff-wiki-page"

export function meta() {
  return [{ title: "Wiki 管理 · PeerGo 管理后台" }]
}

export default function StaffWikiRoute() {
  return <StaffWikiPage />
}

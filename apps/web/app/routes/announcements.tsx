import { AnnouncementListPage } from "~/features/announcement/pages/announcement-list-page"

export function meta() {
  return [{ title: "公告 · PeerGo" }]
}

export default function AnnouncementsRoute() {
  return <AnnouncementListPage />
}

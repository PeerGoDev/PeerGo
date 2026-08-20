import { AnnouncementDetailPage } from "~/features/announcement/pages/announcement-detail-page"

export function meta() {
  return [{ title: "公告详情 · PeerGo" }]
}

export default function AnnouncementDetailRoute() {
  return <AnnouncementDetailPage />
}

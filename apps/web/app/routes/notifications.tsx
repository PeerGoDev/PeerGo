import { NotificationPage } from "~/features/notification/pages/notification-page"

export function meta() {
  return [{ title: "消息 · PeerGo" }]
}

export default function NotificationsRoute() {
  return <NotificationPage />
}

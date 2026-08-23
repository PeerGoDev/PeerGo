import { SocialNotificationsPage } from "~/features/social/pages/social-notifications-page"

export function meta() {
  return [{ title: "动态圈通知 · PeerGo" }]
}

export default function SocialNotificationsRoute() {
  return <SocialNotificationsPage />
}

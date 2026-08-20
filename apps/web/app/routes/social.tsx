import { SocialFeedPage } from "~/features/social/pages/social-feed-page"

export function meta() {
  return [{ title: "动态圈 · PeerGo" }]
}

export default function SocialRoute() {
  return <SocialFeedPage />
}

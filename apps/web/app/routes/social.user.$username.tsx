import { UserSocialPostsPage } from "~/features/social/pages/user-social-posts-page"

export function meta() {
  return [{ title: "成员动态 · PeerGo" }]
}

export default function UserSocialPostsRoute() {
  return <UserSocialPostsPage />
}

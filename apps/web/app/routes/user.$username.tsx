import { UserProfilePage } from "~/features/user/pages/user-profile-page"

export function meta() {
  return [{ title: "用户资料 · PeerGo" }]
}

export default function UserProfileRoute() {
  return <UserProfilePage />
}

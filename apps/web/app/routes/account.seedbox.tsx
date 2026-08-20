import { MySeedboxPage } from "~/features/seedbox/pages/my-seedbox-page"

export function meta() {
  return [{ title: "盒子申报 · PeerGo" }]
}

export default function AccountSeedboxRoute() {
  return <MySeedboxPage />
}

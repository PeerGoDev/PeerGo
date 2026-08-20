import { HomePage } from "~/features/home/pages/home-page"

export function meta() {
  return [{ title: "首页 · PeerGo" }]
}

export default function HomeRoute() {
  return <HomePage />
}

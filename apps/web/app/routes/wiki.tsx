import { WikiListPage } from "~/features/wiki/pages/wiki-list-page"

export function meta() {
  return [{ title: "Wiki · PeerGo" }]
}

export default function WikiRoute() {
  return <WikiListPage />
}

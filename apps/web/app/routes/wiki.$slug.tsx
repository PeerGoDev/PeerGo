import { WikiDetailPage } from "~/features/wiki/pages/wiki-detail-page"

export function meta() {
  return [{ title: "Wiki · PeerGo" }]
}

export default function WikiDetailRoute() {
  return <WikiDetailPage />
}

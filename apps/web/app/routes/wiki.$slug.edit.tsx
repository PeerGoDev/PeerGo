import { WikiEditPage } from "~/features/wiki/pages/wiki-edit-page"

export function meta() {
  return [{ title: "编辑 Wiki · PeerGo" }]
}

export default function WikiEditRoute() {
  return <WikiEditPage />
}

import { MyTorrentSubmissionsPage } from "~/features/torrent/pages/my-torrent-submissions-page"

export function meta() {
  return [{ title: "我的发布 · PeerGo" }]
}

export default function AccountSubmissionsRoute() {
  return <MyTorrentSubmissionsPage />
}

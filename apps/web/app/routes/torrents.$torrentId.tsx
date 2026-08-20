import { TorrentDetailPage } from "~/features/torrent/pages/torrent-detail-page"

export function meta() {
  return [{ title: "种子详情 · PeerGo" }]
}

export default function TorrentDetailRoute() {
  return <TorrentDetailPage />
}

import { TorrentReseedPage } from "~/features/torrent/pages/torrent-reseed-page"

export function meta() {
  return [{ title: "补种专区 · PeerGo" }]
}

export default function TorrentReseedRoute() {
  return <TorrentReseedPage />
}

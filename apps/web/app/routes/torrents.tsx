import { TorrentCatalogPage } from "~/features/torrent/pages/torrent-catalog-page"

export function meta() {
  return [{ title: "种子 · PeerGo" }]
}

export default function TorrentsRoute() {
  return <TorrentCatalogPage />
}

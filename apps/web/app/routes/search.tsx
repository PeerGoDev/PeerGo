import { TorrentSearchPage } from "~/features/torrent/pages/torrent-search-page"

export function meta() {
  return [{ title: "搜索种子 · PeerGo" }]
}

export default function SearchRoute() {
  return <TorrentSearchPage />
}

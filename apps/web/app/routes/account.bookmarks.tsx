import { MyTorrentBookmarksPage } from "~/features/torrent/pages/my-torrent-bookmarks-page"

export function meta() {
  return [{ title: "我的收藏 · PeerGo" }]
}

export default function AccountBookmarksRoute() {
  return <MyTorrentBookmarksPage />
}

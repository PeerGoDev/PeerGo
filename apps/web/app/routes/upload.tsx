import { TorrentUploadPage } from "~/features/torrent/pages/torrent-upload-page"

export function meta() {
  return [{ title: "上传种子 · PeerGo" }]
}

export default function UploadRoute() {
  return <TorrentUploadPage />
}

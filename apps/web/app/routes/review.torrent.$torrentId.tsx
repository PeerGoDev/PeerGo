import { TorrentReviewDetailPage } from "~/features/review/pages/torrent-review-detail-page"

export function meta() {
  return [{ title: "参与种子审核 · PeerGo" }]
}

export default function TorrentReviewDetailRoute() {
  return <TorrentReviewDetailPage />
}

import { TorrentReviewQueuePage } from "~/features/review/pages/torrent-review-queue-page"

export function meta() {
  return [{ title: "种子审核 · PeerGo" }]
}

export default function ReviewQueueRoute() {
  return <TorrentReviewQueuePage />
}

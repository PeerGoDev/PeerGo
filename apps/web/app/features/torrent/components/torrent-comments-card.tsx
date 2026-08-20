import { torrentCommentTarget } from "~/features/social/api/comments.queries"
import { CommentThreadCard } from "~/features/social/components/comment-thread-card"

export function TorrentCommentsCard({ torrentId }: { torrentId: number }) {
  return (
    <CommentThreadCard
      target={torrentCommentTarget(torrentId)}
      description="交流资源内容与校验情况。"
      composerPlaceholder="写下你的评论..."
      appearance="torrent"
    />
  )
}

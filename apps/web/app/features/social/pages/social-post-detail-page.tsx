import { Link, useNavigate, useParams } from "react-router"
import { ArrowLeftIcon } from "lucide-react"

import { Alert, AlertDescription, AlertTitle } from "~/components/ui/alert"
import { Button } from "~/components/ui/button"
import { Skeleton } from "~/components/ui/skeleton"
import { useWebSession } from "~/features/auth/api/session.mutations"
import { postCommentTarget } from "~/features/social/api/comments.queries"
import { useSocialPost } from "~/features/social/api/posts.queries"
import { CommentThreadCard } from "~/features/social/components/comment-thread-card"
import { SocialPostCard } from "~/features/social/components/social-post-card"
import { PageLayout } from "~/shared/components/page-layout"

export function SocialPostDetailPage() {
  const { postId = "" } = useParams()
  const navigate = useNavigate()
  const session = useWebSession()
  const post = useSocialPost(postId)

  return (
    <PageLayout className="max-w-[704px] gap-6 lg:max-w-[720px]">
      <header className="flex items-center gap-4">
        <Button
          render={<Link to="/social" />}
          nativeButton={false}
          variant="ghost"
          size="icon"
          className="size-10"
          aria-label="返回动态圈"
        >
          <ArrowLeftIcon />
        </Button>
        <h1 className="font-heading text-3xl font-bold">动态详情</h1>
      </header>

      {post.isPending ? (
        <div className="rounded-lg border bg-card p-4">
          <div className="flex items-center gap-3">
            <Skeleton className="size-10 rounded-full" />
            <Skeleton className="h-4 w-32" />
          </div>
          <Skeleton className="mt-4 h-20 w-full" />
        </div>
      ) : post.isError || !post.data ? (
        <Alert variant="destructive">
          <AlertTitle>动态不可用</AlertTitle>
          <AlertDescription>
            该动态不存在、已删除或暂时无法读取。
          </AlertDescription>
        </Alert>
      ) : (
        <div className="flex flex-col gap-4">
          <SocialPostCard
            post={post.data}
            currentUserId={session.data?.user.id}
            csrfToken={session.data?.csrf_token}
            linkToDetail={false}
            onDeleted={() => navigate("/social", { replace: true })}
          />
          <CommentThreadCard
            target={postCommentTarget(post.data.id)}
            description="参与这条动态的讨论。"
            composerPlaceholder="写下你的评论..."
            appearance="social"
          />
        </div>
      )}
    </PageLayout>
  )
}

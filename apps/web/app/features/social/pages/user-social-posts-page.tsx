import { Link, useParams } from "react-router"
import {
  ArrowLeftIcon,
  LoaderCircleIcon,
  LogInIcon,
  MessageCircleIcon,
} from "lucide-react"

import { Alert, AlertDescription, AlertTitle } from "~/components/ui/alert"
import { Button, buttonVariants } from "~/components/ui/button"
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "~/components/ui/empty"
import { Skeleton } from "~/components/ui/skeleton"
import { useWebSession } from "~/features/auth/api/session.mutations"
import { useInfiniteSocialPosts } from "~/features/social/api/posts.queries"
import { SocialPostCard } from "~/features/social/components/social-post-card"
import { usePublicUserProfile } from "~/features/user/api/user.queries"
import { PageLayout } from "~/shared/components/page-layout"

const pageSize = 20

export function UserSocialPostsPage() {
  const { username = "" } = useParams()
  const session = useWebSession()
  const profile = usePublicUserProfile(username, Boolean(session.data?.user))
  const posts = useInfiniteSocialPosts(
    "newest",
    pageSize,
    username,
    Boolean(session.data?.user && profile.data)
  )
  const pages = posts.data?.pages ?? []
  const items = pages.flatMap((page) => page.items)
  const total = pages[0]?.total

  if (session.isPending || (session.data?.user && profile.isPending)) {
    return <UserPostsSkeleton />
  }

  if (!session.data?.user) {
    return (
      <PageLayout className="max-w-[704px] items-center justify-center lg:max-w-[720px]">
        <Empty className="min-h-64 border">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <LogInIcon />
            </EmptyMedia>
            <EmptyTitle>登录后查看成员动态</EmptyTitle>
            <EmptyDescription>成员动态只向已登录用户开放。</EmptyDescription>
          </EmptyHeader>
          <Link to="/login" className={buttonVariants()}>
            前往登录
          </Link>
        </Empty>
      </PageLayout>
    )
  }

  if (profile.isError || !profile.data) {
    return (
      <PageLayout className="max-w-[704px] gap-6 lg:max-w-[720px]">
        <BackToProfile username={username} />
        <Alert variant="destructive">
          <AlertTitle>成员资料暂时无法读取</AlertTitle>
          <AlertDescription>
            该成员不存在、已停用，或资料暂时不可访问。
          </AlertDescription>
        </Alert>
      </PageLayout>
    )
  }

  return (
    <PageLayout className="max-w-[704px] gap-6 lg:max-w-[720px]">
      <header className="flex items-center gap-4">
        <BackToProfile username={profile.data.username} />
        <div className="min-w-0">
          <h1 className="truncate font-heading text-xl font-bold">
            <Link
              to={`/user/${encodeURIComponent(profile.data.username)}`}
              className="hover:text-primary"
            >
              {profile.data.username}
            </Link>{" "}
            的动态
          </h1>
          <p className="text-sm text-muted-foreground">
            {total === undefined
              ? "正在读取动态…"
              : `共 ${total.toLocaleString("zh-CN")} 条`}
          </p>
        </div>
      </header>

      {posts.isPending ? (
        <UserPostsSkeletonContent />
      ) : posts.isError ? (
        <Alert variant="destructive">
          <AlertTitle>动态暂时不可用</AlertTitle>
          <AlertDescription>无法读取动态，请稍后重试。</AlertDescription>
        </Alert>
      ) : items.length === 0 ? (
        <Empty className="min-h-64 border">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <MessageCircleIcon />
            </EmptyMedia>
            <EmptyTitle>暂无动态</EmptyTitle>
            <EmptyDescription>这位成员还没有发布公开动态。</EmptyDescription>
          </EmptyHeader>
        </Empty>
      ) : (
        <div className="flex flex-col gap-4">
          {items.map((post) => (
            <SocialPostCard
              key={post.id}
              post={post}
              currentUserId={session.data?.user.id}
              csrfToken={session.data?.csrf_token}
            />
          ))}
        </div>
      )}

      {posts.hasNextPage ? (
        <div className="flex justify-center py-4">
          <Button
            variant="outline"
            disabled={posts.isFetchingNextPage}
            onClick={() => void posts.fetchNextPage()}
          >
            {posts.isFetchingNextPage ? (
              <LoaderCircleIcon
                className="animate-spin"
                data-icon="inline-start"
              />
            ) : null}
            {posts.isFetchingNextPage ? "加载中..." : "加载更多"}
          </Button>
        </div>
      ) : null}
    </PageLayout>
  )
}

function BackToProfile({ username }: { username: string }) {
  return (
    <Button
      variant="ghost"
      size="icon"
      nativeButton={false}
      render={
        <Link
          to={`/user/${encodeURIComponent(username)}`}
          aria-label="返回成员资料"
        />
      }
    >
      <ArrowLeftIcon />
    </Button>
  )
}

function UserPostsSkeleton() {
  return (
    <PageLayout
      className="max-w-[704px] gap-6 lg:max-w-[720px]"
      aria-busy="true"
    >
      <div className="flex items-center gap-4">
        <Skeleton className="size-9 rounded-md" />
        <div className="flex flex-col gap-2">
          <Skeleton className="h-7 w-48" />
          <Skeleton className="h-4 w-28" />
        </div>
      </div>
      <UserPostsSkeletonContent />
    </PageLayout>
  )
}

function UserPostsSkeletonContent() {
  return (
    <div className="flex flex-col gap-4" aria-busy="true">
      {Array.from({ length: 3 }, (_, index) => (
        <Skeleton key={index} className="h-44 rounded-lg" />
      ))}
    </div>
  )
}

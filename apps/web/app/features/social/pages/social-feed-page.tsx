import * as React from "react"
import { Link } from "react-router"
import {
  BellIcon,
  ChevronDownIcon,
  RefreshCwIcon,
  SparklesIcon,
  TrendingUpIcon,
  UsersIcon,
} from "lucide-react"

import { Alert, AlertDescription, AlertTitle } from "~/components/ui/alert"
import { Button } from "~/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "~/components/ui/card"
import { Badge } from "~/components/ui/badge"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "~/components/ui/dropdown-menu"
import { Skeleton } from "~/components/ui/skeleton"
import { useWebSession } from "~/features/auth/api/session.mutations"
import { useCapabilities } from "~/features/authz/api/capabilities.queries"
import { useMyNotificationSummary } from "~/features/notification/api/notifications.queries"
import {
  type SocialPostSort,
  useSocialPosts,
} from "~/features/social/api/posts.queries"
import { PostComposer } from "~/features/social/components/post-composer"
import { SocialPostCard } from "~/features/social/components/social-post-card"
import { ApiProblemError } from "~/shared/api/problem"
import { PageLayout } from "~/shared/components/page-layout"
import { cn } from "~/lib/utils"

const pageSize = 20

export function SocialFeedPage() {
  const [sort, setSort] = React.useState<SocialPostSort>("newest")
  const [offset, setOffset] = React.useState(0)
  const session = useWebSession()
  const capabilities = useCapabilities(session.data?.user.id)
  const posts = useSocialPosts(sort, pageSize, offset)
  const actions = React.useMemo(
    () => new Set(capabilities.data?.items.map((item) => item.action) ?? []),
    [capabilities.data?.items]
  )
  const canPost = actions.has("social.post.create.self")
  const canReadNotifications = actions.has("notification.read.self")
  const notificationSummary = useMyNotificationSummary(
    session.data?.user.id,
    canReadNotifications
  )
  const unreadNotifications = notificationSummary.data?.unread_count ?? 0

  React.useEffect(() => setOffset(0), [sort])

  return (
    <PageLayout className="max-w-[944px] gap-0">
      <header className="mb-6 flex items-center justify-between">
        <h1 className="font-heading text-3xl font-bold">动态圈</h1>
        <div className="flex items-center gap-2">
          {canReadNotifications ? (
            <Button
              render={<Link to="/notifications" />}
              nativeButton={false}
              variant="ghost"
              size="icon"
              className="relative"
              aria-label={
                unreadNotifications > 0
                  ? `通知，${unreadNotifications} 条未读`
                  : "通知"
              }
            >
              <BellIcon data-icon="inline-start" />
              {unreadNotifications > 0 ? (
                <Badge className="absolute -top-1 -right-1 h-5 min-w-5 justify-center rounded-full px-1 text-[10px]">
                  {unreadNotifications > 99 ? "99+" : unreadNotifications}
                </Badge>
              ) : null}
            </Button>
          ) : null}
          <Button
            variant="ghost"
            size="icon"
            aria-label="刷新动态"
            onClick={() => void posts.refetch()}
            disabled={posts.isFetching}
          >
            <RefreshCwIcon
              className={cn(posts.isFetching && "animate-spin")}
              data-icon="inline-start"
            />
          </Button>
        </div>
      </header>

      <div className="grid grid-cols-1 gap-6 lg:grid-cols-3">
        <div className="flex flex-col gap-4 lg:col-span-2">
          {session.data ? (
            <PostComposer
              csrfToken={session.data.csrf_token}
              canPost={canPost}
            />
          ) : null}

          <div
            className="grid h-10 grid-cols-3 rounded-lg bg-muted p-1 text-sm"
            role="tablist"
            aria-label="动态流筛选"
          >
            <button
              type="button"
              role="tab"
              aria-disabled="true"
              aria-selected="false"
              title="关注 Feed 将在关注关系上线后开放"
              className="flex items-center justify-center gap-2 rounded-md text-muted-foreground"
            >
              <UsersIcon className="size-4" />
              关注
            </button>
            <button
              type="button"
              role="tab"
              aria-selected="true"
              className="flex items-center justify-center gap-2 rounded-md bg-background font-medium text-foreground shadow-sm"
            >
              <SparklesIcon className="size-4" />
              发现
            </button>
            <button
              type="button"
              role="tab"
              aria-disabled="true"
              aria-selected="false"
              title="热门 Feed 将在互动排序上线后开放"
              className="flex items-center justify-center gap-2 rounded-md text-muted-foreground"
            >
              <TrendingUpIcon className="size-4" />
              热门
            </button>
          </div>

          <div
            className="flex flex-col gap-4"
            role="tabpanel"
            aria-label="发现"
          >
            <div className="flex justify-end">
              <DropdownMenu>
                <DropdownMenuTrigger
                  render={
                    <Button
                      variant="ghost"
                      size="sm"
                      className="gap-1 text-muted-foreground"
                    />
                  }
                >
                  {sort === "newest" ? "最新" : "最早"}
                  <ChevronDownIcon data-icon="inline-end" />
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end" className="w-24">
                  <DropdownMenuItem onClick={() => setSort("newest")}>
                    最新
                  </DropdownMenuItem>
                  <DropdownMenuItem onClick={() => setSort("oldest")}>
                    最早
                  </DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenu>
            </div>

            {posts.isPending ? (
              <FeedSkeleton />
            ) : posts.isError ? (
              <Alert variant="destructive">
                <AlertTitle>动态暂时不可用</AlertTitle>
                <AlertDescription>
                  {posts.error instanceof ApiProblemError
                    ? posts.error.message
                    : "无法读取动态，请稍后重试。"}
                </AlertDescription>
              </Alert>
            ) : posts.data.items.length === 0 ? (
              <Card>
                <CardContent className="py-12 text-center text-muted-foreground">
                  暂无动态
                </CardContent>
              </Card>
            ) : (
              <div className="flex flex-col gap-4">
                {posts.data.items.map((post) => (
                  <SocialPostCard
                    key={post.id}
                    post={post}
                    currentUserId={session.data?.user.id}
                    csrfToken={session.data?.csrf_token}
                  />
                ))}
              </div>
            )}

            {posts.data && posts.data.total > pageSize ? (
              <div className="flex items-center justify-center gap-2 py-2">
                <Button
                  variant="outline"
                  size="sm"
                  disabled={offset === 0}
                  onClick={() => setOffset(Math.max(0, offset - pageSize))}
                >
                  上一页
                </Button>
                <span className="text-xs text-muted-foreground">
                  {Math.floor(offset / pageSize) + 1} /{" "}
                  {Math.ceil(posts.data.total / pageSize)}
                </span>
                <Button
                  variant="outline"
                  size="sm"
                  disabled={offset + pageSize >= posts.data.total}
                  onClick={() => setOffset(offset + pageSize)}
                >
                  下一页
                </Button>
              </div>
            ) : null}
          </div>
        </div>

        <aside className="flex flex-col gap-4">
          <Card>
            <CardHeader className="pb-3">
              <CardTitle className="flex items-center gap-2 text-base">
                <TrendingUpIcon />
                热门话题
              </CardTitle>
            </CardHeader>
            <CardContent>
              <p className="text-sm text-muted-foreground">暂无热门话题</p>
            </CardContent>
          </Card>
        </aside>
      </div>
    </PageLayout>
  )
}

function FeedSkeleton() {
  return (
    <div className="flex flex-col gap-4">
      {Array.from({ length: 3 }).map((_, index) => (
        <Card key={index}>
          <CardContent className="flex flex-col gap-3 pt-4">
            <div className="flex items-center gap-3">
              <Skeleton className="size-10 rounded-full" />
              <div className="flex flex-col gap-1.5">
                <Skeleton className="h-4 w-24" />
                <Skeleton className="h-3 w-16" />
              </div>
            </div>
            <Skeleton className="h-4 w-full" />
            <Skeleton className="h-4 w-4/5" />
          </CardContent>
        </Card>
      ))}
    </div>
  )
}

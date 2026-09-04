import * as React from "react"
import { Link } from "react-router"
import {
  BellIcon,
  ChevronDownIcon,
  RefreshCwIcon,
  SparklesIcon,
  TrendingUpIcon,
  UsersIcon,
  LayoutGridIcon,
  CheckIcon,
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
import {
  type SocialPostSort,
  type SocialFeedKind,
  useSocialCommunityOverview,
  useSocialPosts,
} from "~/features/social/api/posts.queries"
import { useSocialNotificationSummary } from "~/features/social/api/social-notifications.queries"
import { PostComposer } from "~/features/social/components/post-composer"
import { SocialPostCard } from "~/features/social/components/social-post-card"
import { ApiProblemError } from "~/shared/api/problem"
import { PageLayout } from "~/shared/components/page-layout"
import { cn } from "~/lib/utils"

const pageSize = 20

export function SocialFeedPage() {
  const [sort, setSort] = React.useState<SocialPostSort>("newest")
  const [feed, setFeed] = React.useState<SocialFeedKind>("discover")
  const [boardId, setBoardId] = React.useState("")
  const [featuredOnly, setFeaturedOnly] = React.useState(false)
  const [topic, setTopic] = React.useState("")
  const [offset, setOffset] = React.useState(0)
  const session = useWebSession()
  const capabilities = useCapabilities(session.data?.user.id)
  const overview = useSocialCommunityOverview()
  const posts = useSocialPosts(sort, pageSize, offset, {
    feed,
    boardId: boardId || undefined,
    featuredOnly,
    topic: topic || undefined,
  })
  const actions = React.useMemo(
    () => new Set(capabilities.data?.items.map((item) => item.action) ?? []),
    [capabilities.data?.items]
  )
  const canPost = actions.has("social.post.create.self")
  const canPostRestrictedBoards = actions.has(
    "social.post.create.restricted.self"
  )
  const canReadNotifications = actions.has("notification.read.self")
  const notificationSummary = useSocialNotificationSummary(
    session.data?.user.id,
    canReadNotifications
  )
  const unreadNotifications = notificationSummary.data?.unread_count ?? 0

  React.useEffect(
    () => setOffset(0),
    [sort, feed, boardId, featuredOnly, topic]
  )

  return (
    <PageLayout className="max-w-[944px] gap-0">
      <header className="mb-6 flex items-center justify-between">
        <h1 className="font-heading text-3xl font-bold">动态圈</h1>
        <div className="flex items-center gap-2">
          {canReadNotifications ? (
            <Button
              render={<Link to="/social/notifications" />}
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
              canPostRestrictedBoards={canPostRestrictedBoards}
              boards={overview.data?.boards ?? []}
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
              aria-selected={feed === "following"}
              onClick={() => {
                setFeed("following")
                if (sort === "hot") setSort("newest")
              }}
              className={cn(
                "flex items-center justify-center gap-2 rounded-md",
                feed === "following"
                  ? "bg-background font-medium text-foreground shadow-sm"
                  : "text-muted-foreground"
              )}
            >
              <UsersIcon className="size-4" />
              关注
            </button>
            <button
              type="button"
              role="tab"
              aria-selected={feed === "discover" && sort !== "hot"}
              onClick={() => {
                setFeed("discover")
                if (sort === "hot") setSort("newest")
              }}
              className={cn(
                "flex items-center justify-center gap-2 rounded-md",
                feed === "discover" && sort !== "hot"
                  ? "bg-background font-medium text-foreground shadow-sm"
                  : "text-muted-foreground"
              )}
            >
              <SparklesIcon className="size-4" />
              发现
            </button>
            <button
              type="button"
              role="tab"
              aria-selected={sort === "hot"}
              onClick={() => {
                setFeed("discover")
                setSort("hot")
              }}
              className={cn(
                "flex items-center justify-center gap-2 rounded-md",
                sort === "hot"
                  ? "bg-background font-medium text-foreground shadow-sm"
                  : "text-muted-foreground"
              )}
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
            <div className="flex items-center justify-between gap-2">
              <div className="flex items-center gap-2">
                <DropdownMenu>
                  <DropdownMenuTrigger
                    render={<Button variant="outline" size="sm" />}
                  >
                    <LayoutGridIcon data-icon="inline-start" />
                    {overview.data?.boards.find((board) => board.id === boardId)
                      ?.name ?? "全部板块"}
                    <ChevronDownIcon data-icon="inline-end" />
                  </DropdownMenuTrigger>
                  <DropdownMenuContent align="start" className="w-40">
                    <DropdownMenuItem onClick={() => setBoardId("")}>
                      {!boardId ? <CheckIcon /> : <span className="size-4" />}
                      全部板块
                    </DropdownMenuItem>
                    {overview.data?.boards.map((board) => (
                      <DropdownMenuItem
                        key={board.id}
                        onClick={() => setBoardId(board.id)}
                      >
                        {boardId === board.id ? (
                          <CheckIcon />
                        ) : (
                          <span className="size-4" />
                        )}
                        {board.name}
                      </DropdownMenuItem>
                    ))}
                  </DropdownMenuContent>
                </DropdownMenu>
                <Button
                  variant={featuredOnly ? "secondary" : "ghost"}
                  size="sm"
                  onClick={() => setFeaturedOnly((value) => !value)}
                >
                  只看精华
                </Button>
              </div>
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
                  {sort === "newest"
                    ? "最新"
                    : sort === "oldest"
                      ? "最早"
                      : "热门"}
                  <ChevronDownIcon data-icon="inline-end" />
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end" className="w-24">
                  <DropdownMenuItem onClick={() => setSort("newest")}>
                    最新
                  </DropdownMenuItem>
                  <DropdownMenuItem onClick={() => setSort("oldest")}>
                    最早
                  </DropdownMenuItem>
                  <DropdownMenuItem onClick={() => setSort("hot")}>
                    热门
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
              <div className="flex flex-col gap-2">
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
                <LayoutGridIcon />
                社区板块
              </CardTitle>
            </CardHeader>
            <CardContent className="space-y-1">
              {overview.data?.boards.map((board) => (
                <button
                  key={board.id}
                  type="button"
                  onClick={() =>
                    setBoardId(board.id === boardId ? "" : board.id)
                  }
                  className={cn(
                    "flex w-full items-center justify-between rounded-lg px-3 py-2.5 text-sm hover:bg-muted",
                    board.id === boardId && "bg-muted font-medium"
                  )}
                >
                  <span>{board.name}</span>
                  <span className="text-xs text-muted-foreground">
                    {board.post_count.toLocaleString()}
                  </span>
                </button>
              ))}
            </CardContent>
          </Card>
          <Card>
            <CardHeader className="pb-3">
              <CardTitle className="flex items-center gap-2 text-base">
                <TrendingUpIcon />
                热门话题
              </CardTitle>
            </CardHeader>
            <CardContent>
              {overview.data?.hot_topics.length ? (
                <div className="space-y-1">
                  {overview.data.hot_topics.map((item) => (
                    <button
                      key={item.name}
                      type="button"
                      onClick={() =>
                        setTopic(item.name === topic ? "" : item.name)
                      }
                      className={cn(
                        "flex w-full items-center justify-between rounded-lg px-3 py-2.5 text-sm hover:bg-muted",
                        item.name === topic && "bg-muted font-medium"
                      )}
                    >
                      <span>#{item.name}</span>
                      <span className="text-xs text-muted-foreground">
                        {item.post_count}
                      </span>
                    </button>
                  ))}
                </div>
              ) : (
                <p className="text-sm text-muted-foreground">暂无热门话题</p>
              )}
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

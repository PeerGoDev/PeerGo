import { Link } from "react-router"
import { MessageCircleIcon, RefreshCwIcon } from "lucide-react"

import { Button } from "~/components/ui/button"
import {
  Card,
  CardAction,
  CardContent,
  CardHeader,
  CardTitle,
} from "~/components/ui/card"
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "~/components/ui/empty"
import { Skeleton } from "~/components/ui/skeleton"
import type { SocialPostPage } from "~/features/social/api/posts.queries"
import { SocialPostCard } from "~/features/social/components/social-post-card"

export function UserRecentPostsCard({
  username,
  page,
  loading,
  error,
  currentUserId,
  csrfToken,
  offset,
  pageSize,
  onRetry,
  onPageChange,
}: {
  username: string
  page?: SocialPostPage
  loading: boolean
  error: boolean
  currentUserId?: string
  csrfToken?: string
  offset: number
  pageSize: number
  onRetry: () => void
  onPageChange: (offset: number) => void
}) {
  return (
    <Card className="gap-0 rounded-lg border py-0 shadow-sm ring-0">
      <CardHeader className="px-6 pt-6 pb-2">
        <CardTitle className="flex items-center gap-2 text-base leading-6 font-semibold">
          <MessageCircleIcon className="size-4" />
          <h2>最新动态</h2>
          {page ? (
            <span className="text-sm font-normal text-muted-foreground">
              ({page.total.toLocaleString("zh-CN")} 条)
            </span>
          ) : null}
        </CardTitle>
        <CardAction>
          <Button
            variant="ghost"
            size="sm"
            nativeButton={false}
            render={
              <Link
                to={`/social/user/${encodeURIComponent(username)}`}
                aria-label={`查看 ${username} 的全部动态`}
              />
            }
          >
            查看全部
          </Button>
        </CardAction>
      </CardHeader>
      <CardContent className="flex flex-col gap-4 px-6 pb-6">
        {loading ? (
          <RecentPostsSkeleton />
        ) : error ? (
          <Empty className="min-h-36 border-0 py-6">
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <MessageCircleIcon />
              </EmptyMedia>
              <EmptyTitle>动态暂时无法读取</EmptyTitle>
              <EmptyDescription>请稍后重试。</EmptyDescription>
            </EmptyHeader>
            <EmptyContent>
              <Button
                type="button"
                variant="outline"
                size="sm"
                onClick={onRetry}
              >
                <RefreshCwIcon data-icon="inline-start" />
                重试
              </Button>
            </EmptyContent>
          </Empty>
        ) : !page || page.items.length === 0 ? (
          <Empty className="min-h-36 border-0 py-6">
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <MessageCircleIcon />
              </EmptyMedia>
              <EmptyTitle>暂无动态</EmptyTitle>
              <EmptyDescription>这位成员还没有发布公开动态。</EmptyDescription>
            </EmptyHeader>
          </Empty>
        ) : (
          <>
            {page.items.map((post) => (
              <SocialPostCard
                key={post.id}
                post={post}
                currentUserId={currentUserId}
                csrfToken={csrfToken}
                compact
              />
            ))}
            {page.total > pageSize ? (
              <nav
                className="flex items-center justify-end gap-2 pt-2"
                aria-label="资料页动态分页"
              >
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  disabled={offset === 0}
                  onClick={() => onPageChange(Math.max(0, offset - pageSize))}
                >
                  上一页
                </Button>
                <span className="text-sm text-muted-foreground">
                  {Math.floor(offset / pageSize) + 1} /{" "}
                  {Math.ceil(page.total / pageSize)}
                </span>
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  disabled={offset + pageSize >= page.total}
                  onClick={() => onPageChange(offset + pageSize)}
                >
                  下一页
                </Button>
              </nav>
            ) : null}
          </>
        )}
      </CardContent>
    </Card>
  )
}

function RecentPostsSkeleton() {
  return (
    <div className="flex flex-col gap-4" aria-busy="true">
      {Array.from({ length: 2 }, (_, index) => (
        <div key={index} className="flex flex-col gap-3 rounded-lg border p-4">
          <div className="flex items-center gap-3">
            <Skeleton className="size-10 rounded-full" />
            <div className="flex flex-col gap-1.5">
              <Skeleton className="h-4 w-24" />
              <Skeleton className="h-3 w-16" />
            </div>
          </div>
          <Skeleton className="h-4 w-full" />
          <Skeleton className="h-4 w-4/5" />
        </div>
      ))}
    </div>
  )
}

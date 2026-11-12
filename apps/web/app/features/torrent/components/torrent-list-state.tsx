import { SearchXIcon, TriangleAlertIcon } from "lucide-react"

import {
  Alert,
  AlertAction,
  AlertDescription,
  AlertTitle,
} from "~/components/ui/alert"
import { Button } from "~/components/ui/button"
import { Card } from "~/components/ui/card"
import { Skeleton } from "~/components/ui/skeleton"
import { requestErrorDescription } from "~/shared/api/problem"

export function TorrentListSkeleton() {
  return (
    <Card className="gap-0 py-0" aria-label="正在加载种子" aria-busy="true">
      <span className="sr-only">正在读取最新内容。</span>
      <div className="h-10 border-b bg-muted" aria-hidden="true" />
      {Array.from({ length: 6 }, (_, index) => (
        <div
          key={index}
          className="flex h-14 items-center gap-3 border-b px-4 last:border-b-0"
          aria-hidden="true"
        >
          <Skeleton className="h-8 w-12 shrink-0" />
          <div className="flex min-w-0 flex-1 flex-col gap-2">
            <Skeleton className="h-3 w-3/5" />
            <Skeleton className="h-2.5 w-2/5" />
          </div>
          <Skeleton className="hidden h-3 w-24 sm:block" />
        </div>
      ))}
    </Card>
  )
}

export function TorrentListError({
  error,
  retry,
}: {
  error: unknown
  retry: () => void
}) {
  return (
    <Alert variant="destructive">
      <TriangleAlertIcon />
      <AlertTitle>最新种子暂时无法读取</AlertTitle>
      <AlertDescription>
        {requestErrorDescription(error, "种子目录请求未能完成，请稍后再试。")}
      </AlertDescription>
      <AlertAction>
        <Button variant="outline" size="sm" onClick={retry}>
          重试
        </Button>
      </AlertAction>
    </Alert>
  )
}

export function TorrentListEmpty({ query }: { query: string }) {
  return (
    <Card className="items-center gap-3 py-12 text-center">
      <span className="flex size-10 items-center justify-center rounded-lg border bg-muted text-faint">
        <SearchXIcon className="size-5" />
      </span>
      <p className="text-sm text-muted-foreground">
        {query ? "没有找到符合条件的种子" : "暂无种子"}
      </p>
    </Card>
  )
}

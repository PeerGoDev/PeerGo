import { TriangleAlertIcon } from "lucide-react"

import {
  Alert,
  AlertAction,
  AlertDescription,
  AlertTitle,
} from "~/components/ui/alert"
import { Button } from "~/components/ui/button"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "~/components/ui/card"
import { Skeleton } from "~/components/ui/skeleton"
import { requestErrorDescription } from "~/shared/api/problem"

export function TorrentListSkeleton() {
  return (
    <Card aria-label="正在加载种子" aria-busy="true">
      <CardHeader className="sr-only">
        <CardTitle>正在加载种子</CardTitle>
        <CardDescription>正在读取最新内容。</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        {Array.from({ length: 6 }, (_, index) => (
          <div key={index} className="flex h-12 items-center gap-3">
            <Skeleton className="size-10 shrink-0" />
            <div className="flex min-w-0 flex-1 flex-col gap-2">
              <Skeleton className="h-3 w-3/5" />
              <Skeleton className="h-2.5 w-2/5" />
            </div>
            <Skeleton className="hidden h-3 w-24 sm:block" />
          </div>
        ))}
      </CardContent>
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
    <Card className="py-0">
      <CardContent className="py-12 text-center text-muted-foreground">
        {query ? "没有找到符合条件的种子" : "暂无种子"}
      </CardContent>
    </Card>
  )
}

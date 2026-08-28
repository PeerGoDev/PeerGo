import { useQuery } from "@tanstack/react-query"
import { CircleAlertIcon, NetworkIcon, RefreshCwIcon } from "lucide-react"

import { Alert, AlertDescription, AlertTitle } from "~/components/ui/alert"
import { Badge } from "~/components/ui/badge"
import { Button } from "~/components/ui/button"
import { Skeleton } from "~/components/ui/skeleton"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "~/components/ui/table"
import { managedUserNetworkHistoryQueryOptions } from "~/features/staff/api/user-administration.queries"
import { formatDateTime } from "~/shared/formatters/date-time"
import { formatInteger } from "~/shared/formatters/integer"

export function ManagedUserNetworkHistoryCard({ userId }: { userId: string }) {
  const history = useQuery(managedUserNetworkHistoryQueryOptions(userId))

  return (
    <section className="flex flex-col gap-3">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h2 className="flex items-center gap-2 font-heading font-medium">
            <NetworkIcon className="size-4" aria-hidden="true" />
            IP 历史
          </h2>
          <p className="text-xs text-muted-foreground">
            只保留登录成功后的聚合记录，不保存 User-Agent 和逐请求日志。
          </p>
        </div>
        {history.data ? (
          <Badge variant="secondary">
            保留 {history.data.retention_days} 天 · 最多{" "}
            {history.data.maximum_items} 条
          </Badge>
        ) : null}
      </div>

      {history.isPending ? (
        <div className="flex flex-col gap-2" aria-label="正在加载 IP 历史">
          <Skeleton className="h-10 w-full" />
          <Skeleton className="h-16 w-full" />
        </div>
      ) : history.isError || !history.data ? (
        <Alert variant="destructive">
          <CircleAlertIcon />
          <AlertTitle>IP 历史暂时无法读取</AlertTitle>
          <AlertDescription>
            该数据受单独权限保护，请刷新权限或稍后重试。
          </AlertDescription>
          <Button
            type="button"
            size="sm"
            variant="outline"
            className="mt-2"
            onClick={() => void history.refetch()}
          >
            <RefreshCwIcon data-icon="inline-start" />
            重试
          </Button>
        </Alert>
      ) : history.data.items.length === 0 ? (
        <Alert>
          <NetworkIcon />
          <AlertTitle>暂无登录 IP</AlertTitle>
          <AlertDescription>
            迁移保留窗口内没有有效地址；该用户下次登录成功后会开始聚合记录。
          </AlertDescription>
        </Alert>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>IP 地址</TableHead>
              <TableHead>首次出现</TableHead>
              <TableHead>最后出现</TableHead>
              <TableHead className="text-right">登录次数</TableHead>
              <TableHead className="text-right">关联用户</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {history.data.items.map((item) => (
              <TableRow key={item.address}>
                <TableCell className="font-mono text-xs">
                  {item.address}
                </TableCell>
                <TableCell className="text-xs text-muted-foreground">
                  {formatDateTime(item.first_seen_at)}
                </TableCell>
                <TableCell className="text-xs">
                  {formatDateTime(item.last_seen_at)}
                </TableCell>
                <TableCell className="text-right tabular-nums">
                  {formatInteger(item.seen_count)}
                </TableCell>
                <TableCell className="text-right tabular-nums">
                  {formatInteger(item.related_user_count)}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
    </section>
  )
}

import * as React from "react"
import { useQuery } from "@tanstack/react-query"
import {
  CircleAlertIcon,
  InboxIcon,
  RefreshCwIcon,
  ScaleIcon,
} from "lucide-react"

import { Alert, AlertDescription, AlertTitle } from "~/components/ui/alert"
import { Badge } from "~/components/ui/badge"
import { Button } from "~/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "~/components/ui/card"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "~/components/ui/select"
import { Skeleton } from "~/components/ui/skeleton"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "~/components/ui/table"
import {
  type AccountAccessAppeal,
  type AccountAccessAppealFilter,
  accountAccessAppealListQueryOptions,
} from "~/features/staff/api/account-access-appeal-administration.queries"
import { AccountAccessAppealDecisionDialog } from "~/features/staff/components/account-access-appeal-decision-dialog"

export function AccountAccessAppealQueue({
  csrfToken,
  canDecide,
}: {
  csrfToken: string
  canDecide: boolean
}) {
  const [filter, setFilter] =
    React.useState<AccountAccessAppealFilter>("pending")
  const [selected, setSelected] = React.useState<AccountAccessAppeal | null>(
    null
  )
  const appeals = useQuery(accountAccessAppealListQueryOptions(filter))

  return (
    <Card className="gap-0 py-0">
      <CardHeader className="flex-row items-center justify-between gap-3 p-6 pb-3">
        <CardTitle className="flex items-center gap-2 text-lg">
          <ScaleIcon className="size-5" />
          账户与下载限制申诉
          {appeals.data ? (
            <Badge variant="secondary">{appeals.data.total}</Badge>
          ) : null}
        </CardTitle>
        <div className="flex items-center gap-2">
          <Select
            value={filter}
            onValueChange={(value) =>
              setFilter(value as AccountAccessAppealFilter)
            }
          >
            <SelectTrigger size="xs" aria-label="筛选账户限制申诉">
              <SelectValue>{appealFilterLabel(filter)}</SelectValue>
            </SelectTrigger>
            <SelectContent align="end">
              <SelectItem value="pending">待处理</SelectItem>
              <SelectItem value="resolved">已处理</SelectItem>
              <SelectItem value="all">全部</SelectItem>
            </SelectContent>
          </Select>
          <Button
            variant="outline"
            size="icon-sm"
            onClick={() => void appeals.refetch()}
            disabled={appeals.isFetching}
            aria-label="刷新账户限制申诉"
          >
            <RefreshCwIcon />
          </Button>
        </div>
      </CardHeader>
      <CardContent className="p-6 pt-0">
        {appeals.isPending ? (
          <Skeleton className="h-36 w-full" aria-label="正在读取账户申诉" />
        ) : appeals.isError || !appeals.data ? (
          <Alert variant="destructive">
            <CircleAlertIcon />
            <AlertTitle>账户申诉暂时无法读取</AlertTitle>
            <AlertDescription>请检查后台登录状态后重试。</AlertDescription>
          </Alert>
        ) : appeals.data.items.length === 0 ? (
          <div className="flex min-h-28 flex-col items-center justify-center gap-2 rounded-md border border-dashed text-sm text-muted-foreground">
            <InboxIcon className="size-5" />
            {filter === "pending" ? "当前没有待处理申诉" : "没有符合筛选的申诉"}
          </div>
        ) : (
          <div className="overflow-x-auto rounded-md border">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>用户</TableHead>
                  <TableHead>限制来源</TableHead>
                  <TableHead className="min-w-64">申诉说明</TableHead>
                  <TableHead>提交时间</TableHead>
                  <TableHead>状态</TableHead>
                  <TableHead className="text-right">操作</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {appeals.data.items.map((appeal) => (
                  <TableRow key={appeal.id}>
                    <TableCell>
                      <div className="font-medium">{appeal.username}</div>
                      <div className="text-xs text-muted-foreground">
                        ID {appeal.user_numeric_id}
                      </div>
                    </TableCell>
                    <TableCell>
                      <div>
                        {appealSourceLabel(appeal.restriction.source_kind)}
                      </div>
                      <div className="max-w-48 truncate text-xs text-muted-foreground">
                        {appeal.restriction.reason_summary}
                      </div>
                    </TableCell>
                    <TableCell>
                      <p className="line-clamp-2 text-sm leading-5">
                        {appeal.statement}
                      </p>
                    </TableCell>
                    <TableCell className="whitespace-nowrap text-muted-foreground">
                      {formatDateTime(appeal.created_at)}
                    </TableCell>
                    <TableCell>
                      <AppealStatusBadge status={appeal.status} />
                    </TableCell>
                    <TableCell className="text-right">
                      {appeal.status === "pending" && canDecide ? (
                        <Button
                          size="sm"
                          variant="outline"
                          onClick={() => setSelected(appeal)}
                        >
                          处理
                        </Button>
                      ) : (
                        <span className="text-xs text-muted-foreground">
                          {appeal.response || "—"}
                        </span>
                      )}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        )}
      </CardContent>
      <AccountAccessAppealDecisionDialog
        appeal={selected}
        csrfToken={csrfToken}
        onOpenChange={(open) => {
          if (!open) setSelected(null)
        }}
      />
    </Card>
  )
}

function AppealStatusBadge({ status }: { status: string }) {
  const labels: Record<string, string> = {
    pending: "待处理",
    approved: "已批准",
    rejected: "已驳回",
    source_resolved: "来源已解除",
  }
  return <Badge variant="outline">{labels[status] ?? status}</Badge>
}

function appealSourceLabel(source: string) {
  if (source === "disabled_account") return "账户封禁"
  if (source === "manual_download_restriction") {
    return "旧站 / 人工下载限制"
  }
  return "临时访问限制"
}

function appealFilterLabel(filter: AccountAccessAppealFilter) {
  return filter === "pending"
    ? "待处理"
    : filter === "resolved"
      ? "已处理"
      : "全部"
}

function formatDateTime(value: string) {
  return new Intl.DateTimeFormat("zh-CN", {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  }).format(new Date(value))
}

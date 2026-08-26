import { Link } from "react-router"
import { CircleAlertIcon, GiftIcon, RefreshCwIcon } from "lucide-react"

import { Alert, AlertDescription, AlertTitle } from "~/components/ui/alert"
import { Badge } from "~/components/ui/badge"
import { Button } from "~/components/ui/button"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "~/components/ui/card"
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
  type ContentTipRecord,
  useContentTipOverview,
} from "~/features/economy/api/content-tips.queries"
import { requestErrorDescription } from "~/shared/api/problem"
import { formatCompactDateTime } from "~/shared/formatters/date-time"
import { formatInteger } from "~/shared/formatters/integer"

export function ContentTipHistoryCard({ userId }: { userId: string }) {
  const overview = useContentTipOverview(userId)
  if (overview.isPending) return <Skeleton className="h-72 rounded-lg" />
  if (overview.isError || !overview.data) {
    return (
      <Card>
        <CardContent className="pt-6">
          <Alert variant="destructive">
            <CircleAlertIcon />
            <AlertTitle>内容打赏记录暂时无法读取</AlertTitle>
            <AlertDescription>
              {requestErrorDescription(overview.error, "请稍后重试。")}
            </AlertDescription>
          </Alert>
          <Button
            variant="outline"
            size="sm"
            className="mt-3"
            onClick={() => void overview.refetch()}
          >
            <RefreshCwIcon data-icon="inline-start" />
            重试
          </Button>
        </CardContent>
      </Card>
    )
  }

  const { policy, history, remaining_today } = overview.data
  return (
    <Card className="gap-0 py-0">
      <CardHeader className="border-b p-6 pb-4">
        <CardTitle className="flex items-center gap-2">
          <GiftIcon data-icon="inline-start" />
          内容打赏
        </CardTitle>
        <CardDescription>
          种子、动态和评论共用同一套整数魔力值规则。
        </CardDescription>
        <div className="flex flex-wrap gap-2 text-xs text-muted-foreground">
          <Badge variant={policy.settings.enabled ? "outline" : "secondary"}>
            {policy.settings.enabled ? "已开放" : "当前关闭"}
          </Badge>
          <span>今日剩余 {formatInteger(remaining_today)}</span>
          <span>
            单笔 {formatInteger(policy.settings.minimum_amount)}–
            {formatInteger(policy.settings.maximum_amount)}
          </span>
        </div>
      </CardHeader>
      <CardContent className="p-0">
        {history.length === 0 ? (
          <div className="p-8 text-center text-sm text-muted-foreground">
            还没有内容打赏记录。
          </div>
        ) : (
          <Table containerClassName="px-3">
            <TableHeader>
              <TableRow>
                <TableHead>时间</TableHead>
                <TableHead>内容</TableHead>
                <TableHead>成员</TableHead>
                <TableHead className="text-right">变动</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {history.map((tip) => {
                const received = tip.direction === "received"
                return (
                  <TableRow key={tip.id}>
                    <TableCell className="text-xs whitespace-nowrap text-muted-foreground">
                      {formatCompactDateTime(tip.occurred_at)}
                    </TableCell>
                    <TableCell className="max-w-72 truncate">
                      <TipTargetLink tip={tip} />
                    </TableCell>
                    <TableCell>
                      <div>{tip.counterparty.display_name}</div>
                      <div className="text-xs text-muted-foreground">
                        #{tip.counterparty.numeric_id} ·{" "}
                        {tip.counterparty.username}
                      </div>
                    </TableCell>
                    <TableCell
                      className={
                        received
                          ? "text-right text-success-foreground"
                          : "text-right text-destructive"
                      }
                    >
                      {received ? "+" : "−"}
                      {formatInteger(
                        received ? tip.net_amount : tip.gross_amount
                      )}
                    </TableCell>
                  </TableRow>
                )
              })}
            </TableBody>
          </Table>
        )}
      </CardContent>
    </Card>
  )
}

function TipTargetLink({ tip }: { tip: ContentTipRecord }) {
  const target = tip.target
  const path =
    target.kind === "torrent" && target.torrent_id
      ? `/torrents/${target.torrent_id}`
      : target.kind === "post" && target.post_id
        ? `/social/post/${target.post_id}`
        : null
  if (!path) return <span title={target.title}>{target.title}</span>
  return (
    <Link
      to={path}
      title={target.title}
      className="hover:text-primary hover:underline"
    >
      {target.title}
    </Link>
  )
}

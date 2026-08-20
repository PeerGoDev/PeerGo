import * as React from "react"
import {
  CalendarCheck2Icon,
  CircleAlertIcon,
  Dice5Icon,
  GiftIcon,
  RefreshCwIcon,
  ShieldCheckIcon,
} from "lucide-react"

import { Alert, AlertDescription, AlertTitle } from "~/components/ui/alert"
import { Badge } from "~/components/ui/badge"
import { Button } from "~/components/ui/button"
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "~/components/ui/card"
import { Skeleton } from "~/components/ui/skeleton"
import { Spinner } from "~/components/ui/spinner"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "~/components/ui/table"
import { ToggleGroup, ToggleGroupItem } from "~/components/ui/toggle-group"
import {
  type AttendanceMode,
  type AttendanceOverview,
  useAttendanceOverview,
  useClaimAttendance,
} from "~/features/economy/api/attendance.queries"
import { requestErrorDescription } from "~/shared/api/problem"
import { formatCompactDateTime } from "~/shared/formatters/date-time"
import { formatInteger } from "~/shared/formatters/integer"

export function AttendanceCard({
  userId,
  csrfToken,
}: {
  userId: string
  csrfToken: string
}) {
  const query = useAttendanceOverview(userId)
  const mutation = useClaimAttendance(userId)

  if (query.isPending) {
    return <Skeleton className="h-64 rounded-lg" />
  }
  if (query.isError || !query.data) {
    return (
      <Alert variant="destructive">
        <CircleAlertIcon />
        <AlertTitle>签到状态暂时无法读取</AlertTitle>
        <AlertDescription>
          {requestErrorDescription(query.error, "请稍后重试。")}
        </AlertDescription>
        <Button
          variant="outline"
          size="sm"
          className="mt-2 w-fit"
          onClick={() => void query.refetch()}
        >
          <RefreshCwIcon data-icon="inline-start" />
          重试
        </Button>
      </Alert>
    )
  }
  return (
    <AttendanceContent
      overview={query.data}
      pending={mutation.isPending}
      error={mutation.error}
      onClaim={(mode) =>
        mutation.mutate({
          csrfToken,
          mode,
          idempotencyKey: globalThis.crypto.randomUUID(),
        })
      }
    />
  )
}

function AttendanceContent({
  overview,
  pending,
  error,
  onClaim,
}: {
  overview: AttendanceOverview
  pending: boolean
  error: Error | null
  onClaim: (mode: AttendanceMode) => void
}) {
  const policy = overview.policy
  const settings = policy?.settings
  const defaultMode: AttendanceMode = settings?.fixed_enabled
    ? "fixed"
    : "random"
  const [mode, setMode] = React.useState<AttendanceMode>(defaultMode)

  React.useEffect(() => {
    setMode(defaultMode)
  }, [defaultMode, policy?.revision])

  if (!policy || !settings?.enabled) {
    return (
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <CalendarCheck2Icon />
            每日签到
          </CardTitle>
          <CardDescription>签到活动当前未开放。</CardDescription>
        </CardHeader>
      </Card>
    )
  }

  return (
    <Card className="gap-0 py-0">
      <CardHeader className="p-6 pb-4">
        <CardTitle className="flex items-center gap-2">
          <CalendarCheck2Icon />
          每日签到
        </CardTitle>
        <CardDescription>
          已连续 {overview.current_streak} 天，累计签到 {overview.total_days} 天
        </CardDescription>
        <CardAction>
          <Badge variant={overview.claimed_today ? "outline" : "secondary"}>
            {overview.claimed_today ? "今日已签到" : "今日待签到"}
          </Badge>
        </CardAction>
      </CardHeader>
      <CardContent className="space-y-5 px-6 pb-6">
        {overview.claimed_today && overview.today_record ? (
          <Alert>
            <ShieldCheckIcon />
            <AlertTitle>
              今日获得 {formatInteger(overview.today_record.total_reward)}{" "}
              魔力值
            </AlertTitle>
            <AlertDescription>
              基础奖励 {formatInteger(overview.today_record.base_reward)}
              {BigInt(overview.today_record.streak_reward) > 0n
                ? `，连续签到加奖 ${formatInteger(overview.today_record.streak_reward)}`
                : ""}
              ，并获得 {formatInteger(overview.today_record.experience_reward)}{" "}
              经验。
            </AlertDescription>
          </Alert>
        ) : overview.claimed_today ? (
          <Alert>
            <ShieldCheckIcon />
            <AlertTitle>旧站今日签到已承接</AlertTitle>
            <AlertDescription>
              今天的签到已经包含在迁移期初数据中，无需重复领取；明天起将继续使用
              PeerGo 签到记录。
            </AlertDescription>
          </Alert>
        ) : (
          <div className="rounded-lg border bg-muted/20 p-4">
            <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
              <div>
                <p className="text-sm font-medium">选择今天的奖励方式</p>
                <p className="mt-1 text-sm text-muted-foreground">
                  固定奖励更稳定，随机奖励由服务端安全抽取且每天只结算一次。
                </p>
              </div>
              <ToggleGroup
                value={[mode]}
                onValueChange={(values) => {
                  const next = values[0]
                  if (next === "fixed" || next === "random") setMode(next)
                }}
                variant="outline"
                spacing={0}
                disabled={pending}
                className="w-full sm:w-auto"
              >
                {settings.fixed_enabled ? (
                  <ToggleGroupItem value="fixed" className="h-10 flex-1 px-4">
                    <GiftIcon data-icon="inline-start" />
                    固定 {formatInteger(settings.fixed_reward)}
                  </ToggleGroupItem>
                ) : null}
                {settings.random_enabled ? (
                  <ToggleGroupItem value="random" className="h-10 flex-1 px-4">
                    <Dice5Icon data-icon="inline-start" />
                    随机 {formatInteger(settings.random_min)}–
                    {formatInteger(settings.random_max)}
                  </ToggleGroupItem>
                ) : null}
              </ToggleGroup>
            </div>
            <Button
              className="mt-4 w-full sm:w-auto"
              disabled={pending}
              onClick={() => onClaim(mode)}
            >
              {pending ? (
                <Spinner data-icon="inline-start" />
              ) : (
                <CalendarCheck2Icon data-icon="inline-start" />
              )}
              {pending ? "正在签到…" : "立即签到"}
            </Button>
          </div>
        )}

        {error ? (
          <Alert variant="destructive">
            <CircleAlertIcon />
            <AlertTitle>签到没有完成</AlertTitle>
            <AlertDescription>
              {requestErrorDescription(error, "请刷新状态后重试。")}
            </AlertDescription>
          </Alert>
        ) : null}

        {overview.history.length > 0 ? (
          <div className="overflow-hidden rounded-lg border">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>日期</TableHead>
                  <TableHead>方式</TableHead>
                  <TableHead>连续</TableHead>
                  <TableHead className="text-right">魔力值</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {overview.history.slice(0, 7).map((record) => (
                  <TableRow
                    key={`${record.attendance_date}-${record.occurred_at}`}
                  >
                    <TableCell>
                      {formatCompactDateTime(record.occurred_at)}
                    </TableCell>
                    <TableCell>
                      {record.mode === "fixed" ? "固定" : "随机"}
                    </TableCell>
                    <TableCell>{record.current_streak} 天</TableCell>
                    <TableCell className="text-right font-medium text-success-foreground">
                      +{formatInteger(record.total_reward)}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        ) : null}
      </CardContent>
    </Card>
  )
}

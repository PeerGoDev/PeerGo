import { Link } from "react-router"
import {
  CircleAlertIcon,
  CoinsIcon,
  GaugeIcon,
  HistoryIcon,
  LogInIcon,
  RefreshCwIcon,
  SparklesIcon,
  StarIcon,
} from "lucide-react"

import {
  Alert,
  AlertAction,
  AlertDescription,
  AlertTitle,
} from "~/components/ui/alert"
import { Button, buttonVariants } from "~/components/ui/button"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "~/components/ui/card"
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "~/components/ui/empty"
import {
  Progress,
  ProgressLabel,
  ProgressValue,
} from "~/components/ui/progress"
import { Skeleton } from "~/components/ui/skeleton"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "~/components/ui/table"
import { useWebSession } from "~/features/auth/api/session.mutations"
import {
  type EconomyOverview,
  useEconomyOverview,
} from "~/features/economy/api/economy.queries"
import { AttendanceCard } from "~/features/economy/components/attendance-card"
import { ContentTipHistoryCard } from "~/features/economy/components/content-tip-history-card"
import { EconomyRulesOverview } from "~/features/economy/components/economy-rules-overview"
import { MemberGiftCard } from "~/features/economy/components/member-gift-card"
import { requestErrorDescription } from "~/shared/api/problem"
import { MetricCard } from "~/shared/components/metric-card"
import { PageHeader, PageLayout } from "~/shared/components/page-layout"
import { formatCompactDateTime } from "~/shared/formatters/date-time"
import { formatInteger } from "~/shared/formatters/integer"

export function EconomyPage() {
  const session = useWebSession()
  const economy = useEconomyOverview(session.data?.user.id)

  return (
    <PageLayout>
      <PageHeader
        title="等级与魔力值"
        description="查看魔力值收支、经验获取和下一等级进度。"
      />

      {session.isPending || (session.data && economy.isPending) ? (
        <EconomySkeleton />
      ) : null}

      {session.isError ? (
        <Alert variant="destructive">
          <CircleAlertIcon />
          <AlertTitle>会话状态暂时无法读取</AlertTitle>
          <AlertDescription>
            {requestErrorDescription(session.error, "请稍后刷新页面。")}
          </AlertDescription>
        </Alert>
      ) : null}

      {!session.isPending && !session.isError && !session.data ? (
        <Card>
          <CardHeader>
            <CardTitle>需要登录</CardTitle>
            <CardDescription>登录后可查看自己的等级与魔力值。</CardDescription>
          </CardHeader>
          <CardContent>
            <Link to="/login" className={buttonVariants()}>
              <LogInIcon data-icon="inline-start" />
              前往登录
            </Link>
          </CardContent>
        </Card>
      ) : null}

      {session.data && economy.isError ? (
        <Alert variant="destructive">
          <CircleAlertIcon />
          <AlertTitle>等级与魔力值暂时无法读取</AlertTitle>
          <AlertDescription>
            {requestErrorDescription(economy.error, "请稍后重试。")}
          </AlertDescription>
          <AlertAction>
            <Button
              variant="outline"
              size="sm"
              onClick={() => void economy.refetch()}
            >
              <RefreshCwIcon data-icon="inline-start" />
              重试
            </Button>
          </AlertAction>
        </Alert>
      ) : null}

      {session.data && economy.data ? (
        <>
          <EconomySummary overview={economy.data} />
          <EconomyRulesOverview
            overview={economy.data}
            userId={session.data.user.id}
          />
          <AttendanceCard
            userId={session.data.user.id}
            csrfToken={session.data.csrf_token}
          />
          <MemberGiftCard
            userId={session.data.user.id}
            csrfToken={session.data.csrf_token}
            magicBalance={economy.data.magic_balance}
          />
          <ContentTipHistoryCard userId={session.data.user.id} />
          <EconomyStatements overview={economy.data} />
        </>
      ) : null}
    </PageLayout>
  )
}

function EconomySummary({ overview }: { overview: EconomyOverview }) {
  const progress = levelProgress(overview)
  return (
    <>
      <div className="grid gap-4 md:grid-cols-3">
        <MetricCard
          title="魔力值"
          value={formatInteger(overview.magic_balance)}
          description="统一整数资产"
          icon={<CoinsIcon />}
          tone="warning"
        />
        <MetricCard
          title="当前等级"
          value={`Lv.${overview.progress.level}`}
          description={
            overview.progress.next
              ? `下一等级 Lv.${overview.progress.next.level}`
              : "已达到当前最高等级"
          }
          icon={<StarIcon />}
          tone="primary"
        />
        <MetricCard
          title="经验值"
          value={formatExperience(overview.progress.experience)}
          description="经验不可消费"
          icon={<SparklesIcon />}
          tone="positive"
        />
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <GaugeIcon data-icon="inline-start" />
            等级进度
          </CardTitle>
          <CardDescription>
            做种奖励及站内贡献会按对应政策增加经验值。
          </CardDescription>
        </CardHeader>
        <CardContent>
          <Progress value={progress.percent}>
            <ProgressLabel>Lv.{overview.progress.level}</ProgressLabel>
            <ProgressValue>
              {() =>
                overview.progress.next
                  ? `${formatExperience(overview.progress.experience)} / ${formatExperience(overview.progress.next.minimum_experience)}`
                  : "最高等级"
              }
            </ProgressValue>
          </Progress>
        </CardContent>
      </Card>
    </>
  )
}

function EconomyStatements({ overview }: { overview: EconomyOverview }) {
  return (
    <>
      <MagicStatement entries={overview.magic_entries} />
      <ExperienceStatement entries={overview.experience_entries} />
    </>
  )
}

function MagicStatement({
  entries,
}: {
  entries: EconomyOverview["magic_entries"]
}) {
  return (
    <Card className="gap-0 py-0">
      <CardHeader className="p-6 pb-4">
        <CardTitle className="flex items-center gap-2">
          <HistoryIcon data-icon="inline-start" />
          魔力值明细
        </CardTitle>
        <CardDescription>
          最近 {entries.length} 条不可变收支记录
        </CardDescription>
      </CardHeader>
      <CardContent className="p-0">
        {entries.length === 0 ? (
          <StatementEmpty description="产生做种或活动奖励后会显示在这里。" />
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>时间</TableHead>
                <TableHead>来源</TableHead>
                <TableHead className="text-right">变动</TableHead>
                <TableHead className="text-right">余额</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {entries.map((entry) => (
                <TableRow key={entry.sequence}>
                  <TableCell className="text-muted-foreground">
                    {formatCompactDateTime(entry.occurred_at)}
                  </TableCell>
                  <TableCell>
                    {magicSourceLabel(entry.transaction_type)}
                  </TableCell>
                  <TableCell
                    className={
                      BigInt(entry.amount) >= 0n
                        ? "text-right font-medium text-success-foreground"
                        : "text-right font-medium text-destructive"
                    }
                  >
                    {BigInt(entry.amount) > 0n ? "+" : ""}
                    {formatInteger(entry.amount)}
                  </TableCell>
                  <TableCell className="text-right tabular-nums">
                    {formatInteger(entry.balance_after)}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </CardContent>
    </Card>
  )
}

function ExperienceStatement({
  entries,
}: {
  entries: EconomyOverview["experience_entries"]
}) {
  return (
    <Card className="gap-0 py-0">
      <CardHeader className="p-6 pb-4">
        <CardTitle className="flex items-center gap-2">
          <SparklesIcon data-icon="inline-start" />
          经验记录
        </CardTitle>
        <CardDescription>经验只用于等级计算，不会被消费。</CardDescription>
      </CardHeader>
      <CardContent className="p-0">
        {entries.length === 0 ? (
          <StatementEmpty description="首次获得经验后会显示在这里。" />
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>时间</TableHead>
                <TableHead>来源</TableHead>
                <TableHead>等级</TableHead>
                <TableHead className="text-right">经验</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {entries.map((entry) => (
                <TableRow key={entry.sequence}>
                  <TableCell className="text-muted-foreground">
                    {formatCompactDateTime(entry.occurred_at)}
                  </TableCell>
                  <TableCell>
                    {experienceSourceLabel(entry.source_kind)}
                  </TableCell>
                  <TableCell>Lv.{entry.level_after}</TableCell>
                  <TableCell className="text-right font-medium text-success-foreground">
                    {entry.amount.startsWith("-") ? "" : "+"}
                    {formatExperience(entry.amount)}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </CardContent>
    </Card>
  )
}

function StatementEmpty({ description }: { description: string }) {
  return (
    <Empty className="min-h-44 rounded-none border-0 border-t">
      <EmptyHeader>
        <EmptyMedia variant="icon">
          <HistoryIcon />
        </EmptyMedia>
        <EmptyTitle>暂无记录</EmptyTitle>
        <EmptyDescription>{description}</EmptyDescription>
      </EmptyHeader>
    </Empty>
  )
}

function levelProgress(overview: EconomyOverview) {
  const next = overview.progress.next
  if (!next) return { percent: 100 }
  const current = parseExact(overview.progress.experience)
  const start = parseExact(overview.progress.current_minimum_experience)
  const end = parseExact(next.minimum_experience)
  const range = end - start
  if (range <= 0n) return { percent: 0 }
  const completed = current > start ? current - start : 0n
  const basisPoints =
    completed >= range ? 10_000n : (completed * 10_000n) / range
  return { percent: Number(basisPoints) / 100 }
}

function parseExact(value: string) {
  const [integer = "0", fraction = ""] = value.split(".")
  const scaledFraction = `${fraction}000000`.slice(0, 6)
  return BigInt(integer) * 1_000_000n + BigInt(scaledFraction)
}

function formatExperience(value: string) {
  const [integer, fraction = ""] = value.split(".")
  const visibleFraction = fraction.slice(0, 2).replace(/0+$/, "")
  return visibleFraction
    ? `${formatInteger(integer)}.${visibleFraction}`
    : formatInteger(integer)
}

function magicSourceLabel(value: string) {
  const labels: Record<string, string> = {
    legacy_opening: "旧站资产迁移",
    seeding_reward: "做种奖励",
    activity_reward: "活动奖励",
    torrent_purchase: "种子购买",
    member_gift: "成员赠送",
    tip: "内容打赏",
    refund: "退还",
    adjustment: "管理员调整",
  }
  return labels[value] ?? "其他"
}

function experienceSourceLabel(value: string) {
  const labels: Record<string, string> = {
    legacy_opening: "旧站经验迁移",
    seeding_reward: "做种奖励",
    torrent_publish: "发布种子",
    activity: "站内活动",
    assessment: "考核任务",
    administrator_adjustment: "管理员调整",
  }
  return labels[value] ?? "其他"
}

function EconomySkeleton() {
  return (
    <div className="flex flex-col gap-5" aria-label="正在加载等级与魔力值">
      <div className="grid gap-4 md:grid-cols-3">
        {Array.from({ length: 3 }, (_, index) => (
          <Skeleton key={index} className="h-36 rounded-lg" />
        ))}
      </div>
      <Skeleton className="h-32 rounded-lg" />
      <Skeleton className="h-80 rounded-lg" />
    </div>
  )
}

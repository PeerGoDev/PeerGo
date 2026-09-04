import { Link, useSearchParams } from "react-router"
import {
  AwardIcon,
  CalendarCheck2Icon,
  CircleAlertIcon,
  CoinsIcon,
  GiftIcon,
  HistoryIcon,
  InfoIcon,
  LogInIcon,
  PinIcon,
  ReceiptTextIcon,
  RefreshCwIcon,
  ShoppingBagIcon,
  SparklesIcon,
  SproutIcon,
  TicketIcon,
  TrendingUpIcon,
} from "lucide-react"

import {
  Alert,
  AlertAction,
  AlertDescription,
  AlertTitle,
} from "~/components/ui/alert"
import { Badge } from "~/components/ui/badge"
import { Button, buttonVariants } from "~/components/ui/button"
import {
  Card,
  CardAction,
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
import { Separator } from "~/components/ui/separator"
import { Skeleton } from "~/components/ui/skeleton"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "~/components/ui/table"
import { ToggleGroup, ToggleGroupItem } from "~/components/ui/toggle-group"
import { useWebSession } from "~/features/auth/api/session.mutations"
import {
  type AttendanceOverview,
  useAttendanceOverview,
} from "~/features/economy/api/attendance.queries"
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
import { PageLayout } from "~/shared/components/page-layout"
import { formatCompactDateTime } from "~/shared/formatters/date-time"
import { formatInteger } from "~/shared/formatters/integer"

const economySections = [
  { value: "overview", label: "总览", icon: TrendingUpIcon },
  { value: "attendance", label: "签到", icon: CalendarCheck2Icon },
  { value: "giving", label: "赠送与打赏", icon: GiftIcon },
  { value: "spending", label: "消费与交易", icon: ShoppingBagIcon },
  { value: "records", label: "记录", icon: HistoryIcon },
] as const

type EconomySection = (typeof economySections)[number]["value"]

export function EconomyPage() {
  const session = useWebSession()
  const economy = useEconomyOverview(session.data?.user.id)
  const attendance = useAttendanceOverview(
    session.data?.user.id,
    Boolean(session.data)
  )
  const [searchParams, setSearchParams] = useSearchParams()
  const activeSection = economySection(searchParams.get("tab"))

  function changeSection(section: EconomySection) {
    const next = new URLSearchParams(searchParams)
    if (section === "overview") next.delete("tab")
    else next.set("tab", section)
    setSearchParams(next, { replace: true })
  }

  return (
    <PageLayout className="gap-6">
      <h1 className="sr-only">等级与魔力值</h1>

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
          <EconomyDashboard
            overview={economy.data}
            attendance={attendance.data}
            attendancePending={attendance.isPending}
          />
          <EconomySectionNavigation
            value={activeSection}
            onValueChange={changeSection}
          />

          <section
            aria-label={economySectionLabel(activeSection)}
            className="flex flex-col gap-5"
          >
            {activeSection === "overview" ? (
              <EconomyRulesOverview
                overview={economy.data}
                userId={session.data.user.id}
              />
            ) : null}
            {activeSection === "attendance" ? (
              <AttendanceCard
                userId={session.data.user.id}
                csrfToken={session.data.csrf_token}
              />
            ) : null}
            {activeSection === "giving" ? (
              <div className="flex flex-col gap-5">
                <MemberGiftCard
                  userId={session.data.user.id}
                  csrfToken={session.data.csrf_token}
                  magicBalance={economy.data.magic_balance}
                />
                <ContentTipHistoryCard userId={session.data.user.id} />
              </div>
            ) : null}
            {activeSection === "spending" ? <EconomySpendingOverview /> : null}
            {activeSection === "records" ? (
              <EconomyStatements overview={economy.data} />
            ) : null}
          </section>
        </>
      ) : null}
    </PageLayout>
  )
}

function EconomyDashboard({
  overview,
  attendance,
  attendancePending,
}: {
  overview: EconomyOverview
  attendance: AttendanceOverview | undefined
  attendancePending: boolean
}) {
  const progress = levelProgress(overview)
  const currentLevel = overview.rules.levels.find(
    (item) => item.level === overview.progress.level
  )
  const nextLevel = overview.progress.next
    ? overview.rules.levels.find(
        (item) => item.level === overview.progress.next?.level
      )
    : undefined
  const seedingPolicy = overview.rules.seeding_reward
  const contribution = overview.rules.contribution_experience

  return (
    <>
      <Card className="gap-0 bg-linear-to-br from-warning/10 via-card to-card py-0">
        <CardContent className="p-5 sm:p-6">
          <div className="grid gap-6 lg:grid-cols-[12rem_minmax(0,1fr)_15rem] lg:items-center">
            <div className="flex items-center gap-4">
              <div className="flex size-16 shrink-0 items-center justify-center rounded-full bg-warning/20 text-warning-foreground">
                <TrendingUpIcon aria-hidden="true" />
              </div>
              <div className="flex flex-col gap-1">
                <p className="text-sm text-muted-foreground">当前等级</p>
                <p className="font-heading text-4xl font-bold text-warning-foreground">
                  Lv.{overview.progress.level}
                </p>
              </div>
            </div>

            <div className="min-w-0">
              <Progress
                value={progress.percent}
                className="[&_[data-slot=progress-track]]:h-2.5"
              >
                <ProgressLabel>
                  经验值 {formatExperience(overview.progress.experience)}
                </ProgressLabel>
                <ProgressValue>
                  {() =>
                    overview.progress.next
                      ? `下一级 ${formatExperience(overview.progress.next.minimum_experience)}`
                      : "已达到当前最高等级"
                  }
                </ProgressValue>
              </Progress>
              <p className="mt-2 text-right text-xs text-muted-foreground">
                {overview.progress.next
                  ? `${formatProgressPercent(progress.percent)} → Lv.${overview.progress.next.level}`
                  : "当前等级规则的最高档位"}
              </p>
            </div>

            <div className="grid grid-cols-2 gap-x-5 gap-y-2 text-xs lg:grid-cols-1">
              <p className="col-span-2 font-medium text-muted-foreground lg:col-span-1">
                经验获取
              </p>
              <ExperienceRate
                label="魔力值"
                value={
                  seedingPolicy
                    ? `×${formatRatio(seedingPolicy.experience_per_magic_bps)}`
                    : "未启用"
                }
              />
              <ExperienceRate
                label="上传 / GiB"
                value={`+${formatMilliExperience(contribution.experience_per_upload_gib_milli)}`}
              />
              <ExperienceRate
                label="发布种子"
                value={`+${formatMilliExperience(contribution.experience_per_torrent_milli)}`}
              />
              <ExperienceRate
                label="签到"
                value={
                  attendance?.policy?.settings.experience_reward
                    ? `+${formatInteger(attendance.policy.settings.experience_reward)}`
                    : "按规则"
                }
              />
            </div>
          </div>

          <Separator className="my-5" />

          <div className="flex flex-wrap items-center gap-2">
            <span className="mr-1 text-xs font-medium text-muted-foreground">
              当前等级权益
            </span>
            <Badge variant="secondary">
              魔力值 +{formatBasisPoints(currentLevel?.karma_bonus_bps ?? 0)}
            </Badge>
            {seedingPolicy ? (
              <Badge variant="outline">
                数量奖励计数 {levelSeedingLimit(overview, currentLevel)} 个
              </Badge>
            ) : (
              <Badge variant="outline">做种奖励未启用</Badge>
            )}
            {overview.progress.next && nextLevel && seedingPolicy ? (
              <span className="text-xs text-muted-foreground lg:ml-auto">
                Lv.{nextLevel.level}：魔力值 +
                {formatBasisPoints(nextLevel.karma_bonus_bps)}，数量奖励计数
                {levelSeedingLimit(overview, nextLevel)} 个
              </span>
            ) : null}
          </div>
        </CardContent>
      </Card>

      <div className="grid gap-4 md:grid-cols-3">
        <MetricCard
          title="魔力值余额"
          value={formatInteger(overview.magic_balance)}
          description="站内唯一可消费资产"
          icon={<CoinsIcon />}
          tone="warning"
        />
        <MetricCard
          title="最近做种结算"
          value={
            overview.latest_seeding_reward
              ? `+${formatInteger(overview.latest_seeding_reward.reward)}`
              : "—"
          }
          description={
            overview.latest_seeding_reward
              ? `${overview.latest_seeding_reward.eligible_torrent_count} 个有效种子 · ${formatCompactDateTime(overview.latest_seeding_reward.calculated_at)}`
              : "暂无已经完成的整点结算"
          }
          icon={<SproutIcon />}
          tone="positive"
        />
        <MetricCard
          title="每日签到"
          value={
            attendancePending
              ? "读取中"
              : attendance
                ? `连续 ${attendance.current_streak} 天`
                : "—"
          }
          description={
            attendance
              ? attendance.claimed_today
                ? `今日已签到 · 累计 ${attendance.total_days} 天`
                : `今日待签到 · 累计 ${attendance.total_days} 天`
              : "签到状态暂时不可用"
          }
          icon={<CalendarCheck2Icon />}
          tone="primary"
        />
      </div>
    </>
  )
}

function ExperienceRate({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex items-center justify-between gap-3">
      <span className="text-muted-foreground">{label}</span>
      <span className="font-medium tabular-nums">{value}</span>
    </div>
  )
}

function EconomySectionNavigation({
  value,
  onValueChange,
}: {
  value: EconomySection
  onValueChange: (value: EconomySection) => void
}) {
  return (
    <nav aria-label="等级与魔力值功能" className="overflow-x-auto border-b">
      <ToggleGroup
        value={[value]}
        onValueChange={(values) => {
          const selected = values[0] as EconomySection | undefined
          if (selected) onValueChange(selected)
        }}
        spacing={0}
        aria-label="切换等级与魔力值内容"
        className="min-w-max gap-0.5"
      >
        {economySections.map((section) => (
          <ToggleGroupItem
            key={section.value}
            value={section.value}
            className="h-9 rounded-lg border-0 px-3.5 text-muted-foreground transition-colors hover:bg-muted hover:text-foreground data-pressed:bg-primary data-pressed:text-primary-foreground"
          >
            <section.icon data-icon="inline-start" />
            {section.label}
          </ToggleGroupItem>
        ))}
      </ToggleGroup>
    </nav>
  )
}

function EconomySpendingOverview() {
  const destinations = [
    {
      title: "勋章中心",
      description: "购买、佩戴勋章并查看当前上传、下载与魔力加成。",
      action: "查看勋章",
      to: "/medals",
      icon: AwardIcon,
    },
    {
      title: "已购种子",
      description: "查看通过魔力值获得的永久种子下载权限与购买价格。",
      action: "查看购买",
      to: "/account/purchases",
      icon: ReceiptTextIcon,
    },
    {
      title: "促销记录",
      description: "查看为已发布种子购买的优惠与列表置顶订单。",
      action: "查看促销",
      to: "/account/promotions",
      icon: PinIcon,
    },
    {
      title: "邀请中心",
      description: "查看可用邀请数量、邀请关系和已经结算的邀请奖励。",
      action: "查看邀请",
      to: "/account/invitations",
      icon: TicketIcon,
    },
  ] as const

  return (
    <div className="flex flex-col gap-5">
      <Alert>
        <InfoIcon />
        <AlertTitle>现行站点统一使用魔力值</AlertTitle>
        <AlertDescription>
          旧版 PT
          币和双向兑换不再新增账本；种子购买、促销和勋章消费都直接使用整数魔力值。
        </AlertDescription>
      </Alert>
      <div className="grid gap-4 sm:grid-cols-2">
        {destinations.map((destination) => (
          <Card key={destination.to} size="sm">
            <CardHeader>
              <CardTitle>{destination.title}</CardTitle>
              <CardDescription>{destination.description}</CardDescription>
              <CardAction className="rounded-full bg-muted p-2 text-muted-foreground">
                <destination.icon aria-hidden="true" />
              </CardAction>
            </CardHeader>
            <CardContent>
              <Link
                to={destination.to}
                className={buttonVariants({ variant: "outline", size: "sm" })}
              >
                {destination.action}
              </Link>
            </CardContent>
          </Card>
        ))}
      </div>
    </div>
  )
}

function EconomyStatements({ overview }: { overview: EconomyOverview }) {
  return (
    <div className="flex flex-col gap-5">
      <MagicStatement entries={overview.magic_entries} />
      <ExperienceStatement entries={overview.experience_entries} />
    </div>
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
          <Table containerClassName="px-3">
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
          <Table containerClassName="px-3">
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
    harem_reward: "后宫奖励",
    activity_reward: "活动奖励",
    torrent_purchase: "种子购买",
    promotion_product_purchase: "种子促销",
    medal_purchase: "购买勋章",
    member_gift: "成员赠送",
    tip: "内容打赏",
    social_red_packet_fund: "发放红包",
    social_red_packet_claim: "领取红包",
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
      <Skeleton className="h-48 rounded-lg" />
      <div className="grid gap-4 md:grid-cols-3">
        {Array.from({ length: 3 }, (_, index) => (
          <Skeleton key={index} className="h-36 rounded-lg" />
        ))}
      </div>
      <Skeleton className="h-11 rounded-lg" />
      <Skeleton className="h-80 rounded-lg" />
    </div>
  )
}

function economySection(value: string | null): EconomySection {
  return economySections.some((section) => section.value === value)
    ? (value as EconomySection)
    : "overview"
}

function economySectionLabel(value: EconomySection) {
  return (
    economySections.find((section) => section.value === value)?.label ?? "总览"
  )
}

function levelSeedingLimit(
  overview: EconomyOverview,
  level: EconomyOverview["rules"]["levels"][number] | undefined
) {
  return (
    (overview.rules.seeding_reward?.base_linear_torrent_limit ?? 0) +
    (level?.seeding_count_bonus ?? 0)
  )
}

function formatProgressPercent(value: number) {
  return `${new Intl.NumberFormat("zh-CN", { maximumFractionDigits: 1 }).format(value)}%`
}

function formatBasisPoints(value: number) {
  return `${new Intl.NumberFormat("zh-CN", { maximumFractionDigits: 2 }).format(value / 100)}%`
}

function formatRatio(value: number) {
  return new Intl.NumberFormat("zh-CN", { maximumFractionDigits: 4 }).format(
    value / 10_000
  )
}

function formatMilliExperience(value: number) {
  return new Intl.NumberFormat("zh-CN", { maximumFractionDigits: 3 }).format(
    value / 1_000
  )
}

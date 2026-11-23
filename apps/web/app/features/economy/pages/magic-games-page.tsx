import * as React from "react"
import { Link } from "react-router"
import {
  CircleAlertIcon,
  DicesIcon,
  HandIcon,
  HandMetalIcon,
  LogInIcon,
  RefreshCwIcon,
  ScissorsIcon,
  SparklesIcon,
  SwordsIcon,
  TicketIcon,
  TrophyIcon,
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
  CardFooter,
  CardHeader,
  CardTitle,
} from "~/components/ui/card"
import { Field, FieldDescription, FieldLabel } from "~/components/ui/field"
import { Input } from "~/components/ui/input"
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
import { Tabs, TabsContent, TabsList, TabsTrigger } from "~/components/ui/tabs"
import { ToggleGroup, ToggleGroupItem } from "~/components/ui/toggle-group"
import { useWebSession } from "~/features/auth/api/session.mutations"
import {
  isMagicRequestUncertain,
  type LotteryRound,
  type MagicGamesOverview,
  type PvPChallenge,
  type PvPMode,
  type RPSChoice,
  type ScratchCard,
  useAcceptPvPChallenge,
  useBuyLotteryTickets,
  useCancelPvPChallenge,
  useCreatePvPChallenge,
  useMagicGames,
  usePlayScratchCard,
} from "~/features/economy/api/magic-store.queries"
import { UserName } from "~/features/economy/components/name-style"
import { verifyLotteryDraw } from "~/features/economy/model/lottery-verification"
import { cn } from "~/lib/utils"
import { requestErrorDescription } from "~/shared/api/problem"
import { PageLayout } from "~/shared/components/page-layout"
import { formatCompactDateTime } from "~/shared/formatters/date-time"
import { formatInteger } from "~/shared/formatters/integer"

type GameTab = "scratch" | "lottery" | "pvp"

export function MagicGamesPage() {
  const session = useWebSession()
  const games = useMagicGames(session.data?.user.id)
  const [tab, setTab] = React.useState<GameTab>("scratch")

  if (session.isPending || (session.data && games.isPending)) {
    return <MagicGamesSkeleton />
  }

  return (
    <PageLayout>
      {session.isError ? (
        <Alert variant="destructive">
          <CircleAlertIcon />
          <AlertTitle>会话状态暂时无法读取</AlertTitle>
          <AlertDescription>
            {requestErrorDescription(session.error, "请稍后刷新页面。")}
          </AlertDescription>
        </Alert>
      ) : null}

      {!session.isError && !session.data ? (
        <Card>
          <CardHeader>
            <CardTitle>需要登录</CardTitle>
            <CardDescription>
              登录后可参与刮刮乐、每日彩票和擂台。
            </CardDescription>
          </CardHeader>
          <CardContent>
            <Link to="/login" className={buttonVariants()}>
              <LogInIcon data-icon="inline-start" />
              前往登录
            </Link>
          </CardContent>
        </Card>
      ) : null}

      {session.data && games.isError ? (
        <Alert variant="destructive">
          <CircleAlertIcon />
          <AlertTitle>游戏中心暂时无法读取</AlertTitle>
          <AlertDescription>
            {requestErrorDescription(games.error, "请稍后重试。")}
          </AlertDescription>
          <AlertAction>
            <Button
              variant="outline"
              size="sm"
              onClick={() => void games.refetch()}
            >
              <RefreshCwIcon data-icon="inline-start" />
              重试
            </Button>
          </AlertAction>
        </Alert>
      ) : null}

      {session.data && games.data ? (
        <>
          <header className="flex flex-wrap items-end justify-between gap-x-4 gap-y-2 pt-1">
            <div className="flex min-w-0 flex-col gap-1">
              <h1 className="font-heading text-lg leading-tight font-semibold tracking-title">
                游戏中心
              </h1>
            </div>
            <div className="flex items-baseline gap-2 rounded-lg border bg-card px-3 py-1.5 shadow-card">
              <span className="text-xs text-muted-foreground">当前魔力值</span>
              <span className="font-mono text-md font-semibold text-warning-foreground tabular-nums">
                {formatInteger(games.data.magic_balance)}
              </span>
            </div>
          </header>

          {!games.data.settings.games_enabled ? (
            <Alert>
              <CircleAlertIcon />
              <AlertTitle>游戏中心暂未开放</AlertTitle>
              <AlertDescription>
                管理员暂时关闭了魔力游戏，请稍后再来。
              </AlertDescription>
            </Alert>
          ) : null}

          <Tabs value={tab} onValueChange={(value) => setTab(value as GameTab)}>
            <TabsList>
              <TabsTrigger value="scratch">
                <SparklesIcon data-icon="inline-start" />
                刮刮乐
              </TabsTrigger>
              <TabsTrigger value="lottery">
                <TicketIcon data-icon="inline-start" />
                每日彩票
              </TabsTrigger>
              <TabsTrigger value="pvp">
                <SwordsIcon data-icon="inline-start" />
                擂台
              </TabsTrigger>
            </TabsList>
            <TabsContent value="scratch" className="flex flex-col gap-4 pt-3">
              <ScratchPanel
                overview={games.data}
                csrfToken={session.data.csrf_token}
              />
            </TabsContent>
            <TabsContent value="lottery" className="flex flex-col gap-4 pt-3">
              <LotteryPanel
                overview={games.data}
                csrfToken={session.data.csrf_token}
              />
            </TabsContent>
            <TabsContent value="pvp" className="flex flex-col gap-4 pt-3">
              <PvPPanel
                overview={games.data}
                csrfToken={session.data.csrf_token}
                viewerId={session.data.user.id}
              />
            </TabsContent>
          </Tabs>
        </>
      ) : null}
    </PageLayout>
  )
}

function ScratchPanel({
  overview,
  csrfToken,
}: {
  overview: MagicGamesOverview
  csrfToken: string
}) {
  const settings = overview.settings.scratch
  const play = usePlayScratchCard()
  const [card, setCard] = React.useState<ScratchCard>()
  const [revealed, setRevealed] = React.useState(false)
  const retry = play.isError && isMagicRequestUncertain(play.error)
  const totalWeight = settings.prizes.reduce(
    (sum, prize) => sum + prize.weight,
    0
  )
  const enabled = overview.settings.games_enabled && settings.enabled
  const exhausted = overview.scratch_used_today >= settings.daily_limit
  const poor = BigInt(overview.magic_balance) < BigInt(settings.price)
  const unopened = Boolean(card && !revealed)
  const history = overview.recent_scratch_cards.filter(
    (item) => !unopened || item.id !== card?.id
  )

  const buy = () => {
    play.mutate(
      {
        csrfToken,
        idempotencyKey:
          (retry && play.variables?.idempotencyKey) || crypto.randomUUID(),
      },
      {
        onSuccess: (receipt) => {
          setCard(receipt.card)
          setRevealed(false)
        },
      }
    )
  }

  return (
    <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_minmax(0,1fr)]">
      <Card>
        <CardHeader>
          <CardTitle>刮刮乐</CardTitle>
          <CardDescription>
            每张 {formatInteger(settings.price)} 魔力，今日已刮{" "}
            {overview.scratch_used_today} / {settings.daily_limit} 张。
          </CardDescription>
        </CardHeader>
        <CardContent className="flex flex-1 flex-col gap-3">
          <div className="relative flex h-40 items-center justify-center overflow-hidden rounded-xl border bg-gradient-to-br from-amber-100 via-orange-50 to-rose-100 dark:from-amber-950/50 dark:via-orange-950/30 dark:to-rose-950/40">
            {card && revealed ? (
              <div
                role="status"
                className="flex flex-col items-center gap-1 text-center"
              >
                <span className="text-sm text-muted-foreground">
                  {card.prize_label}
                </span>
                <span
                  className={cn(
                    "font-mono text-3xl font-bold tabular-nums",
                    card.prize > card.price
                      ? "text-warning-foreground"
                      : card.prize > 0
                        ? "text-foreground"
                        : "text-muted-foreground"
                  )}
                >
                  {card.prize > 0
                    ? `+${formatInteger(card.prize)}`
                    : "谢谢惠顾"}
                </span>
              </div>
            ) : !card ? (
              <span className="text-sm text-muted-foreground">
                购买一张刮刮乐试试手气
              </span>
            ) : null}
            {unopened ? (
              <button
                type="button"
                aria-label="刮开涂层"
                onClick={() => setRevealed(true)}
                className="absolute inset-0 flex items-center justify-center bg-[repeating-linear-gradient(135deg,oklch(0.78_0_0)_0_10px,oklch(0.72_0_0)_10px_20px)] text-sm font-semibold text-neutral-700 focus-visible:outline-2 focus-visible:-outline-offset-4"
              >
                点击刮开
              </button>
            ) : null}
          </div>
          {play.error ? (
            <p className="text-sm text-destructive">
              {requestErrorDescription(play.error, "请稍后重试。")}
            </p>
          ) : null}
        </CardContent>
        <CardFooter>
          <Button
            className="flex-1"
            disabled={
              play.isPending ||
              unopened ||
              (!retry && (!enabled || exhausted || poor))
            }
            onClick={buy}
          >
            {play.isPending ? (
              <Spinner data-icon="inline-start" />
            ) : (
              <SparklesIcon data-icon="inline-start" />
            )}
            {retry
              ? "确认上次购卡"
              : unopened
                ? "先刮开这张"
                : !enabled
                  ? "暂未开放"
                  : exhausted
                    ? "今日次数已用完"
                    : poor
                      ? "魔力值不足"
                      : `${card ? "再来一张" : "购买一张"} · ${formatInteger(settings.price)} 魔力`}
          </Button>
        </CardFooter>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>奖池说明</CardTitle>
          <CardDescription>
            理论返还率 {formatBPS(overview.scratch_expected_return_bps)}。
          </CardDescription>
        </CardHeader>
        <CardContent>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>奖项</TableHead>
                <TableHead className="text-right">奖金</TableHead>
                <TableHead className="text-right">概率</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {settings.prizes.map((prize, index) => (
                <TableRow key={index}>
                  <TableCell>{prize.label}</TableCell>
                  <TableCell className="text-right font-mono tabular-nums">
                    {formatInteger(prize.payout)}
                  </TableCell>
                  <TableCell className="text-right tabular-nums">
                    {totalWeight > 0
                      ? `${((prize.weight / totalWeight) * 100).toFixed(2)}%`
                      : "—"}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </CardContent>
      </Card>

      <Card className="lg:col-span-2">
        <CardHeader>
          <CardTitle>最近刮奖</CardTitle>
        </CardHeader>
        <CardContent>
          {history.length === 0 ? (
            <p className="py-4 text-center text-sm text-muted-foreground">
              还没有刮奖记录。
            </p>
          ) : (
            <div className="flex flex-wrap gap-2">
              {history.map((item) => (
                <Badge
                  key={item.id}
                  variant={
                    item.prize > item.price
                      ? "warning"
                      : item.prize > 0
                        ? "secondary"
                        : "outline"
                  }
                  title={formatCompactDateTime(item.purchased_at)}
                >
                  {item.prize_label}{" "}
                  {item.prize > 0 ? `+${formatInteger(item.prize)}` : ""}
                </Badge>
              ))}
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  )
}

function LotteryPanel({
  overview,
  csrfToken,
}: {
  overview: MagicGamesOverview
  csrfToken: string
}) {
  const round = overview.current_round
  const enabled =
    overview.settings.games_enabled && overview.settings.lottery.enabled
  return (
    <div className="grid gap-4 lg:grid-cols-[minmax(0,3fr)_minmax(0,2fr)]">
      {round ? (
        <CurrentRoundCard
          round={round}
          enabled={enabled}
          balance={BigInt(overview.magic_balance)}
          csrfToken={csrfToken}
        />
      ) : (
        <Card>
          <CardHeader>
            <CardTitle>本期彩票</CardTitle>
            <CardDescription>
              {enabled ? "新一期正在准备中。" : "彩票暂未开放。"}
            </CardDescription>
          </CardHeader>
        </Card>
      )}
      {overview.previous_round ? (
        <PreviousRoundCard
          key={overview.previous_round.id}
          round={overview.previous_round}
        />
      ) : (
        <Card>
          <CardHeader>
            <CardTitle>上期开奖</CardTitle>
            <CardDescription>还没有已开奖的期次。</CardDescription>
          </CardHeader>
        </Card>
      )}
    </div>
  )
}

function CurrentRoundCard({
  round,
  enabled,
  balance,
  csrfToken,
}: {
  round: LotteryRound
  enabled: boolean
  balance: bigint
  csrfToken: string
}) {
  const buy = useBuyLotteryTickets()
  const [quantity, setQuantity] = React.useState("1")
  const retry = buy.isError && isMagicRequestUncertain(buy.error)
  const remaining = Math.max(
    0,
    round.maximum_tickets_per_member - round.my_tickets
  )
  const count = Number(quantity)
  const valid = Number.isInteger(count) && count >= 1 && count <= remaining
  const total = BigInt(round.ticket_price) * BigInt(valid ? count : 0)
  const { countdown, elapsed } = useCountdown(round.draw_at)
  const closed = elapsed || round.status !== "open"
  const pool =
    round.carried_in +
    round.sales_total -
    Math.trunc((round.sales_total * round.burn_bps) / 10000)

  return (
    <Card>
      <CardHeader>
        <CardTitle>第 {round.round_number} 期</CardTitle>
        <CardDescription>
          {formatCompactDateTime(round.draw_at)} 开奖 ·{" "}
          {closed ? "已截止，等待开奖" : `剩余 ${countdown}`}
          <span className="block">
            购票后获得号码，中奖奖励将在开奖后自动入账。
          </span>
        </CardDescription>
        <CardAction>
          <Badge variant="warning" className="font-mono tabular-nums">
            奖池 {formatInteger(pool)}
          </Badge>
        </CardAction>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <div className="grid grid-cols-3 gap-2 text-center">
          <Stat label="已售彩票" value={formatInteger(round.tickets_sold)} />
          <Stat label="上期滚存" value={formatInteger(round.carried_in)} />
          <Stat
            label="我的彩票"
            value={`${round.my_tickets} / ${round.maximum_tickets_per_member}`}
          />
        </div>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>奖级</TableHead>
              <TableHead className="text-right">名额</TableHead>
              <TableHead className="text-right">奖池占比</TableHead>
              <TableHead className="text-right">预计单注奖金</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {round.tiers.map((tier, index) => (
              <TableRow key={index}>
                <TableCell>{tier.label}</TableCell>
                <TableCell className="text-right tabular-nums">
                  {tier.winners}
                </TableCell>
                <TableCell className="text-right tabular-nums">
                  {formatBPS(tier.share_bps)}
                </TableCell>
                <TableCell className="text-right font-mono tabular-nums">
                  {formatInteger(round.estimated_prizes[index] ?? 0)}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
        <TicketNumbers ranges={round.my_ticket_ranges} />
        <LotteryVerificationDetails digest={round.seed_sha256} />
        <p className="text-xs text-muted-foreground">
          销售额的 {formatBPS(round.burn_bps)}{" "}
          从奖池扣除，未派出的奖金滚入下一期。
        </p>
        {buy.error ? (
          <p className="text-sm text-destructive">
            {requestErrorDescription(buy.error, "请稍后重试。")}
          </p>
        ) : buy.data ? (
          <p className="text-sm text-success-foreground">
            已购买第 {buy.data.round_number} 期 {buy.data.quantity} 注，号码 #
            {buy.data.first_ticket}
            {buy.data.last_ticket !== buy.data.first_ticket
              ? `–${buy.data.last_ticket}`
              : ""}
            。
          </p>
        ) : null}
      </CardContent>
      <CardFooter className="flex-wrap gap-2">
        <Input
          type="number"
          min={1}
          max={remaining}
          value={quantity}
          onChange={(event) => setQuantity(event.target.value)}
          className="w-24"
          aria-label="购买注数"
          disabled={
            !enabled || closed || remaining === 0 || buy.isPending || retry
          }
        />
        <Button
          className="flex-1"
          disabled={
            buy.isPending ||
            (!retry && (!enabled || closed || !valid || balance < total))
          }
          onClick={() => {
            if (retry && buy.variables) {
              buy.mutate({ ...buy.variables, csrfToken })
              return
            }
            buy.mutate({
              csrfToken,
              idempotencyKey: crypto.randomUUID(),
              quantity: count,
              roundId: round.id,
            })
          }}
        >
          {buy.isPending ? (
            <Spinner data-icon="inline-start" />
          ) : (
            <TicketIcon data-icon="inline-start" />
          )}
          {retry
            ? "确认上次购票"
            : !enabled
              ? "暂未开放"
              : closed
                ? "已停止售票"
                : remaining === 0
                  ? "本期已达购买上限"
                  : !valid
                    ? `请输入 1–${remaining} 的整数`
                    : balance < total
                      ? "魔力值不足"
                      : `购买 · ${formatInteger(total)} 魔力`}
        </Button>
      </CardFooter>
    </Card>
  )
}

function PreviousRoundCard({ round }: { round: LotteryRound }) {
  const [verification, setVerification] = React.useState<
    "pending" | "valid" | "mismatch" | "unavailable"
  >()

  const verify = async () => {
    setVerification("pending")
    try {
      setVerification((await verifyLotteryDraw(round)) ? "valid" : "mismatch")
    } catch {
      setVerification("unavailable")
    }
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>第 {round.round_number} 期开奖</CardTitle>
        <CardDescription>
          {round.drawn_at
            ? formatCompactDateTime(round.drawn_at)
            : formatCompactDateTime(round.draw_at)}{" "}
          · 售出 {formatInteger(round.tickets_sold)} 注
        </CardDescription>
        {round.my_prize > 0 ? (
          <CardAction>
            <Badge variant="warning">
              我中奖 +{formatInteger(round.my_prize)}
            </Badge>
          </CardAction>
        ) : null}
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        <TicketNumbers ranges={round.my_ticket_ranges} />
        {round.winners.length === 0 ? (
          <p className="text-sm text-muted-foreground">
            本期无人中奖，奖金已滚入下一期。
          </p>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>奖级</TableHead>
                <TableHead>号码</TableHead>
                <TableHead>得主</TableHead>
                <TableHead className="text-right">奖金</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {round.winners.map((winner) => (
                <TableRow key={winner.draw_rank}>
                  <TableCell>{winner.tier_label}</TableCell>
                  <TableCell className="font-mono tabular-nums">
                    #{winner.ticket_number}
                  </TableCell>
                  <TableCell className="max-w-32 truncate">
                    <UserName username={winner.username} />
                  </TableCell>
                  <TableCell className="text-right font-mono tabular-nums">
                    {formatInteger(winner.prize)}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
        <div className="grid grid-cols-3 gap-2 text-center">
          <Stat label="派奖" value={formatInteger(round.paid_out ?? 0)} />
          <Stat label="销毁" value={formatInteger(round.burned ?? 0)} />
          <Stat label="滚存" value={formatInteger(round.carried_out ?? 0)} />
        </div>
        {round.seed ? (
          <div className="flex flex-col items-start gap-2">
            <LotteryVerificationDetails
              digest={round.seed_sha256}
              seed={round.seed}
            />
            <Button
              variant="outline"
              size="sm"
              disabled={verification === "pending"}
              onClick={() => void verify()}
            >
              校验开奖
            </Button>
            {verification ? (
              <p role="status" className="text-xs text-muted-foreground">
                {
                  {
                    pending: "正在重算开奖号码…",
                    valid: "种子摘要与开奖号码均一致。",
                    mismatch: "校验失败，请核对种子摘要和开奖号码。",
                    unavailable: "当前浏览器环境无法校验，请在安全连接中重试。",
                  }[verification]
                }
              </p>
            ) : null}
          </div>
        ) : null}
      </CardContent>
    </Card>
  )
}

function LotteryVerificationDetails({
  digest,
  seed,
}: {
  digest: string
  seed?: string
}) {
  return (
    <details className="w-full text-xs text-muted-foreground">
      <summary className="w-fit cursor-pointer underline-offset-4 hover:underline">
        {seed ? "查看上期开奖校验数据" : "查看本期开奖校验摘要"}
      </summary>
      <dl className="mt-2 space-y-2 break-all">
        {seed ? (
          <div>
            <dt>开奖种子</dt>
            <dd className="font-mono">{seed}</dd>
          </div>
        ) : null}
        <div>
          <dt>种子摘要 SHA-256</dt>
          <dd className="font-mono">{digest}</dd>
        </div>
      </dl>
    </details>
  )
}

function TicketNumbers({
  ranges,
}: {
  ranges: LotteryRound["my_ticket_ranges"]
}) {
  if (ranges.length === 0) return null
  return (
    <p className="text-xs text-muted-foreground">
      我的号码：
      {ranges
        .map(([first, last]) =>
          first === last ? `#${first}` : `#${first}–${last}`
        )
        .join("、")}
    </p>
  )
}

const RPS_OPTIONS: {
  value: RPSChoice
  label: string
  icon: typeof HandIcon
}[] = [
  { value: "rock", label: "石头", icon: HandMetalIcon },
  { value: "scissors", label: "剪刀", icon: ScissorsIcon },
  { value: "paper", label: "布", icon: HandIcon },
]

function rpsLabel(choice?: string | null) {
  return RPS_OPTIONS.find((option) => option.value === choice)?.label ?? "?"
}

function PvPPanel({
  overview,
  csrfToken,
  viewerId,
}: {
  overview: MagicGamesOverview
  csrfToken: string
  viewerId: string
}) {
  const settings = overview.settings.pvp
  const enabled = overview.settings.games_enabled && settings.enabled
  const create = useCreatePvPChallenge()
  const accept = useAcceptPvPChallenge()
  const cancel = useCancelPvPChallenge()
  const [mode, setMode] = React.useState<PvPMode>("dice")
  const [stake, setStake] = React.useState(String(settings.minimum_stake))
  const [choice, setChoice] = React.useState<RPSChoice>()
  const [message, setMessage] = React.useState("")
  const [result, setResult] = React.useState<PvPChallenge>()
  const stakeValue = Number(stake)
  const stakeValid =
    Number.isInteger(stakeValue) &&
    stakeValue >= settings.minimum_stake &&
    stakeValue <= settings.maximum_stake
  const balance = BigInt(overview.magic_balance)
  const openOthers = overview.open_challenges.filter(
    (item) => !item.viewer_is_creator
  )
  const mine = overview.my_challenges.filter((item) => item.status === "open")
  const error = create.error ?? accept.error ?? cancel.error
  const retryCreate = create.isError && isMagicRequestUncertain(create.error)
  const busy = create.isPending || accept.isPending || cancel.isPending
  const lockDraft = busy || retryCreate

  return (
    <>
      {error ? (
        <Alert variant="destructive">
          <CircleAlertIcon />
          <AlertTitle>擂台操作未完成</AlertTitle>
          <AlertDescription>
            {requestErrorDescription(error, "请刷新后重试。")}
          </AlertDescription>
        </Alert>
      ) : result ? (
        <PvPResultAlert challenge={result} viewerId={viewerId} />
      ) : null}

      <div className="grid gap-4 lg:grid-cols-[minmax(0,2fr)_minmax(0,3fr)]">
        <Card>
          <CardHeader>
            <CardTitle>发起擂台</CardTitle>
            <CardDescription>
              押注 {formatInteger(settings.minimum_stake)}–
              {formatInteger(settings.maximum_stake)}{" "}
              魔力，胜者赢得双方押注，扣除 {formatBPS(settings.fee_bps)}{" "}
              手续费；{settings.expiry_hours} 小时无人应战自动退回。
            </CardDescription>
          </CardHeader>
          <CardContent className="flex flex-col gap-4">
            <Field>
              <FieldLabel>玩法</FieldLabel>
              <ToggleGroup
                value={[mode]}
                disabled={lockDraft}
                onValueChange={(values) => {
                  const next = values[0] as PvPMode | undefined
                  if (next) setMode(next)
                }}
                variant="segment"
                size="sm"
                spacing={0.5}
              >
                <ToggleGroupItem value="dice">
                  <DicesIcon data-icon="inline-start" />
                  比大小
                </ToggleGroupItem>
                <ToggleGroupItem value="rps">
                  <HandIcon data-icon="inline-start" />
                  猜拳
                </ToggleGroupItem>
              </ToggleGroup>
              <FieldDescription>
                {mode === "dice"
                  ? "双方各掷 1–100 点，点数大者获胜，平局自动重掷。"
                  : "你先暗中出拳，对手应战时出拳；平局双方全额退回。"}
              </FieldDescription>
            </Field>
            <Field>
              <FieldLabel htmlFor="pvp-stake">押注魔力</FieldLabel>
              <Input
                id="pvp-stake"
                type="number"
                min={settings.minimum_stake}
                max={settings.maximum_stake}
                value={stake}
                disabled={lockDraft}
                onChange={(event) => setStake(event.target.value)}
              />
            </Field>
            {mode === "rps" ? (
              <Field>
                <FieldLabel>我的出拳（对手不可见）</FieldLabel>
                <RPSPicker
                  value={choice}
                  onChange={setChoice}
                  disabled={lockDraft}
                />
              </Field>
            ) : null}
            <Field>
              <FieldLabel htmlFor="pvp-message">擂台宣言（可选）</FieldLabel>
              <Input
                id="pvp-message"
                maxLength={60}
                value={message}
                disabled={lockDraft}
                placeholder="来战！"
                onChange={(event) => setMessage(event.target.value)}
              />
            </Field>
          </CardContent>
          <CardFooter>
            <Button
              className="flex-1"
              disabled={
                busy ||
                (!retryCreate &&
                  (!enabled ||
                    !stakeValid ||
                    balance < BigInt(stakeValid ? stakeValue : 0) ||
                    (mode === "rps" && !choice) ||
                    mine.length >= settings.maximum_open_per_member))
              }
              onClick={() => {
                setResult(undefined)
                accept.reset()
                cancel.reset()
                create.mutate(
                  retryCreate && create.variables
                    ? { ...create.variables, csrfToken }
                    : {
                        csrfToken,
                        idempotencyKey: crypto.randomUUID(),
                        mode,
                        stake: stakeValue,
                        choice: mode === "rps" ? choice : undefined,
                        message: message.trim() || undefined,
                      },
                  {
                    onSuccess: () => {
                      setMessage("")
                    },
                  }
                )
              }}
            >
              {create.isPending ? (
                <Spinner data-icon="inline-start" />
              ) : (
                <SwordsIcon data-icon="inline-start" />
              )}
              {retryCreate
                ? "确认上次发起"
                : !enabled
                  ? "暂未开放"
                  : mine.length >= settings.maximum_open_per_member
                    ? `最多同时 ${settings.maximum_open_per_member} 个擂台`
                    : "摆下擂台"}
            </Button>
          </CardFooter>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>等待应战</CardTitle>
            <CardDescription>应战时会冻结与擂主相同的押注。</CardDescription>
          </CardHeader>
          <CardContent className="flex flex-col gap-2">
            {openOthers.length === 0 ? (
              <p className="py-6 text-center text-sm text-muted-foreground">
                暂时没有可应战的擂台。
              </p>
            ) : (
              openOthers.map((challenge) => (
                <OpenChallengeRow
                  key={challenge.id}
                  challenge={challenge}
                  disabled={
                    !enabled ||
                    balance < BigInt(challenge.stake) ||
                    busy ||
                    retryCreate
                  }
                  busy={
                    accept.isPending &&
                    accept.variables?.challengeId === challenge.id
                  }
                  onAccept={(rps) => {
                    setResult(undefined)
                    create.reset()
                    cancel.reset()
                    accept.mutate(
                      { csrfToken, challengeId: challenge.id, choice: rps },
                      { onSuccess: (data) => setResult(data) }
                    )
                  }}
                />
              ))
            )}
          </CardContent>
        </Card>
      </div>

      <div className="grid gap-4 lg:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle>我的擂台</CardTitle>
          </CardHeader>
          <CardContent className="flex flex-col gap-2">
            {overview.my_challenges.length === 0 ? (
              <p className="py-4 text-center text-sm text-muted-foreground">
                还没有发起或参与过擂台。
              </p>
            ) : (
              overview.my_challenges.map((challenge) => (
                <ChallengeSummary
                  key={challenge.id}
                  challenge={challenge}
                  viewerId={viewerId}
                >
                  {challenge.status === "open" &&
                  challenge.viewer_is_creator ? (
                    <Button
                      variant="outline"
                      size="xs"
                      disabled={busy || retryCreate}
                      onClick={() => {
                        setResult(undefined)
                        cancel.mutate({ csrfToken, challengeId: challenge.id })
                      }}
                    >
                      撤回
                    </Button>
                  ) : null}
                </ChallengeSummary>
              ))
            )}
          </CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardTitle>最近战况</CardTitle>
          </CardHeader>
          <CardContent className="flex flex-col gap-2">
            {overview.recent_settled.length === 0 ? (
              <p className="py-4 text-center text-sm text-muted-foreground">
                还没有已结束的擂台。
              </p>
            ) : (
              overview.recent_settled.map((challenge) => (
                <ChallengeSummary
                  key={challenge.id}
                  challenge={challenge}
                  viewerId={viewerId}
                />
              ))
            )}
          </CardContent>
        </Card>
      </div>
    </>
  )
}

function RPSPicker({
  value,
  onChange,
  disabled,
}: {
  value?: RPSChoice
  onChange: (choice: RPSChoice) => void
  disabled?: boolean
}) {
  return (
    <ToggleGroup
      disabled={disabled}
      value={value ? [value] : []}
      onValueChange={(values) => {
        const next = values[0] as RPSChoice | undefined
        if (next) onChange(next)
      }}
      variant="segment"
      size="sm"
      spacing={0.5}
    >
      {RPS_OPTIONS.map((option) => (
        <ToggleGroupItem key={option.value} value={option.value}>
          <option.icon data-icon="inline-start" />
          {option.label}
        </ToggleGroupItem>
      ))}
    </ToggleGroup>
  )
}

function OpenChallengeRow({
  challenge,
  disabled,
  busy,
  onAccept,
}: {
  challenge: PvPChallenge
  disabled: boolean
  busy: boolean
  onAccept: (choice?: RPSChoice) => void
}) {
  return (
    <div className="flex flex-wrap items-center justify-between gap-2 rounded-lg border bg-muted/40 px-3 py-2">
      <div className="flex min-w-0 flex-col gap-0.5">
        <div className="flex min-w-0 items-center gap-2 text-sm">
          <Badge variant="secondary">
            {challenge.mode === "dice" ? "比大小" : "猜拳"}
          </Badge>
          <UserName
            username={challenge.creator_username}
            className="truncate font-medium"
          />
          <span className="font-mono text-warning-foreground tabular-nums">
            {formatInteger(challenge.stake)}
          </span>
        </div>
        <span className="truncate text-xs text-muted-foreground">
          {challenge.message || "无宣言"} ·{" "}
          {formatCompactDateTime(challenge.expires_at)} 截止
        </span>
      </div>
      {challenge.mode === "dice" ? (
        <Button size="sm" disabled={disabled} onClick={() => onAccept()}>
          {busy ? (
            <Spinner data-icon="inline-start" />
          ) : (
            <DicesIcon data-icon="inline-start" />
          )}
          应战
        </Button>
      ) : (
        <div className="flex gap-1">
          {RPS_OPTIONS.map((option) => (
            <Button
              key={option.value}
              size="sm"
              variant="outline"
              disabled={disabled}
              onClick={() => onAccept(option.value)}
              aria-label={`出${option.label}应战`}
            >
              <option.icon data-icon="inline-start" />
              {option.label}
            </Button>
          ))}
        </div>
      )}
    </div>
  )
}

function ChallengeSummary({
  challenge,
  viewerId,
  children,
}: {
  challenge: PvPChallenge
  viewerId: string
  children?: React.ReactNode
}) {
  const involved =
    challenge.creator_id === viewerId || challenge.opponent_id === viewerId
  const won = challenge.winner_id === viewerId
  const detail =
    challenge.mode === "dice"
      ? challenge.creator_roll
        ? `${challenge.creator_roll} : ${challenge.opponent_roll}`
        : ""
      : challenge.opponent_choice
        ? `${rpsLabel(challenge.creator_choice)} vs ${rpsLabel(challenge.opponent_choice)}`
        : challenge.creator_choice
          ? `已出${rpsLabel(challenge.creator_choice)}`
          : ""

  return (
    <div className="flex flex-wrap items-center justify-between gap-2 rounded-lg border px-3 py-2 text-sm">
      <div className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-0.5">
        <Badge variant="outline">
          {challenge.mode === "dice" ? "比大小" : "猜拳"}
        </Badge>
        <UserName
          username={challenge.creator_username}
          className="max-w-28 truncate"
        />
        <span className="text-muted-foreground">vs</span>
        {challenge.opponent_username ? (
          <UserName
            username={challenge.opponent_username}
            className="max-w-28 truncate"
          />
        ) : (
          <span className="text-muted-foreground">待应战</span>
        )}
        <span className="font-mono text-warning-foreground tabular-nums">
          {formatInteger(challenge.stake)}
        </span>
        {detail ? (
          <span className="text-xs text-muted-foreground">{detail}</span>
        ) : null}
      </div>
      <div className="flex items-center gap-2">
        <ChallengeStatus challenge={challenge} involved={involved} won={won} />
        {children}
      </div>
    </div>
  )
}

function ChallengeStatus({
  challenge,
  involved,
  won,
}: {
  challenge: PvPChallenge
  involved: boolean
  won: boolean
}) {
  if (challenge.status === "open")
    return <Badge variant="primary">进行中</Badge>
  if (challenge.status === "cancelled")
    return <Badge variant="outline">已撤回</Badge>
  if (challenge.status === "expired")
    return <Badge variant="outline">已过期</Badge>
  if (!challenge.winner_id) return <Badge variant="secondary">平局</Badge>
  if (!involved) return <Badge variant="secondary">已结算</Badge>
  return won ? (
    <Badge variant="success">
      <TrophyIcon data-icon="inline-start" />胜
    </Badge>
  ) : (
    <Badge variant="destructive">负</Badge>
  )
}

function PvPResultAlert({
  challenge,
  viewerId,
}: {
  challenge: PvPChallenge
  viewerId: string
}) {
  const tie = challenge.status === "settled" && !challenge.winner_id
  const won = challenge.winner_id === viewerId
  const prize = challenge.stake * 2 - (challenge.fee ?? 0)
  return (
    <Alert variant={tie || won ? "default" : "destructive"}>
      {won ? <TrophyIcon /> : <SwordsIcon />}
      <AlertTitle>
        {tie
          ? "平局，押注已退回"
          : won
            ? `你赢了！获得 ${formatInteger(prize)} 魔力`
            : "惜败"}
      </AlertTitle>
      <AlertDescription>
        {challenge.mode === "dice"
          ? `擂主 ${challenge.creator_roll ?? "?"} 点，你 ${challenge.opponent_roll ?? "?"} 点。`
          : `擂主出${rpsLabel(challenge.creator_choice)}，你出${rpsLabel(challenge.opponent_choice)}。`}
      </AlertDescription>
    </Alert>
  )
}

function Stat({ label, value }: { label: string; value: string }) {
  return (
    <dl className="rounded-lg border bg-muted/40 px-2 py-2">
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className="font-mono text-sm font-semibold tabular-nums">{value}</dd>
    </dl>
  )
}

function useCountdown(target: string) {
  const [now, setNow] = React.useState(() => Date.now())
  React.useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 1000)
    return () => window.clearInterval(timer)
  }, [])
  const remaining = Date.parse(target) - now
  const seconds = Math.max(0, Math.ceil(remaining / 1000))
  const hours = Math.floor(seconds / 3600)
  const minutes = Math.floor((seconds % 3600) / 60)
  const rest = seconds % 60
  return {
    countdown: `${hours}:${String(minutes).padStart(2, "0")}:${String(rest).padStart(2, "0")}`,
    elapsed: remaining <= 0,
  }
}

function formatBPS(value: number) {
  return `${(value / 100).toFixed(value % 100 === 0 ? 0 : 2)}%`
}

function MagicGamesSkeleton() {
  return (
    <PageLayout aria-label="游戏中心加载中">
      <div className="flex items-start justify-between gap-4">
        <div className="flex flex-col gap-2">
          <Skeleton className="h-9 w-40" />
          <Skeleton className="h-4 w-56" />
        </div>
        <Skeleton className="h-12 w-44" />
      </div>
      <Skeleton className="h-9 w-72" />
      <div className="grid gap-4 lg:grid-cols-2">
        <Skeleton className="h-72 w-full" />
        <Skeleton className="h-72 w-full" />
      </div>
    </PageLayout>
  )
}
